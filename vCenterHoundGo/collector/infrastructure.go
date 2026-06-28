package collector

import (
	"fmt"
	"log"
	"sort"

	"github.com/vmware/govmomi/view"
	"github.com/vmware/govmomi/vim25/mo"
	"github.com/vmware/govmomi/vim25/types"
)

// infraCache holds all prefetched vCenter objects keyed by MOID.
type infraCache struct {
	Folders     map[string]mo.Folder
	Datacenters map[string]mo.Datacenter
	Clusters    map[string]mo.ClusterComputeResource
	Computes    map[string]mo.ComputeResource
	Hosts       map[string]mo.HostSystem
	VMs         map[string]mo.VirtualMachine
	Datastores  map[string]mo.Datastore
	Networks    map[string]mo.Network
	ResPools    map[string]mo.ResourcePool
}

// prefetchAll batch-fetches every object type sequentially via ContainerView.
// This replaces O(N) individual RetrieveOne calls with ~9 bulk requests.
func (c *Collector) prefetchAll() (*infraCache, error) {
	m := view.NewManager(c.Client.Client)
	root := c.Client.ServiceContent.RootFolder

	cache := &infraCache{
		Folders:     make(map[string]mo.Folder),
		Datacenters: make(map[string]mo.Datacenter),
		Clusters:    make(map[string]mo.ClusterComputeResource),
		Computes:    make(map[string]mo.ComputeResource),
		Hosts:       make(map[string]mo.HostSystem),
		VMs:         make(map[string]mo.VirtualMachine),
		Datastores:  make(map[string]mo.Datastore),
		Networks:    make(map[string]mo.Network),
		ResPools:    make(map[string]mo.ResourcePool),
	}

	fetch := func(objType string, props []string) (*view.ContainerView, error) {
		v, err := m.CreateContainerView(c.Context, root, []string{objType}, true)
		if err != nil {
			return nil, fmt.Errorf("create view for %s: %w", objType, err)
		}
		return v, nil
	}

	// Folders
	if v, err := fetch("Folder", nil); err == nil {
		var objs []mo.Folder
		if err := v.Retrieve(c.Context, []string{"Folder"}, []string{"name", "childEntity"}, &objs); err != nil {
			log.Printf("Warning: prefetch Folder: %v", err)
		}
		v.Destroy(c.Context)
		for _, o := range objs {
			cache.Folders[o.Self.Value] = o
		}
	} else {
		log.Printf("Warning: %v", err)
	}

	// Datacenters
	if v, err := fetch("Datacenter", nil); err == nil {
		var objs []mo.Datacenter
		if err := v.Retrieve(c.Context, []string{"Datacenter"}, []string{"name", "hostFolder", "vmFolder", "datastore", "network"}, &objs); err != nil {
			log.Printf("Warning: prefetch Datacenter: %v", err)
		}
		v.Destroy(c.Context)
		for _, o := range objs {
			cache.Datacenters[o.Self.Value] = o
		}
	} else {
		log.Printf("Warning: %v", err)
	}

	// Clusters
	if v, err := fetch("ClusterComputeResource", nil); err == nil {
		var objs []mo.ClusterComputeResource
		if err := v.Retrieve(c.Context, []string{"ClusterComputeResource"}, []string{"name", "host", "datastore", "network", "resourcePool", "summary", "configuration"}, &objs); err != nil {
			log.Printf("Warning: prefetch ClusterComputeResource: %v", err)
		}
		v.Destroy(c.Context)
		for _, o := range objs {
			cache.Clusters[o.Self.Value] = o
		}
	} else {
		log.Printf("Warning: %v", err)
	}

	// Standalone ComputeResource (non-cluster)
	if v, err := fetch("ComputeResource", nil); err == nil {
		var objs []mo.ComputeResource
		if err := v.Retrieve(c.Context, []string{"ComputeResource"}, []string{"host"}, &objs); err != nil {
			log.Printf("Warning: prefetch ComputeResource: %v", err)
		}
		v.Destroy(c.Context)
		for _, o := range objs {
			// Skip ClusterComputeResource (it's a subtype returned by ComputeResource view)
			if o.Self.Type == "ComputeResource" {
				cache.Computes[o.Self.Value] = o
			}
		}
	} else {
		log.Printf("Warning: %v", err)
	}

	// Hosts
	if v, err := fetch("HostSystem", nil); err == nil {
		var objs []mo.HostSystem
		if err := v.Retrieve(c.Context, []string{"HostSystem"}, []string{"name", "summary", "vm", "datastore", "network"}, &objs); err != nil {
			log.Printf("Warning: prefetch HostSystem: %v", err)
		}
		v.Destroy(c.Context)
		for _, o := range objs {
			cache.Hosts[o.Self.Value] = o
		}
	} else {
		log.Printf("Warning: %v", err)
	}

	// VMs
	if v, err := fetch("VirtualMachine", nil); err == nil {
		var objs []mo.VirtualMachine
		if err := v.Retrieve(c.Context, []string{"VirtualMachine"}, []string{"name", "config", "guest", "runtime", "summary", "datastore", "network"}, &objs); err != nil {
			log.Printf("Warning: prefetch VirtualMachine: %v", err)
		}
		v.Destroy(c.Context)
		for _, o := range objs {
			cache.VMs[o.Self.Value] = o
		}
	} else {
		log.Printf("Warning: %v", err)
	}

	// Datastores
	if v, err := fetch("Datastore", nil); err == nil {
		var objs []mo.Datastore
		if err := v.Retrieve(c.Context, []string{"Datastore"}, []string{"name", "summary"}, &objs); err != nil {
			log.Printf("Warning: prefetch Datastore: %v", err)
		}
		v.Destroy(c.Context)
		for _, o := range objs {
			cache.Datastores[o.Self.Value] = o
		}
	} else {
		log.Printf("Warning: %v", err)
	}

	// Networks
	if v, err := fetch("Network", nil); err == nil {
		var objs []mo.Network
		if err := v.Retrieve(c.Context, []string{"Network"}, []string{"name", "summary"}, &objs); err != nil {
			log.Printf("Warning: prefetch Network: %v", err)
		}
		v.Destroy(c.Context)
		for _, o := range objs {
			cache.Networks[o.Self.Value] = o
		}
	} else {
		log.Printf("Warning: %v", err)
	}

	// Resource Pools
	if v, err := fetch("ResourcePool", nil); err == nil {
		var objs []mo.ResourcePool
		if err := v.Retrieve(c.Context, []string{"ResourcePool"}, []string{"name", "resourcePool"}, &objs); err != nil {
			log.Printf("Warning: prefetch ResourcePool: %v", err)
		}
		v.Destroy(c.Context)
		for _, o := range objs {
			cache.ResPools[o.Self.Value] = o
		}
	} else {
		log.Printf("Warning: %v", err)
	}

	log.Printf("Prefetch complete: %d folders, %d datacenters, %d clusters, %d compute, %d hosts, %d VMs, %d datastores, %d networks, %d resource pools",
		len(cache.Folders), len(cache.Datacenters), len(cache.Clusters), len(cache.Computes),
		len(cache.Hosts), len(cache.VMs), len(cache.Datastores),
		len(cache.Networks), len(cache.ResPools))

	return cache, nil
}

