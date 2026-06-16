package collector

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"
	"vCenterHoundGo/graph"

	"github.com/vmware/govmomi"
	"github.com/vmware/govmomi/session"
	"github.com/vmware/govmomi/vim25"
	"github.com/vmware/govmomi/vim25/soap"
	"github.com/vmware/govmomi/vim25/types"
)

// VCenterConfig holds connection information
type VCenterConfig struct {
	Host     string
	User     string
	Password string
	Port     int
	Proxy    string // optional, e.g. "http://proxy.corp:8080"
}

// Collector holds the state for data collection
type Collector struct {
	Config       VCenterConfig
	Client       *govmomi.Client
	GraphBuilder *graph.GraphBuilder
	Context      context.Context
	Roles        map[int32]types.AuthorizationRole
	Privileges   map[string]types.AuthorizationPrivilege
}

// NewCollector creates a new Collector
func NewCollector(config VCenterConfig, gb *graph.GraphBuilder) *Collector {
	return &Collector{
		Config:       config,
		GraphBuilder: gb,
		Context:      context.Background(),
		Roles:        make(map[int32]types.AuthorizationRole),
		Privileges:   make(map[string]types.AuthorizationPrivilege),
	}
}

// Connect establishes connection to vCenter
func (c *Collector) Connect() error {
	u, err := url.Parse(fmt.Sprintf("https://%s:%d/sdk", c.Config.Host, c.Config.Port))
	if err != nil {
		return err
	}
	u.User = url.UserPassword(c.Config.User, c.Config.Password)

	soapClient := soap.NewClient(u, true)

	if c.Config.Proxy != "" {
		proxyURL, err := url.Parse(c.Config.Proxy)
		if err != nil {
			return fmt.Errorf("invalid proxy URL %q: %w", c.Config.Proxy, err)
		}
		soapClient.DefaultTransport().Proxy = http.ProxyURL(proxyURL)
		log.Printf("Using proxy %s for govmomi connection", c.Config.Proxy)
	}

	vim25Client, err := vim25.NewClient(c.Context, soapClient)
	if err != nil {
		return err
	}

	c.Client = &govmomi.Client{
		Client:         vim25Client,
		SessionManager: session.NewManager(vim25Client),
	}

	if err := c.Client.Login(c.Context, u.User); err != nil {
		if strings.Contains(err.Error(), "incorrect user name or password") {
			return fmt.Errorf("authentication failed: incorrect user name or password for %s@%s", c.Config.User, c.Config.Host)
		}
		return err
	}

	return nil
}

// Disconnect closes the connection
func (c *Collector) Disconnect() {
	if c.Client != nil {
		_ = c.Client.Logout(c.Context)
	}
}

// Collect orchestrates the data collection
func (c *Collector) Collect() error {
	if err := c.Connect(); err != nil {
		return err
	}
	defer c.Disconnect()

	log.Printf("Connected to %s", c.Config.Host)

	log.Println("Collecting infrastructure data...")
	if err := c.CollectInfrastructure(); err != nil {
		log.Printf("Error collecting infrastructure: %v", err)
		// Continue anyway? Use python behavior: it seems to continue.
	}

	log.Println("Collecting group memberships...")
	if err := c.CollectGroupMemberships(); err != nil {
		log.Printf("Error collecting group memberships: %v", err)
	}

	log.Println("Collecting permissions...")
	if err := c.CollectPermissions(); err != nil {
		log.Printf("Error collecting permissions: %v", err)
	}

	return nil
}

// Helper to get Managed Object ID (MOID)
func (c *Collector) getMOID(obj interface{}) string {
	// This helper might need REF access. Most govmomi objects have Reference().Value
	if ref, ok := obj.(types.ManagedObjectReference); ok {
		return ref.Value
	}
	if obj == nil {
		return "unknown"
	}
	// Attempt to find Reference() method via interface?
	// Or expect `mo` types which have Reference()
	// For now, assume we pass strings or handle specific types in callers.
	return "unknown"
}

// Helper to get MOID from reference
func getID(ref types.ManagedObjectReference) string {
	return ref.Value
}
