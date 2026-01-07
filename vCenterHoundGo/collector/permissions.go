package collector

import (
	"fmt"
	"log"
	"sort"
	"strings"

	"github.com/vmware/govmomi/vim25/methods"
	"github.com/vmware/govmomi/vim25/mo"
	"github.com/vmware/govmomi/vim25/types"
)

// CollectPermissions collects roles and permissions
func (c *Collector) CollectPermissions() error {
	// Authorization Manager
	authManagerRef := c.Client.ServiceContent.AuthorizationManager
	if authManagerRef == nil {
		return fmt.Errorf("no authorization manager found")
	}

	var authManager mo.AuthorizationManager
	err := c.Client.RetrieveOne(c.Context, *authManagerRef, []string{"roleList", "privilegeList"}, &authManager)
	// Note: "permission" property might not be populated directly on AuthorizationManager object in some versions?
	// The Python script calls RetrieveAllPermissions(). govmomi might need us to call the method or read property?
	// The property on AuthorizationManager is 'privilegeList' and 'roleList'.
	// 'permission' is likely NOT a property of AuthorizationManager but retrieved via method.
	// But `RetrieveAllPermissions` IS a method on AuthorizationManager.

	if err != nil {
		return err
	}

	// Retrieve permissions via method call (since it might return a lot)
	// We need to use object.AuthorizationManager wrapper or call raw RetrieveAllPermissions
	// Using raw SOAP call via Methods if needed, or higher level if available.
	// govmomi `vim25/methods` has RetrieveRetrieveAllPermissions.
	// Let's rely on `authManager` if we can, or just call the method.

	// Re-fetch permissions properly
	// Actually `RetrieveAllPermissions` takes `This` reference.
	// We construct the request.

	// 1. Process Privileges
	privMap := make(map[string]types.AuthorizationPrivilege)
	for _, p := range authManager.PrivilegeList {
		privMap[p.PrivId] = p
		// Create Privilege Node
		privID := fmt.Sprintf("privilege:%s:%s", c.Config.Host, p.PrivId)
		c.GraphBuilder.EnsureNode([]string{"Privilege"}, privID, map[string]interface{}{
			"privId": p.PrivId,
			"name":   p.Name,
			"group":  p.PrivGroupName,
		})
	}

	// 2. Process Roles
	rolesByID := make(map[int32]types.AuthorizationRole)
	for _, role := range authManager.RoleList {
		rolesByID[role.RoleId] = role
		roleID := fmt.Sprintf("role:%s:%d", c.Config.Host, role.RoleId)

		privGroups := make(map[string]bool)
		for _, pid := range role.Privilege {
			if p, ok := privMap[pid]; ok {
				privGroups[p.PrivGroupName] = true
				// Edge Role HAS_PRIVILEGE Privilege
				pNodeID := fmt.Sprintf("privilege:%s:%s", c.Config.Host, pid)
				c.GraphBuilder.AddEdge("HAS_PRIVILEGE", roleID, pNodeID, nil)
			}
		}

		pgList := make([]string, 0, len(privGroups))
		for k := range privGroups {
			pgList = append(pgList, k)
		}

		c.GraphBuilder.EnsureNode([]string{"Role"}, roleID, map[string]interface{}{
			"roleId":          role.RoleId,
			"name":            role.Name,
			"privilegeCount":  len(role.Privilege),
			"privilegeGroups": pgList,
		})
	}

	// 3. Process Permissions
	// We need to call RetrieveAllPermissions
	perms, err := c.retrieveAllPermissions(*authManagerRef)
	if err != nil {
		log.Printf("Failed to retrieve permissions: %v", err)
		// Proceed anyway
	}

	for _, perm := range perms {
		c.processPermission(perm, rolesByID, privMap)
	}

	return nil
}

// retrieveAllPermissions helper
// Depending on govmomi version, we can use methods.RetrieveAllPermissions
func (c *Collector) retrieveAllPermissions(am types.ManagedObjectReference) ([]types.Permission, error) {
	req := types.RetrieveAllPermissions{This: am}
	res, err := methods.RetrieveAllPermissions(c.Context, c.Client.Client, &req)
	if err != nil {
		return nil, err
	}
	return res.Returnval, nil
}