// CollectInfrastructure collects all infrastructure data using batch prefetch.
func (c *Collector) CollectInfrastructure() error {
	log.Println("Prefetching all objects from vCenter...")
	cache, err := c.prefetchAll()
	if err != nil {
		return err
	}

	vcenterID := fmt.Sprintf("vcenter:%s", c.Config.Host)
	c.GraphBuilder.EnsureNode([]string{"vCenter"}, vcenterID, map[string]interface{}{"name": c.Config.Host})
	log.Printf("Added vCenter node: %s", c.Config.Host)

	// Root folder is the container itself, so it's excluded from ContainerView results.
	// Fetch it directly (single RetrieveOne for just this one object).
	rootRef := c.Client.ServiceContent.RootFolder
	var rootFolder mo.Folder
	if err := c.Client.RetrieveOne(c.Context, rootRef, []string{"name", "childEntity"}, &rootFolder); err != nil {
		return fmt.Errorf("failed to retrieve root folder: %w", err)
	}

	rootID := fmt.Sprintf("folder:%s:%s", c.Config.Host, rootRef.Value)
	c.GraphBuilder.EnsureNode([]string{"RootFolder", "Folder"}, rootID, map[string]interface{}{
		"name": rootFolder.Name,
		"moid": rootRef.Value,
	})
	c.GraphBuilder.AddEdge("CONTAINS", vcenterID, rootID, nil)

	for _, child := range rootFolder.ChildEntity {
		switch child.Type {
		case "Datacenter":
			c.processDCFromCache(child, rootID, cache)
		case "Folder":
			c.processFolderFromCache(child, rootID, cache)
		}
	}

	return nil
}

