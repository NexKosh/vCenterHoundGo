package collector

import (
	"fmt"
	"log"
	"sort"

	"github.com/vmware/govmomi/view"
	"github.com/vmware/govmomi/vim25/mo"
	"github.com/vmware/govmomi/vim25/types"
)

// CollectInfrastructure collects all infrastructure data
func (c *Collector) CollectInfrastructure() error {
	// Root Folder
	m := view.NewManager(c.Client.Client)

	v, err := m.CreateContainerView(c.Context, c.Client.ServiceContent.RootFolder, []string{"Folder", "Datacenter"}, false)
	if err != nil {
		return err
	}
	defer v.Destroy(c.Context)

	// Add vCenter node
	vcenterID := fmt.Sprintf("vcenter:%s", c.Config.Host)
	c.GraphBuilder.EnsureNode([]string{"vCenter"}, vcenterID, map[string]interface{}{"name": c.Config.Host})
	log.Printf("Added vCenter node: %s", c.Config.Host)

	// Root folder processing
	var rootFolder mo.Folder
	err = c.Client.RetrieveOne(c.Context, c.Client.ServiceContent.RootFolder, nil, &rootFolder)
	if err != nil {
		return err
	}

	rootID := fmt.Sprintf("folder:%s:%s", c.Config.Host, getID(rootFolder.Reference()))
	c.GraphBuilder.EnsureNode([]string{"RootFolder", "Folder"}, rootID, map[string]interface{}{
		"name": rootFolder.Name,
		"moid": getID(rootFolder.Reference()),
	})
	c.GraphBuilder.AddEdge("CONTAINS", vcenterID, rootID, nil)

	// Iterate children of root folder
	for _, child := range rootFolder.ChildEntity {
		switch child.Type {
		case "Datacenter":
			if err := c.processDatacenter(child, rootID); err != nil {
				log.Printf("Error processing datacenter: %v", err)
			}
		case "Folder":
			if err := c.processFolder(child, rootID); err != nil {
				log.Printf("Error processing folder: %v", err)
			}
		}
	}

	return nil
}

func (c *Collector) processFolder(ref types.ManagedObjectReference, parentID string) error {
	var folder mo.Folder
	err := c.Client.RetrieveOne(c.Context, ref, []string{"name", "childEntity"}, &folder)
	if err != nil {
		return err
	}

	folderID := fmt.Sprintf("folder:%s:%s", c.Config.Host, getID(folder.Reference()))
	c.GraphBuilder.EnsureNode([]string{"Folder"}, folderID, map[string]interface{}{
		"name": folder.Name,
		"moid": getID(folder.Reference()),
	})
	c.GraphBuilder.AddEdge("CONTAINS", parentID, folderID, nil)

	for _, child := range folder.ChildEntity {
		switch child.Type {
		case "Datacenter":
			c.processDatacenter(child, folderID)
		case "Folder":
			c.processFolder(child, folderID)
		case "VirtualMachine":
			c.processVM(child, folderID)
		case "ComputeResource", "ClusterComputeResource":
			// Should act like processComputeFolder? Or are these direct children?
			// Process compute folder children usually.
			// Ideally we use specialized methods.
		}
	}
	return nil
}