func (c *Collector) processPermission(perm types.Permission, roles map[int32]types.AuthorizationRole, privMap map[string]types.AuthorizationPrivilege) {
	principal := perm.Principal
	isGroup := perm.Group

	domain, username := parsePrincipal(principal)

	var principalID string
	var kinds []string
	if isGroup {
		principalID = fmt.Sprintf("group:%s:%s", c.Config.Host, principal)
		kinds = []string{"Group"}
	} else {
		principalID = fmt.Sprintf("user:%s:%s", c.Config.Host, principal)
		kinds = []string{"User"}
	}

	c.GraphBuilder.EnsureNode(kinds, principalID, map[string]interface{}{
		"name":     principal,
		"isGroup":  isGroup,
		"domain":   domain,
		"username": username,
	})

	// Role Info
	roleID := perm.RoleId
	roleName := ""

	var privIds []string
	var privNames []string
	var privGroups []string
	privCount := 0

	if r, ok := roles[roleID]; ok {
		roleName = r.Name

		// Populate privilege details
		privIds = r.Privilege
		privCount = len(r.Privilege)

		// Collect names and groups (unique groups)
		groupSet := make(map[string]bool)
		for _, pid := range r.Privilege {
			if p, found := privMap[pid]; found {
				privNames = append(privNames, p.Name)
				groupSet[p.PrivGroupName] = true
			} else {
				// Fallback if privilege not found in map (shouldn't happen usually)
				privNames = append(privNames, pid)
			}
		}
		for g := range groupSet {
			privGroups = append(privGroups, g)
		}
		sort.Strings(privGroups)
	}

	if isNoAccess(roleName) {
		return
	}

	// Entity
	entityRef := perm.Entity
	if entityRef == nil {
		return
	}

	entityKind, entityID := c.getEntityKindAndID(*entityRef)
	// Ensure entity exists? It might not if we failed to collect it (e.g. unknown type)
	// But we should create a placeholder node at least?
	// Python script ensures node with basic info.

	// Check if node exists to avoid overwriting valid names (like "Datacenters") with MOID ("group-d1")
	// If the node exists, EnsureNode with empty/minimal props will just update Kinds
	// Creating properties map
	nodeProps := map[string]interface{}{}

	// Since EnsureNode overwrites properties, we must be careful.
	// We want to set 'moid' if missing, but not overwrite 'name' if it exists.
	// EnsureNode logic: if exists, overwrite properties.
	// So if we pass "name": MOID, it overwrites.
	// We should check if node exists in GraphBuilder.

	_, exists := c.GraphBuilder.NodesByID[entityID]
	if !exists {
		// Node doesn't exist, set name = MOID as placeholder
		nodeProps["moid"] = getID(*entityRef)
		nodeProps["name"] = getID(*entityRef)
	} else {
		// Node exists.
		// If it was created but has no properties (unlikely), we might want to set MOID.
		// But usually Infrastructure collection runs first and sets correct names.
		// So we do NOT put "name" in nodeProps.
		// We can put "moid" just in case.
		nodeProps["moid"] = getID(*entityRef)

		// However, if the existing node has no "name" property (corner case), we might want to set it?
		// But let's trust Infrastructure collection.
		// If we omit "name" from nodeProps, EnsureNode won't touch existing "name".
	}

	c.GraphBuilder.EnsureNode([]string{entityKind}, entityID, nodeProps)

	// Edge properties
	props := map[string]interface{}{
		"roleId":          roleID,
		"roleName":        roleName,
		"propagate":       perm.Propagate,
		"privilegeIds":    privIds,
		"privilegeNames":  privNames,
		"privilegeGroups": privGroups,
		"privilegeCount":  privCount,
	}

	c.GraphBuilder.AddEdge("HAS_PERMISSION", principalID, entityID, props)
}

func parsePrincipal(principal string) (string, string) {
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

func isNoAccess(roleName string) bool {
	l := strings.ToLower(roleName)
	return l == "no access" || l == "noaccess" || l == "no-access"
}

func (c *Collector) getEntityKindAndID(ref types.ManagedObjectReference) (string, string) {
	// Map types to our Kinds
	// Reuse logic from Python `_get_entity_kind_and_id`
	moid := getID(ref)

	// Simplification:
	switch ref.Type {
	case "Datacenter":
		return "Datacenter", fmt.Sprintf("datacenter:%s:%s", c.Config.Host, moid)
	case "ClusterComputeResource":
		return "Cluster", fmt.Sprintf("cluster:%s:%s", c.Config.Host, moid)
	case "HostSystem":
		return "ESXiHost", fmt.Sprintf("esxi_host:%s:%s", c.Config.Host, moid)
	case "ComputeResource":
		// Python logic: if hosts exist, use first host moid and type ESXiHost
		// We need to fetch the property 'host'
		var cr mo.ComputeResource
		err := c.Client.RetrieveOne(c.Context, ref, []string{"host"}, &cr)
		if err == nil && len(cr.Host) > 0 {
			hostMoid := cr.Host[0].Value
			return "ESXiHost", fmt.Sprintf("esxi_host:%s:%s", c.Config.Host, hostMoid)
		}
		// Fallback if no host found (unlikely) or valid empty ComputeResource?
		// Python fallback is: return "ESXiHost", f"esxi_host:{self.config.host}:{moid}"
		return "ESXiHost", fmt.Sprintf("esxi_host:%s:%s", c.Config.Host, moid)
	case "VirtualMachine":
		return "VM", fmt.Sprintf("vm:%s:%s", c.Config.Host, moid)
	case "Folder":
		return "Folder", fmt.Sprintf("folder:%s:%s", c.Config.Host, moid)
	}
	// Fallback
	cleanType := ref.Type
	return cleanType, fmt.Sprintf("%s:%s:%s", strings.ToLower(cleanType), c.Config.Host, moid)
}