func (c *Collector) processFolderFromCache(ref types.ManagedObjectReference, parentID string, cache *infraCache) {
	folder, ok := cache.Folders[ref.Value]
	if !ok {
		return
	}

	folderID := fmt.Sprintf("folder:%s:%s", c.Config.Host, ref.Value)
	c.GraphBuilder.EnsureNode([]string{"Folder"}, folderID, map[string]interface{}{
		"name": folder.Name,
		"moid": ref.Value,
	})
	c.GraphBuilder.AddEdge("CONTAINS", parentID, folderID, nil)

	for _, child := range folder.ChildEntity {
		switch child.Type {
		case "Datacenter":
			c.processDCFromCache(child, folderID, cache)
		case "Folder":
			c.processFolderFromCache(child, folderID, cache)
		case "VirtualMachine":
			c.processVMFromCache(child, folderID, cache)
		}
	}
}

func (c *Collector) processDCFromCache(ref types.ManagedObjectReference, parentID string, cache *infraCache) {
	dc, ok := cache.Datacenters[ref.Value]
	if !ok {
		return
	}

	dcID := fmt.Sprintf("datacenter:%s:%s", c.Config.Host, ref.Value)
	c.GraphBuilder.EnsureNode([]string{"Datacenter"}, dcID, map[string]interface{}{
		"name": dc.Name,
		"moid": ref.Value,
	})
	c.GraphBuilder.AddEdge("CONTAINS", parentID, dcID, nil)
	log.Printf("Processing Datacenter: %s", dc.Name)

	c.processHostFolderFromCache(dc.HostFolder, dcID, cache)
	c.processVMFolderFromCache(dc.VmFolder, dcID, cache)

	for _, ds := range dc.Datastore {
		c.processDatastoreFromCache(ds, "", cache)
	}
	for _, net := range dc.Network {
		c.processNetworkFromCache(net, "", cache)
	}
}

func (c *Collector) processHostFolderFromCache(ref types.ManagedObjectReference, parentID string, cache *infraCache) {
	folder, ok := cache.Folders[ref.Value]
	if !ok {
		return
	}
	for _, child := range folder.ChildEntity {
		switch child.Type {
		case "Folder":
			c.processComputeFolderFromCache(child, parentID, cache)
		case "ClusterComputeResource":
			c.processClusterFromCache(child, parentID, cache)
		case "ComputeResource":
			c.processComputeResourceFromCache(child, parentID, cache)
		case "HostSystem":
			c.processHostFromCache(child, parentID, false, cache)
		}
	}
}

func (c *Collector) processComputeFolderFromCache(ref types.ManagedObjectReference, parentID string, cache *infraCache) {
	folder, ok := cache.Folders[ref.Value]
	if !ok {
		return
	}

	folderID := fmt.Sprintf("folder:%s:%s", c.Config.Host, ref.Value)
	c.GraphBuilder.EnsureNode([]string{"Folder"}, folderID, map[string]interface{}{
		"name": folder.Name,
		"moid": ref.Value,
	})
	c.GraphBuilder.AddEdge("CONTAINS", parentID, folderID, nil)

	for _, child := range folder.ChildEntity {
		switch child.Type {
		case "Folder":
			c.processComputeFolderFromCache(child, folderID, cache)
		case "ClusterComputeResource":
			c.processClusterFromCache(child, folderID, cache)
		case "ComputeResource":
			c.processComputeResourceFromCache(child, folderID, cache)
		}
	}
}

func (c *Collector) processClusterFromCache(ref types.ManagedObjectReference, parentID string, cache *infraCache) {
	cluster, ok := cache.Clusters[ref.Value]
	if !ok {
		return
	}

	clusterID := fmt.Sprintf("cluster:%s:%s", c.Config.Host, ref.Value)
	props := map[string]interface{}{
		"name": cluster.Name,
		"moid": ref.Value,
	}
	if cluster.Summary != nil {
		s := cluster.Summary.GetComputeResourceSummary()
		props["totalCpu"] = s.TotalCpu
		props["totalMemory"] = s.TotalMemory
		props["numHosts"] = s.NumHosts
		props["numCpuCores"] = s.NumCpuCores
		props["numCpuThreads"] = s.NumCpuThreads
		props["effectiveCpu"] = s.EffectiveCpu
		props["effectiveMemory"] = s.EffectiveMemory
	}

	c.GraphBuilder.EnsureNode([]string{"Cluster"}, clusterID, props)
	c.GraphBuilder.AddEdge("CONTAINS", parentID, clusterID, nil)

	for _, hostRef := range cluster.Host {
		c.processHostFromCache(hostRef, clusterID, false, cache)
	}
	for _, dsRef := range cluster.Datastore {
		c.processDatastoreFromCache(dsRef, "", cache)
	}
	if cluster.ResourcePool != nil {
		c.processResourcePoolFromCache(*cluster.ResourcePool, clusterID, cache)
	}
}

