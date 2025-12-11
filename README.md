# vCenterHound

Export vCenter data (hosts, VMs, permissions, users, groups, tags) into a BloodHound-compatible JSON file for security analysis and attack path visualization.

🚀 **Now in Go!** This version replaces the original Python script, offering significant performance improvements and new features like REST API tag collection and BloodHound Enterprise integration.

## Features

*   **High Performance**: Go implementation with concurrent processing for large environments.
*   **Comprehensive Collection**:
    *   **Infrastructure**: Datacenters, Clusters, ESXi Hosts, Resource Pools, VMs, Datastores, Networks.
    *   **Permissions**: Roles, Privileges, Users, Groups, and complex permission assignments.
    *   **Tags**: vCenter Tags collected via REST API (associated with VMs/Hosts).
*   **Active Directory Sync**: Automatically links vCenter users/groups to Active Directory nodes in BloodHound by resolving NetBIOS domains to FQDNs via BloodHound Enterprise API.
*   **Group Memberships**: Resolves nested group memberships including SSO and local groups.
*   **BloodHound Compatible**: Generates a standard graph JSON file with custom nodes/edges defined in `model.json`.

## Installation

### Requirements
*   Go 1.21 or later

### Build from Source

```bash
git clone https://github.com/jazofra/vCenterHoundGo
cd vCenterHoundGo
go build -o vCenterHound.exe cmd/vcenterhoundgo/main.go
```

## Usage

### 1. Upload Model to BloodHound
Before importing data, you must register the custom node/edge types in BloodHound. Use the provided `model.json`.

(Use `update_custom_nodes_to_bloodhound.py` if available, or upload via BloodHound API).

### 2. Run Collector

**Basic Run:**
```bash
./vCenterHound -s vc.example.com -u administrator@vsphere.local -p "Password!"
```

**With Active Directory Sync (BloodHound Enterprise):**
This mode fetches available domains from BloodHound to map vCenter NetBIOS names (e.g., `CORP`) to FQDNs (e.g., `CORP.LOCAL`), creating `SyncsTovCenterUser` edges.

```bash
./vCenterHound \
  -s vc.example.com \
  -u administrator@vsphere.local \
  -p "Password!" \
  --bh-url https://bloodhound.example.com \
  --bh-key-id "YOUR_KEY_ID" \
  --bh-key-secret "YOUR_KEY_SECRET"
```

**Debug Mode:**
Enable detailed logging and stats.
```bash
./vCenterHound -s vc.example.com ... --debug
```

### Command-Line Arguments

*   `-s`: vCenter server(s) (comma-separated).
*   `-u`: vCenter username.
*   `-p`: vCenter password.
*   `-P`: vCenter port (default 443).
*   `-o`: Output file path (default `vcenter_graph.json`).
*   `--debug`: Enable debug logging.
*   `--bh-url`: BloodHound Enterprise URL (for AD sync).
*   `--bh-key-id`: BloodHound API Key ID.
*   `--bh-key-secret`: BloodHound API Key Secret.

## Edge Types

| Edge Type | Source | Target | Description |
|-----------|--------|--------|-------------|
| `vCenter_Contains` | Folder/DC | Entity | Hierarchical containment |
| `vCenter_Hosts` | ESXiHost | VM | VM execution location |
| `vCenter_HasPermission` | User/Group | Entity | Direct permission assignment |
| `vCenter_MemberOf` | User/Group | Group | Group membership |
| `vCenter_UsesDatastore` | VM | Datastore | Storage dependency |
| `vCenter_UsesNetwork` | VM | Network | Network connection |
| `SyncsTovCenterUser` | User (AD) | vCenter_User | Sync relationship (AD -> vCenter) |
| `SyncsTovCenterGroup` | Group (AD)| vCenter_Group| Sync relationship (AD -> vCenter) |

## Acknowledgments

This tool is a Go port and enhancement of the original [vCenterHound](https://github.com/MorDavid/vCenterHound) by **Mor David**.

Original Author: Mor David (https://github.com/MorDavid)
Go Port & Enhancements: Javier Azofra Ovejero
