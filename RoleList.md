# vCenter Role Permissions

| Role Name | Description | Permissions |
| :--- | :--- | :--- |
| **vCenter_ReadOnly** | Standard vCenter Read-Only role. | System.Anonymous, System.View, System.Read. Users can view objects but cannot make changes. |
| **vCenter_WorkloadStorageManagement** | Workload Storage Management role (vSphere with Tanzu). | Resource.AssignVMToPool, Resource.ColdMigrate, VirtualMachine.Config.*, Datastore.AllocateSpace, etc. Manages storage for workloads. |
| **vCenter_NsxAuditor** | NSX Auditor role. | Read-only access to NSX system settings, auditing, events, and reporting. No configuration changes allowed. |
| **vCenter_TrustedAdmin** | Trusted Infrastructure Administrator (vSphere Trust Authority). | VTA APIs (.TrustedAdmin.*). associated with TrustedAdmins group to manage Trust Authority. |
| **vCenter_com_vmware_Content_Registry_Admin** | Content Library Administrator. | Create, edit, delete, publish, and subscribe to content libraries. Manage global library settings. |
| **vCenter_vCLSAdmin** | vSphere Cluster Services (vCLS) Administrator (or related). | vCLS VMs are system-managed. This role (or Administrator) allows viewing/managing vCLS VMs if necessary (though usually discouraged). |
| **vCenter_Admin** | Administrator role. | Full privileges on all objects in the vCenter Server. |
| **vCenter_vStatsAdmin** | vStats Service Administrator (Internal/Custom). | Likely related to vCenter Statistics/vStats service integration. Not a standard user-facing role. |
| **vCenter_AutoUpdateUser** | Auto Update Service User/Role. | Permissions related to vSphere Lifecycle Manager (vLCM) or Update Manager (VUM) to perform automatic updates. |
| **vCenter_SyncUsers** | vCenter Cloud Gateway Internal Role (RoleID 1002). | Internal role for Hybrid vCenter Service (HVC). Required for service operations and upgrades. |
| **vCenter_applmgmtSvcRole** | Appliance Management Service Role. | Internal system role for vCenter Server Appliance (VCSA) management operations. |
| **vCenter_NsxViAdministrator** | NSX VI Administrator (Custom/NSX). | Likely "NSX Administrator" privileges for VI path, allowing deployment and administration of NSX components. |
| **vCenter_NsxAdministrator** | NSX Administrator. | Full administration of NSX Manager, including appliance installation and port group configuration. |
| **vCenter_vSphere_Client_Solution_User** | vSphere Client Solution User. | Custom role or user for a specific solution integration with vSphere Client. Privileges depend on the specific solution. |
| **vCenter_VsmSvcRole** | VMware Service Manager / Service Role. | Internal service role for specific VMware services (e.g., related to visual/storage management services). |
| **vCenter_ObservabilityVapiSvcRole** | Observability VAPI Service Role. | Access to vCenter Observability APIs. Used by agents or services to monitor vCenter health/stats. |
| **vCenter_SSO_ReadOnly** | Single Sign-On Read Only. | Read-only access to SSO configuration (Users, Groups, Identity Sources). |
| **vCenter_TopologysvcUser** | Topology Service User. | Internal service account/role for vCenter Inventory Topology mapping services. |
| **vCenter_PerfchartsSvcRole** | Performance Charts Service Role. | Internal role for the Performance Charts service to access and render performance data. |
| **vCenter_VirtualMachinePowerUser** | Virtual Machine Power User. | Interact with VMs (Power On/Off, Suspend, Reset), Snapshot management, and Guest OS interaction. Cannot add/remove hardware. |