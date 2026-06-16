package collector

import (
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"regexp"
	"strings"

	"github.com/google/uuid"
)

// pscMember represents a member returned by the PSC REST API.
type pscMember struct {
	Name          string `json:"name"`
	Domain        string `json:"domain"`
	Kind          string `json:"kind"`
	Type          string `json:"type"`
	PrincipalName string `json:"principalName"`
}

// pscClient bundles an HTTP client holding an authenticated VSPHERE-UI-JSESSIONID
// and the XSRF token needed for all /ui/psc-ui/ requests.
type pscClient struct {
	http              *http.Client
	xsrfToken         string
	webClientSessionID string
	jar               *cookiejar.Jar
}

// loggingTransport wraps a RoundTripper and logs every Set-Cookie header so we
// can trace when VSPHERE-UI-XSRF-TOKEN is first issued by the server.
type loggingTransport struct {
	base http.RoundTripper
}

func (t *loggingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := t.base.RoundTrip(req)
	if err != nil {
		return resp, err
	}
	for _, sc := range resp.Header["Set-Cookie"] {
		log.Printf("  [SetCookie] %s  ←  %s %s", truncate(sc, 120), req.Method, req.URL.Path)
	}
	return resp, nil
}

// CollectGroupMemberships collects group memberships via the PSC REST API.
func (c *Collector) CollectGroupMemberships() error {
	psc, err := c.buildPSCClient()
	if err != nil {
		return fmt.Errorf("failed to build PSC client: %w", err)
	}

	// Warm up the PSC backend session — the browser Angular app always calls
	// these endpoints before groups, which initialises a server-side PSC session.
	if err := c.warmupPSC(psc); err != nil {
		log.Printf("  [WARN] PSC warmup failed (continuing anyway): %v", err)
	}

	groupPrincipals, err := c.fetchPSCAllGroups(psc)
	if err != nil {
		log.Printf("  [WARN] failed to fetch all groups: %v", err)
		return err
	}

	if len(groupPrincipals) == 0 {
		log.Println("No groups found; skipping group membership collection.")
		return nil
	}

	for _, groupPrincipal := range groupPrincipals {
		parentGID := fmt.Sprintf("group:%s:%s", c.Config.Host, groupPrincipal)
		groupQuery := principalToAtFormat(groupPrincipal)

		log.Printf("Fetching PSC members for group: %s (query: %s)", groupPrincipal, groupQuery)
		if err := c.fetchPSCGroupMembers(psc, groupQuery, parentGID); err != nil {
			log.Printf("  [WARN] %v", err)
		}
	}

	return nil
}