func (c *Collector) processComputeResourceFromCache(ref types.ManagedObjectReference, parentID string, cache *infraCache) {
	cr, ok := cache.Computes[ref.Value]
	if !ok {
		return
	}
	for _, hostRef := range cr.Host {
		c.processHostFromCache(hostRef, parentID, true, cache)
	}
}

func (c *Collector) processHostFromCache(ref types.ManagedObjectReference, parentID string, isStandalone bool, cache *infraCache) {
	host, ok := cache.Hosts[ref.Value]
	if !ok {
		return
	}

	hostID := fmt.Sprintf("esxi_host:%s:%s", c.Config.Host, ref.Value)
	props := map[string]interface{}{
		"name": host.Name,
		"moid": ref.Value,
	}
	if isStandalone {
		props["isStandalone"] = true
	}

	summary := host.Summary
	if summary.Hardware != nil {
		hw := summary.Hardware
		props["vendor"] = hw.Vendor
		props["model"] = hw.Model
		props["cpuModel"] = hw.CpuModel
		props["numCpuCores"] = fmt.Sprintf("%d", hw.NumCpuCores)
		props["numCpuThreads"] = fmt.Sprintf("%d", hw.NumCpuThreads)
		props["cpuMhz"] = hw.CpuMhz
		props["memorySize"] = fmt.Sprintf("%d", hw.MemorySize)
	}
	if summary.Config.Product != nil {
		props["version"] = summary.Config.Product.Version
		props["build"] = summary.Config.Product.Build
	}
	if summary.Runtime != nil {
		props["connectionState"] = string(summary.Runtime.ConnectionState)
		props["powerState"] = string(summary.Runtime.PowerState)
		props["inMaintenanceMode"] = summary.Runtime.InMaintenanceMode
	}

	c.GraphBuilder.EnsureNode([]string{"ESXiHost"}, hostID, props)
	c.GraphBuilder.AddEdge("CONTAINS", parentID, hostID, nil)

	for _, vmRef := range host.Vm {
		c.processVMFromCache(vmRef, hostID, cache)
	}
	for _, dsRef := range host.Datastore {
		c.processDatastoreFromCache(dsRef, "", cache)
	}
	for _, netRef := range host.Network {
		c.processNetworkFromCache(netRef, "", cache)
	}
}

func (c *Collector) processVMFolderFromCache(ref types.ManagedObjectReference, parentID string, cache *infraCache) {
	folder, ok := cache.Folders[ref.Value]
	if !ok {
		return
	}

	folderID := fmt.Sprintf("folder:%s:%s", c.Config.Host, ref.Value)
	c.GraphBuilder.EnsureNode([]string{"Folder"}, folderID, map[string]interface{}{
		"name": folder.Name,
		"moid": ref.Value,
	})
	c.GraphBuilder.AddEdge("CONTAINS", parentID, folderID, nil)

	for _, child := range folder.ChildEntity {
		switch child.Type {
		case "Folder":
			c.processVMFolderFromCache(child, folderID, cache)
		case "VirtualMachine":
			c.processVMFromCache(child, folderID, cache)
		}
	}
}

