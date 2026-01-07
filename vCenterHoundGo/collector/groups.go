package collector

import (
	"fmt"
	"log"
	"strings"

	"github.com/vmware/govmomi/vim25/methods"
	"github.com/vmware/govmomi/vim25/mo"
	"github.com/vmware/govmomi/vim25/types"
)

// CollectGroupMemberships collects user directory and group memberships
func (c *Collector) CollectGroupMemberships() error {
	// Retrieve UserDirectory
	userDirRef := c.Client.ServiceContent.UserDirectory
	if userDirRef == nil {
		return fmt.Errorf("no user directory found")
	}

	// We can't really "Retrieve" properties of UserDirectory as it's a manager mostly with methods.
	// But it has `domainList` property.
	var userDir mo.UserDirectory
	_ = c.Client.RetrieveOne(c.Context, *userDirRef, []string{"domainList"}, &userDir)

	domains := userDir.DomainList

	// Get all groups with permissions to scan
	// We need to re-fetch permissions or store them?
	// The Python script re-fetches permissions or iterates valid ones.
	// We can iterate the graph for "Group" nodes or just re-fetch permissions.
	// Simpler to re-run RetrieveAllPermissions or cache it.

	perms, _ := c.retrieveAllPermissions(*c.Client.ServiceContent.AuthorizationManager)

	groupsWithPermissions := make(map[string]bool)
	for _, perm := range perms {
		if perm.Group {
			groupsWithPermissions[perm.Principal] = true
		}
	}

	for groupPrincipal := range groupsWithPermissions {
		parentGID := fmt.Sprintf("group:%s:%s", c.Config.Host, groupPrincipal)

		for _, domain := range domains {
			// Find users in group
			c.findMembers(groupPrincipal, domain, parentGID, true)
			// Find nested groups
			c.findMembers(groupPrincipal, domain, parentGID, false)
		}

		// Try without domain prefix if needed
		if strings.Contains(groupPrincipal, "\\") {
			parts := strings.SplitN(groupPrincipal, "\\", 2)
			if len(parts) == 2 {
				groupNameOnly := parts[1]
				for _, domain := range domains {
					c.findMembers(groupNameOnly, domain, parentGID, true)
					c.findMembers(groupNameOnly, domain, parentGID, false)
				}
			}
		}
	}

	return nil
}

func (c *Collector) findMembers(groupPrincipal string, domain string, parentGID string, findUsers bool) {
	req := types.RetrieveUserGroups{
		This:           *c.Client.ServiceContent.UserDirectory,
		Domain:         domain,
		SearchStr:      "",
		ExactMatch:     false,
		FindUsers:      findUsers,
		FindGroups:     !findUsers,
		BelongsToGroup: groupPrincipal,
	}

	res, err := methods.RetrieveUserGroups(c.Context, c.Client.Client, &req)
	if err != nil {
		log.Printf("Error retrieving user groups for group %s in domain %s: %v", groupPrincipal, domain, err)
		return
	}

	for _, baseResult := range res.Returnval {
		var result *types.UserSearchResult
		if r, ok := baseResult.(*types.UserSearchResult); ok {
			result = r
		} else {
			continue
		}

		domain, username := c.parsePrincipal(result.Principal)

		var id string
		var kinds []string
		var isGroup bool

		if findUsers {
			id = fmt.Sprintf("user:%s:%s", c.Config.Host, result.Principal)
			kinds = []string{"User"}
			isGroup = false
		} else {
			id = fmt.Sprintf("group:%s:%s", c.Config.Host, result.Principal)
			kinds = []string{"Group"}
			isGroup = true
		}

		c.GraphBuilder.EnsureNode(kinds, id, map[string]interface{}{
			"name":     result.Principal,
			"domain":   domain,
			"username": username,
			"isGroup":  isGroup,
		})

		// Add MEMBER_OF edge
		c.GraphBuilder.AddEdge("MEMBER_OF", id, parentGID, nil)
	}
}

func (c *Collector) parsePrincipal(principal string) (string, string) {
	if strings.Contains(principal, "\\") {
		parts := strings.SplitN(principal, "\\", 2)
		return parts[0], parts[1]
	}
	if strings.Contains(principal, "@") {
		parts := strings.SplitN(principal, "@", 2)
		return parts[1], parts[0]
	}
	return "", principal
}