// warmupPSC calls lightweight PSC endpoints in the same order the vSphere Angular
// app does before it fetches groups. This initialises the PSC backend session so
// subsequent requests to /ui/psc-ui/ctrl/psc/tenant/* are accepted.
func (c *Collector) warmupPSC(psc *pscClient) error {
	for _, path := range []string{
		"/ui/psc-ui/ctrl/psc/passwordpolicy",
		"/ui/psc-ui/ctrl/psc/domains",
	} {
		endpoint := fmt.Sprintf("https://%s%s", c.Config.Host, path)
		req, err := http.NewRequestWithContext(c.Context, http.MethodGet, endpoint, nil)
		if err != nil {
			return err
		}
		req.Header.Set("Accept", "application/json, text/plain, */*")
		req.Header.Set("X-Requested-With", "XMLHttpRequest")
		req.Header.Set("webclientsessionid", psc.webClientSessionID)
		req.Header.Set("Referer", fmt.Sprintf("https://%s/ui/", c.Config.Host))
		if psc.xsrfToken != "" {
			req.Header.Set("X-VSPHERE-UI-XSRF-TOKEN", psc.xsrfToken)
		}
		resp, err := psc.http.Do(req)
		if err != nil {
			return err
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		log.Printf("  PSC warmup %s → HTTP %d: %s", path, resp.StatusCode, truncate(string(body), 100))
	}
	return nil
}

// fetchPSCAllGroups retrieves all groups via the PSC REST API.
func (c *Collector) fetchPSCAllGroups(psc *pscClient) ([]string, error) {
	endpoint := fmt.Sprintf("https://%s/ui/psc-ui/ctrl/psc/tenant/groups?query=", c.Config.Host)
	
	req, err := http.NewRequestWithContext(c.Context, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Accept", "application/json, text/plain, */*")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	req.Header.Set("webclientsessionid", psc.webClientSessionID)
	req.Header.Set("Origin", fmt.Sprintf("https://%s", c.Config.Host))
	req.Header.Set("Referer", fmt.Sprintf("https://%s/ui/", c.Config.Host))
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")
	if psc.xsrfToken != "" {
		req.Header.Set("X-VSPHERE-UI-XSRF-TOKEN", psc.xsrfToken)
		req.Header.Set("X-XSRF-TOKEN", psc.xsrfToken)
	}

	resp, err := psc.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("HTTP GET: %w", err)
	}
	defer resp.Body.Close()

	rawBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("PSC API HTTP %d for groups list: %s", resp.StatusCode, truncate(string(rawBody), 2000))
	}
	
	log.Printf("  PSC raw response for groups: %s", truncate(string(rawBody), 400))

	groups, err := parsePSCMembersResponse(rawBody)
	if err != nil {
		return nil, fmt.Errorf("parse PSC response for groups list: %w", err)
	}

	log.Printf("  Found %d group(s)", len(groups))
	var groupPrincipals []string
	for _, m := range groups {
		var principal string
		if m.PrincipalName != "" {
			principal = m.PrincipalName
		} else if m.Domain != "" && m.Name != "" {
			principal = m.Domain + "\\" + m.Name
		} else {
			principal = m.Name
		}
		groupPrincipals = append(groupPrincipals, principal)

		// Create a Group node in the graph for each group found
		nodeID := fmt.Sprintf("group:%s:%s", c.Config.Host, principal)
		c.GraphBuilder.EnsureNode([]string{"Group"}, nodeID, map[string]interface{}{
			"name":     principal,
			"domain":   m.Domain,
			"username": m.Name,
			"isGroup":  true,
		})
	}
	return groupPrincipals, nil
}

// buildPSCClient authenticates to the vSphere UI via the SAML SSO flow and returns
// a client with a valid VSPHERE-UI-JSESSIONID cookie.
//
// Flow:
//  1. GET  /ui/login       → intercept redirect → obtain websso URL with SAMLRequest
//  2. GET  websso URL      with CastleAuthorization: Basic header
//                          → vSphere SSO authenticates us and returns HTML form
//                             containing a SAMLResponse assertion
//  3. POST SAMLResponse    to the ACS URL (/ui/saml/websso/sso or similar)
//                          → server validates SAML, sets VSPHERE-UI-JSESSIONID
func (c *Collector) buildPSCClient() (*pscClient, error) {
	proxyFunc := http.ProxyFromEnvironment
	if c.Config.Proxy != "" {
		proxyURL, err := url.Parse(c.Config.Proxy)
		if err != nil {
			return nil, fmt.Errorf("invalid proxy URL %q: %w", c.Config.Proxy, err)
		}
		proxyFunc = http.ProxyURL(proxyURL)
		log.Printf("Using proxy %s for PSC client", c.Config.Proxy)
	}

	transport := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec
		Proxy:           proxyFunc,
	}
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, fmt.Errorf("create cookie jar: %w", err)
	}

	loggedTransport := &loggingTransport{base: transport}

	// Full client that follows redirects and stores cookies.
	client := &http.Client{
		Transport: loggedTransport,
		Jar:       jar,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) > 20 {
				return fmt.Errorf("too many redirects")
			}
			return nil
		},
	}
	// Non-following client for intercepting individual redirects.
	noFollow := &http.Client{
		Transport: loggedTransport,
		Jar:       jar,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	// --- Step 1: GET /ui/login, stop at first redirect ----------------------
	loginURL := fmt.Sprintf("https://%s/ui/login", c.Config.Host)
	resp1, err := noFollow.Get(loginURL)
	if err != nil {
		return nil, fmt.Errorf("GET /ui/login: %w", err)
	}
	io.Copy(io.Discard, resp1.Body)
	resp1.Body.Close()

	webssoURL := resp1.Header.Get("Location")
	if webssoURL == "" {
		// /ui/login returned 200 directly (no redirect) — unusual but handle it.
		log.Printf("  [WARN] /ui/login did not redirect; status=%d", resp1.StatusCode)
		return &pscClient{http: client}, nil
	}
	// Make absolute if relative.
	if !strings.HasPrefix(webssoURL, "http") {
		webssoURL = fmt.Sprintf("https://%s%s", c.Config.Host, webssoURL)
	}
	log.Printf("  Websso URL: %s", truncate(webssoURL, 120))

	// --- Step 2: GET websso with CastleAuthorization ------------------------
	// VMware's SSO endpoint accepts Basic credentials via the CastleAuthorization
	// header, which bypasses the interactive form and returns a SAMLResponse form.
	castleToken := base64.StdEncoding.EncodeToString(
		[]byte(c.Config.User + ":" + c.Config.Password),
	)
	req2, err := http.NewRequestWithContext(c.Context, http.MethodGet, webssoURL, nil)
	if err != nil {
		return nil, fmt.Errorf("build websso request: %w", err)
	}
	req2.Header.Set("CastleAuthorization", "Basic "+castleToken)

	resp2, err := noFollow.Do(req2)
	if err != nil {
		return nil, fmt.Errorf("GET websso with CastleAuthorization: %w", err)
	}
	body2, _ := io.ReadAll(resp2.Body)
	resp2.Body.Close()
	log.Printf("  GET websso → HTTP %d", resp2.StatusCode)
	log.Printf("  websso body (first 1000): %s", truncate(string(body2), 1000))

	switch {
	case resp2.StatusCode == http.StatusFound:
		// Redirect binding: SAMLResponse is in the Location query string.
		acsLocation := resp2.Header.Get("Location")
		log.Printf("  SAML redirect binding → %s", truncate(acsLocation, 120))
		resp3, err := client.Get(acsLocation)
		if err != nil {
			return nil, fmt.Errorf("follow SAML redirect: %w", err)
		}
		io.Copy(io.Discard, resp3.Body)
		resp3.Body.Close()
		log.Printf("  ACS redirect → HTTP %d", resp3.StatusCode)

	case resp2.StatusCode == http.StatusOK:
		// CastleAuthorization as header didn't trigger auth — try as POST body field.
		// Also try username/password form POST. Find the form action in the page.
		log.Printf("  CastleAuthorization header returned login page; retrying as form POST...")

		// Try POST to websso with CastleAuthorization as body field + username/password
		formBody := url.Values{
			"CastleAuthorization": {"Basic " + castleToken},
			"username":            {c.Config.User},
			"password":            {c.Config.Password},
		}
		req3, err := http.NewRequestWithContext(c.Context, http.MethodPost, webssoURL,
			strings.NewReader(formBody.Encode()))
		if err != nil {
			return nil, fmt.Errorf("build form POST: %w", err)
		}
		req3.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req3.Header.Set("Referer", webssoURL)

		resp3, err := noFollow.Do(req3)
		if err != nil {
			return nil, fmt.Errorf("form POST websso: %w", err)
		}
		body3, _ := io.ReadAll(resp3.Body)
		resp3.Body.Close()
		log.Printf("  form POST websso → HTTP %d  Location=%s",
			resp3.StatusCode, resp3.Header.Get("Location"))
		if resp3.StatusCode != http.StatusOK {
			log.Printf("  form POST body (first 500): %s", truncate(string(body3), 500))
		}

		// Use body3 for SAML extraction; if it's a redirect follow it.
		if resp3.StatusCode == http.StatusFound {
			acsLoc := resp3.Header.Get("Location")
			if !strings.HasPrefix(acsLoc, "http") {
				acsLoc = fmt.Sprintf("https://%s%s", c.Config.Host, acsLoc)
			}
			r4, err := client.Get(acsLoc)
			if err != nil {
				return nil, fmt.Errorf("follow POST redirect: %w", err)
			}
			io.Copy(io.Discard, r4.Body)
			r4.Body.Close()
			log.Printf("  POST redirect → HTTP %d", r4.StatusCode)
			break
		}

		// Show form section of the body (look for <form tag context)
		body2 = body3
		if idx := strings.Index(string(body2), "<form"); idx >= 0 {
			end := idx + 1500
			if end > len(body2) {
				end = len(body2)
			}
			log.Printf("  [DEBUG] form section: %s", string(body2)[idx:end])
		}

		// POST binding: server returned HTML page with SAMLResponse hidden form.
		samlResp, acsURL, relayState := extractSAMLForm(string(body2))
		if samlResp == "" {
			log.Printf("  [DEBUG] Could not extract SAMLResponse from websso body")
			log.Printf("  [DEBUG] Full body: %s", truncate(string(body2), 2000))
			return nil, fmt.Errorf("SAMLResponse not found in websso response (HTTP 200)")
		}
		// Make ACS URL absolute.
		if !strings.HasPrefix(acsURL, "http") {
			acsURL = fmt.Sprintf("https://%s%s", c.Config.Host, acsURL)
		}
		log.Printf("  SAML POST binding → ACS=%s", acsURL)

		if err := c.postSAMLResponse(client, acsURL, samlResp, relayState); err != nil {
			return nil, fmt.Errorf("POST SAMLResponse to ACS: %w", err)
		}

	default:
		log.Printf("  [DEBUG] websso body: %s", truncate(string(body2), 500))
		return nil, fmt.Errorf("unexpected websso HTTP status %d", resp2.StatusCode)
	}

	// Check cookies for /ui/ path (JSESSIONID has Path=/ui, not /).
	uiURL, _ := url.Parse(fmt.Sprintf("https://%s/ui/psc-ui/", c.Config.Host))
	hasSession := false
	for _, ck := range jar.Cookies(uiURL) {
		log.Printf("  Cookie[/ui/]: %s=%s", ck.Name, truncate(ck.Value, 60))
		if ck.Name == "VSPHERE-UI-JSESSIONID" {
			hasSession = true
		}
	}
	if !hasSession {
		log.Printf("  [WARN] VSPHERE-UI-JSESSIONID not found — PSC calls may fail")
	} else {
		log.Printf("  vSphere UI session established (JSESSIONID present)")
	}

	// Fetch h5-config to get the clientId (webclientsessionid) and XSRF cookie name.
	// The Angular app always calls this endpoint after login; the returned clientId is
	// what the PSC backend validates as webclientsessionid.
	webClientSessionID, xsrfToken, err := c.fetchH5Config(client, uiURL, jar)
	if err != nil {
		return nil, fmt.Errorf("fetch h5-config: %w", err)
	}
	log.Printf("  webclientsessionid (clientId): %s", webClientSessionID)
	log.Printf("  XSRF token: %s", xsrfToken)

	return &pscClient{http: client, xsrfToken: xsrfToken, webClientSessionID: webClientSessionID, jar: jar}, nil
}

// fetchH5Config calls /ui/config/h5-config?debug=false to obtain the clientId
// (used as webclientsessionid) and sets the XSRF cookie in the jar.
func (c *Collector) fetchH5Config(client *http.Client, uiURL *url.URL, jar *cookiejar.Jar) (clientID, xsrfToken string, err error) {
	endpoint := fmt.Sprintf("https://%s/ui/config/h5-config?debug=false", c.Config.Host)
	req, err := http.NewRequestWithContext(c.Context, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", "", err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Referer", fmt.Sprintf("https://%s/ui/", c.Config.Host))

	resp, err := client.Do(req)
	if err != nil {
		return "", "", err
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("h5-config HTTP %d: %s", resp.StatusCode, truncate(string(body), 200))
	}

	var cfg struct {
		ClientID       string `json:"clientId"`
		XsrfCookieName string `json:"xsrfCookieName"`
	}
	if err := json.Unmarshal(body, &cfg); err != nil {
		return "", "", fmt.Errorf("parse h5-config: %w", err)
	}
	if cfg.ClientID == "" {
		return "", "", fmt.Errorf("h5-config missing clientId")
	}

	xsrfCookieName := cfg.XsrfCookieName
	if xsrfCookieName == "" {
		xsrfCookieName = "VSPHERE-UI-XSRF-TOKEN"
	}

	// Generate XSRF token and plant it as a cookie (double-submit pattern).
	xsrfToken = uuid.New().String()
	jar.SetCookies(uiURL, []*http.Cookie{{
		Name:   xsrfCookieName,
		Value:  xsrfToken,
		Path:   "/ui",
		Secure: true,
	}})

	return cfg.ClientID, xsrfToken, nil
}

// postSAMLResponse POSTs the SAMLResponse assertion to the ACS endpoint.
func (c *Collector) postSAMLResponse(client *http.Client, acsURL, samlResp, relayState string) error {
	form := url.Values{
		"SAMLResponse": {samlResp},
		"RelayState":   {relayState},
	}
	req, err := http.NewRequestWithContext(
		c.Context, http.MethodPost, acsURL,
		strings.NewReader(form.Encode()),
	)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	log.Printf("  POST ACS → HTTP %d  finalURL=%s", resp.StatusCode, resp.Request.URL)
	log.Printf("  ACS body (first 500): %s", truncate(string(body), 500))
	return nil
}

// extractSAMLForm parses an HTML page and extracts the auto-submit SAML form fields.
func extractSAMLForm(html string) (samlResponse, acsURL, relayState string) {
	// ACS form action
	if m := regexp.MustCompile(`<form[^>]+action="([^"]+)"`).FindStringSubmatch(html); len(m) >= 2 {
		acsURL = m[1]
	}
	// Try value before name and name before value ordering
	for _, re := range []*regexp.Regexp{
		regexp.MustCompile(`name="SAMLResponse"[^>]*value="([^"]+)"`),
		regexp.MustCompile(`value="([^"]+)"[^>]*name="SAMLResponse"`),
	} {
		if m := re.FindStringSubmatch(html); len(m) >= 2 {
			samlResponse = m[1]
			break
		}
	}
	for _, re := range []*regexp.Regexp{
		regexp.MustCompile(`name="RelayState"[^>]*value="([^"]*)"`),
		regexp.MustCompile(`value="([^"]*)"[^>]*name="RelayState"`),
	} {
		if m := re.FindStringSubmatch(html); len(m) >= 2 {
			relayState = m[1]
			break
		}
	}
	return
}

// fetchPSCGroupMembers calls the PSC REST endpoint and adds members to the graph.
func (c *Collector) fetchPSCGroupMembers(psc *pscClient, groupQuery, parentGID string) error {
	endpoint := fmt.Sprintf(
		"https://%s/ui/psc-ui/ctrl/psc/tenant/groupmembers/all?groupname=%s",
		c.Config.Host,
		url.QueryEscape(groupQuery),
	)

	req, err := http.NewRequestWithContext(c.Context, http.MethodGet, endpoint, nil)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Accept", "application/json, text/plain, */*")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	req.Header.Set("webclientsessionid", psc.webClientSessionID)
	req.Header.Set("Origin", fmt.Sprintf("https://%s", c.Config.Host))
	req.Header.Set("Referer", fmt.Sprintf("https://%s/ui/", c.Config.Host))
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36")
	if psc.xsrfToken != "" {
		req.Header.Set("X-VSPHERE-UI-XSRF-TOKEN", psc.xsrfToken)
		req.Header.Set("X-XSRF-TOKEN", psc.xsrfToken)
	}

	resp, err := psc.http.Do(req)
	if err != nil {
		return fmt.Errorf("HTTP GET: %w", err)
	}
	defer resp.Body.Close()

	rawBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("PSC API HTTP %d for %s: %s",
			resp.StatusCode, groupQuery, truncate(string(rawBody), 200))
	}

	log.Printf("  PSC raw response for %s: %s", groupQuery, truncate(string(rawBody), 400))

	members, err := parsePSCMembersResponse(rawBody)
	if err != nil {
		return fmt.Errorf("parse PSC response for %s: %w", groupQuery, err)
	}

	log.Printf("  Found %d member(s) in %s", len(members), groupQuery)
	for _, m := range members {
		c.addPSCMemberToGraph(m, parentGID)
	}
	return nil
}

func parsePSCMembersResponse(data []byte) ([]pscMember, error) {
	var members []pscMember
	if err := json.Unmarshal(data, &members); err == nil {
		return members, nil
	}
	var wrapper map[string]json.RawMessage
	if err := json.Unmarshal(data, &wrapper); err != nil {
		return nil, fmt.Errorf("neither array nor object: %w", err)
	}

	mergedList := []pscMember{}
	foundKey := false

	for _, key := range []string{"groups", "users", "members", "values", "items", "data"} {
		if raw, ok := wrapper[key]; ok {
			foundKey = true
			var list []pscMember
			if err := json.Unmarshal(raw, &list); err == nil {
				mergedList = append(mergedList, list...)
			}
		}
	}

	// If no keys were found but the object parsed correctly, it might just be empty, like {}
	if !foundKey && len(wrapper) == 0 {
		return mergedList, nil
	}

	// If we found at least one of our expected keys, return the aggregate
	if foundKey {
		return mergedList, nil
	}

	return nil, fmt.Errorf("unrecognised PSC response: %s", truncate(string(data), 200))
}

func (c *Collector) addPSCMemberToGraph(member pscMember, parentGID string) {
	name := member.Name
	domain := member.Domain

	memberKind := strings.ToLower(member.Kind)
	if memberKind == "" {
		memberKind = strings.ToLower(member.Type)
	}
	isGroup := strings.Contains(memberKind, "group")

	var principal string
	switch {
	case member.PrincipalName != "":
		principal = member.PrincipalName
	case domain != "" && name != "":
		principal = domain + "\\" + name
	default:
		principal = name
	}

	var nodeID string
	var kinds []string
	if isGroup {
		nodeID = fmt.Sprintf("group:%s:%s", c.Config.Host, principal)
		kinds = []string{"Group"}
	} else {
		nodeID = fmt.Sprintf("user:%s:%s", c.Config.Host, principal)
		kinds = []string{"User"}
	}

	c.GraphBuilder.EnsureNode(kinds, nodeID, map[string]interface{}{
		"name":     principal,
		"domain":   domain,
		"username": name,
		"isGroup":  isGroup,
	})
	c.GraphBuilder.AddEdge("MEMBER_OF", nodeID, parentGID, nil)
}

func principalToAtFormat(principal string) string {
	if strings.Contains(principal, "\\") {
		parts := strings.SplitN(principal, "\\", 2)
		if len(parts) == 2 {
			// PSC API expects lowercase domain: "name@domain.local"
			return parts[1] + "@" + strings.ToLower(parts[0])
		}
	}
	return strings.ToLower(principal)
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