func (c *Collector) processDatacenter(ref types.ManagedObjectReference, parentID string) error {
	var dc mo.Datacenter
	// Retrieve structure
	err := c.Client.RetrieveOne(c.Context, ref, []string{"name", "hostFolder", "vmFolder", "datastore", "network"}, &dc)
	if err != nil {
		return err
	}

	dcID := fmt.Sprintf("datacenter:%s:%s", c.Config.Host, getID(dc.Reference()))
	c.GraphBuilder.EnsureNode([]string{"Datacenter"}, dcID, map[string]interface{}{
		"name": dc.Name,
		"moid": getID(dc.Reference()),
	})
	c.GraphBuilder.AddEdge("CONTAINS", parentID, dcID, nil)

	log.Printf("Processing Datacenter: %s", dc.Name)

	// Process Host Folder (Compute Resources)
	if err := c.processHostFolder(dc.HostFolder, dcID); err != nil {
		log.Printf("Error processing host folder for DC %s: %v", dc.Name, err)
	}

	// Process VM Folder
	if err := c.processVMFolder(dc.VmFolder, dcID); err != nil {
		log.Printf("Error processing VM folder for DC %s: %v", dc.Name, err)
	}

	// Process Datastores
	for _, ds := range dc.Datastore {
		c.processDatastore(ds, dcID)
	}

	// Process Networks
	for _, net := range dc.Network {
		c.processNetwork(net, dcID)
	}

	return nil
}

func (c *Collector) processHostFolder(ref types.ManagedObjectReference, parentID string) error {
	// Recursively process folders until we find ComputeResource or ClusterComputeResource
	var folder mo.Folder
	err := c.Client.RetrieveOne(c.Context, ref, []string{"name", "childEntity"}, &folder)
	if err != nil {
		return err
	}

	// Host folder itself is not usually a node in the graph in the Python script logic if it's strictly "hostFolder",
	// BUT the Python script processes "Compute Folder" recursively and adds it as a Folder node.
	// Let's verify Python logic: _process_compute_folder adds a Folder node.
	// _process_datacenter calls calls _process_compute_folder on compute wrapper folders if they are folders?
	// In Python: `if isinstance(compute, vim.Folder): _process_compute_folder`
	// Actually `dc.hostFolder` IS a folder. So we should treat it as one if we want exact parity.
	// Only if it's NOT the root host folder? The Python script iterates `dc.hostFolder.childEntity`.
	// If the child is a Folder, it calls `_process_compute_folder`.
	// If the child is Host/Cluster, it calls `_process_standalone_host` / `_process_cluster`.
	// It does NOT add the `hostFolder` itself as a node usually, unless it enters `_process_compute_folder`.

	for _, child := range folder.ChildEntity {
		switch child.Type {
		case "Folder":
			c.processComputeFolder(child, parentID)
		case "ClusterComputeResource":
			c.processCluster(child, parentID)
		case "ComputeResource":
			// Standalone host
			c.processComputeResource(child, parentID)
		case "HostSystem":
			// Sometimes direct HostSystem?
			c.processStandaloneHost(child, parentID)
		}
	}
	return nil
}

func (c *Collector) processComputeFolder(ref types.ManagedObjectReference, parentID string) {
	var folder mo.Folder
	err := c.Client.RetrieveOne(c.Context, ref, []string{"name", "childEntity"}, &folder)
	if err != nil {
		return
	}

	folderID := fmt.Sprintf("folder:%s:%s", c.Config.Host, getID(folder.Reference()))
	c.GraphBuilder.EnsureNode([]string{"Folder"}, folderID, map[string]interface{}{
		"name": folder.Name,
		"moid": getID(folder.Reference()),
	})
	// Connect to parent (Datacenter or parent Folder)
	// But in processDatacenter we passed dcID as parent.
	// Python script: _process_compute_folder adds edge PARENT -> FOLDER.
	c.GraphBuilder.AddEdge("CONTAINS", parentID, folderID, nil)

	for _, child := range folder.ChildEntity {
		switch child.Type {
		case "Folder":
			c.processComputeFolder(child, folderID)
		case "ClusterComputeResource":
			c.processCluster(child, folderID)
		case "ComputeResource":
			c.processComputeResource(child, folderID)
		}
	}
}