func (c *Collector) processVMFromCache(ref types.ManagedObjectReference, parentID string, cache *infraCache) {
	vm, ok := cache.VMs[ref.Value]
	if !ok {
		return
	}

	vmID := fmt.Sprintf("vm:%s:%s", c.Config.Host, ref.Value)
	props := map[string]interface{}{
		"name": vm.Name,
		"moid": ref.Value,
	}

	if vm.Config != nil {
		props["guestFullName"] = vm.Config.GuestFullName
		props["guestId"] = vm.Config.GuestId
		props["version"] = vm.Config.Version
		props["uuid"] = vm.Config.Uuid
		props["isTemplate"] = vm.Config.Template
		if vm.Config.Hardware.NumCPU > 0 {
			props["numCPU"] = vm.Config.Hardware.NumCPU
			props["numCoresPerSocket"] = vm.Config.Hardware.NumCoresPerSocket
			props["memoryMB"] = vm.Config.Hardware.MemoryMB
		}
	}

	if vm.Runtime.PowerState != "" {
		props["powerState"] = string(vm.Runtime.PowerState)
		props["connectionState"] = string(vm.Runtime.ConnectionState)
		if vm.Runtime.BootTime != nil {
			props["bootTime"] = vm.Runtime.BootTime.String()
		} else {
			props["bootTime"] = "None"
		}
	}

	if vm.Guest != nil {
		props["toolsStatus"] = string(vm.Guest.ToolsStatus)
		props["toolsVersion"] = vm.Guest.ToolsVersion
		props["hostName"] = vm.Guest.HostName

		ipSet := make(map[string]bool)
		if vm.Guest.IpAddress != "" {
			ipSet[vm.Guest.IpAddress] = true
		}
		for _, net := range vm.Guest.Net {
			for _, ip := range net.IpAddress {
				ipSet[ip] = true
			}
		}
		if len(ipSet) > 0 {
			var ips []string
			for ip := range ipSet {
				ips = append(ips, ip)
			}
			sort.Strings(ips)
			props["ipAddresses"] = ips
		}
	}

	if vm.Summary.Storage != nil {
		committed := vm.Summary.Storage.Committed
		uncommitted := vm.Summary.Storage.Uncommitted
		props["storageCommitted"] = bytesToHuman(float64(committed))
		props["storageUncommitted"] = bytesToHuman(float64(uncommitted))
		props["storageTotalUsed"] = bytesToHuman(float64(committed + uncommitted))
	}

	c.GraphBuilder.EnsureNode([]string{"VM"}, vmID, props)
	c.GraphBuilder.AddEdge("HOSTS", parentID, vmID, nil)

	for _, dsRef := range vm.Datastore {
		dsID := fmt.Sprintf("datastore:%s:%s", c.Config.Host, dsRef.Value)
		c.processDatastoreFromCache(dsRef, "", cache)
		c.GraphBuilder.AddEdge("USES_DATASTORE", vmID, dsID, nil)
	}
	for _, netRef := range vm.Network {
		netID := fmt.Sprintf("network:%s:%s", c.Config.Host, netRef.Value)
		c.processNetworkFromCache(netRef, "", cache)
		c.GraphBuilder.AddEdge("USES_NETWORK", vmID, netID, nil)
	}
}

func (c *Collector) processDatastoreFromCache(ref types.ManagedObjectReference, parentID string, cache *infraCache) {
	ds, ok := cache.Datastores[ref.Value]
	if !ok {
		return
	}

	dsID := fmt.Sprintf("datastore:%s:%s", c.Config.Host, ref.Value)
	props := map[string]interface{}{
		"name":       ds.Name,
		"moid":       ref.Value,
		"type":       ds.Summary.Type,
		"capacity":   fmt.Sprintf("%d", ds.Summary.Capacity),
		"freeSpace":  fmt.Sprintf("%d", ds.Summary.FreeSpace),
		"accessible": ds.Summary.Accessible,
		"url":        ds.Summary.Url,
	}
	c.GraphBuilder.EnsureNode([]string{"Datastore"}, dsID, props)
}

func (c *Collector) processNetworkFromCache(ref types.ManagedObjectReference, parentID string, cache *infraCache) {
	netID := fmt.Sprintf("network:%s:%s", c.Config.Host, ref.Value)

	kind := "Network"
	if ref.Type == "DistributedVirtualPortgroup" {
		kind = "DVPortgroup"
	}

	name := ref.Value // fallback
	if net, ok := cache.Networks[ref.Value]; ok {
		name = net.Name
	}

	c.GraphBuilder.EnsureNode([]string{kind}, netID, map[string]interface{}{
		"name": name,
		"moid": ref.Value,
		"type": ref.Type,
		"kind": kind,
	})
}

func (c *Collector) processResourcePoolFromCache(ref types.ManagedObjectReference, parentID string, cache *infraCache) {
	rp, ok := cache.ResPools[ref.Value]
	if !ok {
		return
	}

	rpID := fmt.Sprintf("resource_pool:%s:%s", c.Config.Host, ref.Value)
	c.GraphBuilder.EnsureNode([]string{"ResourcePool"}, rpID, map[string]interface{}{
		"name": rp.Name,
		"moid": ref.Value,
	})
	c.GraphBuilder.AddEdge("CONTAINS", parentID, rpID, nil)

	for _, child := range rp.ResourcePool {
		c.processResourcePoolFromCache(child, rpID, cache)
	}
}

// bytesToHuman converts bytes to human readable string.
func bytesToHuman(bytesVal float64) string {
	if bytesVal == 0 {
		return "0 B"
	}
	units := []string{"B", "KB", "MB", "GB", "TB"}
	for _, unit := range units {
		if bytesVal < 1024.0 {
			return fmt.Sprintf("%.1f %s", bytesVal, unit)
		}
		bytesVal /= 1024.0
	}
	return fmt.Sprintf("%.1f PB", bytesVal)
}