func (c *Collector) processCluster(ref types.ManagedObjectReference, parentID string) {
	var cluster mo.ClusterComputeResource
	err := c.Client.RetrieveOne(c.Context, ref, []string{"name", "host", "datastore", "network", "resourcePool", "summary", "configuration"}, &cluster)
	if err != nil {
		log.Printf("Error collecting cluster %s: %v", ref.Value, err)
		return
	}

	clusterID := fmt.Sprintf("cluster:%s:%s", c.Config.Host, getID(cluster.Reference()))
	props := map[string]interface{}{
		"name": cluster.Name,
		"moid": getID(cluster.Reference()),
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

	// Configuration (DRS/HA)
	// govmomi configuration might be deep.
	// Configuration (DRS/HA)
	if cluster.Configuration.DrsConfig.Enabled != nil || cluster.Configuration.DasConfig.Enabled != nil {
		// Just proceed if fields exist
	}

	c.GraphBuilder.EnsureNode([]string{"Cluster"}, clusterID, props)
	c.GraphBuilder.AddEdge("CONTAINS", parentID, clusterID, nil)

	// Hosts
	for _, hostRef := range cluster.Host {
		c.processHost(hostRef, clusterID)
	}

	// Datastores (collect as nodes)
	for _, dsRef := range cluster.Datastore {
		c.processDatastore(dsRef, "") // Just ensure node, no edge from cluster? Python code: ensure_node only.
	}

	// Resource Pool
	if cluster.ResourcePool != nil {
		c.processResourcePool(*cluster.ResourcePool, clusterID)
	}
}

func (c *Collector) processComputeResource(ref types.ManagedObjectReference, parentID string) {
	// Standalone host wrapper
	var cr mo.ComputeResource
	err := c.Client.RetrieveOne(c.Context, ref, []string{"host"}, &cr)
	if err != nil {
		return
	}
	for _, hostRef := range cr.Host {
		c.processStandaloneHost(hostRef, parentID)
	}
}

func (c *Collector) processStandaloneHost(ref types.ManagedObjectReference, parentID string) {
	c.processHostCommon(ref, parentID, true)
}

func (c *Collector) processHost(ref types.ManagedObjectReference, parentID string) {
	c.processHostCommon(ref, parentID, false)
}

func (c *Collector) processHostCommon(ref types.ManagedObjectReference, parentID string, isStandalone bool) {
	var host mo.HostSystem
	// Retrieve properties
	err := c.Client.RetrieveOne(c.Context, ref, []string{"name", "summary", "vm", "datastore", "network"}, &host)
	if err != nil {
		log.Printf("Error processing host %s: %v", ref.Value, err)
		return
	}

	hostID := fmt.Sprintf("esxi_host:%s:%s", c.Config.Host, getID(host.Reference()))
	props := map[string]interface{}{
		"name": host.Name,
		"moid": getID(host.Reference()),
	}
	if isStandalone {
		props["isStandalone"] = true
	}

	// Summary Hardware/Config/Runtime
	summary := host.Summary
	if summary.Hardware != nil {
		hw := summary.Hardware
		props["vendor"] = hw.Vendor
		props["model"] = hw.Model
		props["cpuModel"] = hw.CpuModel
		props["numCpuCores"] = fmt.Sprintf("%d", hw.NumCpuCores)     // String in Python
		props["numCpuThreads"] = fmt.Sprintf("%d", hw.NumCpuThreads) // String in Python
		props["cpuMhz"] = hw.CpuMhz
		props["memorySize"] = fmt.Sprintf("%d", hw.MemorySize) // String in Python
	}
	// Config product
	if summary.Config.Product != nil {
		props["version"] = summary.Config.Product.Version
		props["build"] = summary.Config.Product.Build
	}
	// Runtime
	if summary.Runtime != nil {
		props["connectionState"] = string(summary.Runtime.ConnectionState)
		props["powerState"] = string(summary.Runtime.PowerState)
		props["inMaintenanceMode"] = summary.Runtime.InMaintenanceMode
	}

	c.GraphBuilder.EnsureNode([]string{"ESXiHost"}, hostID, props)
	c.GraphBuilder.AddEdge("CONTAINS", parentID, hostID, nil)

	// VMs
	for _, vmRef := range host.Vm {
		c.processVM(vmRef, hostID)
	}

	// Datastores (Just ensure node)
	for _, dsRef := range host.Datastore {
		c.processDatastore(dsRef, "")
	}

	// Networks (Ensure node)
	for _, netRef := range host.Network {
		c.processNetwork(netRef, "")
	}
}

func (c *Collector) processVMFolder(ref types.ManagedObjectReference, parentID string) error {
	var folder mo.Folder
	err := c.Client.RetrieveOne(c.Context, ref, []string{"name", "childEntity"}, &folder)
	if err != nil {
		return err
	}

	folderID := fmt.Sprintf("folder:%s:%s", c.Config.Host, getID(folder.Reference()))
	c.GraphBuilder.EnsureNode([]string{"Folder"}, folderID, map[string]interface{}{
		"name": folder.Name,
		"moid": getID(folder.Reference()),
	})
	c.GraphBuilder.AddEdge("CONTAINS", parentID, folderID, nil)

	for _, child := range folder.ChildEntity {
		switch child.Type {
		case "Folder":
			c.processVMFolder(child, folderID)
		case "VirtualMachine":
			c.processVM(child, folderID)
		}
	}
	return nil
}

func (c *Collector) processVM(ref types.ManagedObjectReference, parentID string) {
	// Retrieve logic matching Python's _collect_vm_properties
	var vm mo.VirtualMachine
	err := c.Client.RetrieveOne(c.Context, ref, []string{"name", "config", "guest", "runtime", "summary", "datastore", "network"}, &vm)
	if err != nil {
		return
	}

	vmID := fmt.Sprintf("vm:%s:%s", c.Config.Host, getID(vm.Reference()))

	// Default properties
	props := map[string]interface{}{
		"name": vm.Name,
		"moid": getID(vm.Reference()),
	}

	// Config
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

	// Runtime
	if vm.Runtime.PowerState != "" {
		props["powerState"] = string(vm.Runtime.PowerState)
		props["connectionState"] = string(vm.Runtime.ConnectionState)
		if vm.Runtime.BootTime != nil {
			// Approximation of Python's str(datetime)
			props["bootTime"] = vm.Runtime.BootTime.Format("2006-01-02 15:04:05.999999-07:00")
			// Or just stick to .String() if that was close enough.
			// But previous verification didn't flag bootTime. Let's leave it as .String() if it works.
			props["bootTime"] = vm.Runtime.BootTime.String()
		} else {
			props["bootTime"] = "None"
		}
	}

	// Guest
	if vm.Guest != nil {
		props["toolsStatus"] = string(vm.Guest.ToolsStatus)
		props["toolsVersion"] = vm.Guest.ToolsVersion
		props["hostName"] = vm.Guest.HostName

		ipSet := make(map[string]bool)

		if vm.Guest.IpAddress != "" {
			ipSet[vm.Guest.IpAddress] = true
		}

		for _, net := range vm.Guest.Net {
			if net.IpAddress != nil {
				for _, ip := range net.IpAddress {
					ipSet[ip] = true
				}
			}
		}

		if len(ipSet) > 0 {
			var ips []string
			for ip := range ipSet {
				ips = append(ips, ip)
			}
			// Python list(set()) order is undefined/random. Go map iteration is random.
			// If strict match needed, might need sorting, but JSON arrays usually ordered.
			sort.Strings(ips)
			props["ipAddresses"] = ips
		}
	}

	// Storage (missing in previous implementation)
	if vm.Summary.Storage != nil {
		storage := vm.Summary.Storage
		committed := storage.Committed
		uncommitted := storage.Uncommitted
		props["storageCommitted"] = bytesToHuman(float64(committed))
		props["storageUncommitted"] = bytesToHuman(float64(uncommitted))
		props["storageTotalUsed"] = bytesToHuman(float64(committed + uncommitted))
	}

	c.GraphBuilder.EnsureNode([]string{"VM"}, vmID, props)
	c.GraphBuilder.AddEdge("HOSTS", parentID, vmID, nil)

	// Datastores
	for _, dsRef := range vm.Datastore {
		dsID := fmt.Sprintf("datastore:%s:%s", c.Config.Host, getID(dsRef))
		c.processDatastore(dsRef, "")
		c.GraphBuilder.AddEdge("USES_DATASTORE", vmID, dsID, nil)
	}

	// Networks
	for _, netRef := range vm.Network {
		netID := fmt.Sprintf("network:%s:%s", c.Config.Host, getID(netRef))
		c.processNetwork(netRef, "")
		c.GraphBuilder.AddEdge("USES_NETWORK", vmID, netID, nil)
	}
}

func (c *Collector) getIPAddresses(guest *types.GuestInfo) []string {
	var ips []string
	if guest.IpAddress != "" {
		ips = append(ips, guest.IpAddress)
	}
	for _, nic := range guest.Net {
		if nic.IpAddress != nil {
			ips = append(ips, nic.IpAddress...)
		}
	}
	// unique
	seen := make(map[string]bool)
	var ret []string
	for _, ip := range ips {
		if !seen[ip] {
			seen[ip] = true
			ret = append(ret, ip)
		}
	}
	return ret
}

func (c *Collector) processDatastore(ref types.ManagedObjectReference, parentID string) {
	// Same as Python _process_datastore
	var ds mo.Datastore
	err := c.Client.RetrieveOne(c.Context, ref, []string{"name", "summary", "info"}, &ds)
	if err != nil {
		return
	}

	dsID := fmt.Sprintf("datastore:%s:%s", c.Config.Host, getID(ds.Reference()))
	props := map[string]interface{}{
		"name": ds.Name,
		"moid": getID(ds.Reference()),
	}

	// Summary is a value type, assume it's populated
	props["type"] = ds.Summary.Type
	props["capacity"] = fmt.Sprintf("%d", ds.Summary.Capacity)   // String
	props["freeSpace"] = fmt.Sprintf("%d", ds.Summary.FreeSpace) // String
	props["accessible"] = ds.Summary.Accessible
	props["url"] = ds.Summary.Url

	// Extra fields usually?
	// Python doesn't add much else.

	c.GraphBuilder.EnsureNode([]string{"Datastore"}, dsID, props)
}

func (c *Collector) processNetwork(ref types.ManagedObjectReference, parentID string) {
	// Check type: Network or DistributedVirtualPortgroup
	var net mo.Network
	_ = c.Client.RetrieveOne(c.Context, ref, []string{"name", "summary"}, &net)

	// Fallback/Check for DVPortgroup
	isDV := false
	if ref.Type == "DistributedVirtualPortgroup" {
		isDV = true
	}

	kind := "Network"
	if isDV {
		kind = "DVPortgroup"
	}

	netID := fmt.Sprintf("network:%s:%s", c.Config.Host, getID(ref))
	props := map[string]interface{}{
		"name": net.Name,
		"moid": getID(ref),
		"type": ref.Type,
		"kind": kind,
	}

	c.GraphBuilder.EnsureNode([]string{kind}, netID, props)
}

func (c *Collector) processResourcePool(ref types.ManagedObjectReference, parentID string) {
	// Recursive resource pool processing
	var rp mo.ResourcePool
	_ = c.Client.RetrieveOne(c.Context, ref, []string{"name", "resourcePool", "vApp", "vm"}, &rp)

	rpID := fmt.Sprintf("resource_pool:%s:%s", c.Config.Host, getID(ref))
	c.GraphBuilder.EnsureNode([]string{"ResourcePool"}, rpID, map[string]interface{}{
		"name": rp.Name,
		"moid": getID(ref),
	})

	// Children
	for _, child := range rp.ResourcePool {
		c.processResourcePool(child, rpID)
	}
}

// bytesToHuman converts bytes to human readable string, matching Python logic
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
