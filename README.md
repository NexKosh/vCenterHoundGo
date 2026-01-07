# vCenter to BloodHound SOP

This document outlines the standard operating procedure for integrating vCenter data into BloodHound.

## Prerequisites

Ensure you have the following executables compiled and ready:
1.  **vCenterSchemaUploader**: For uploading the `model.json` schema.
2.  **vCenterHoundGo**: For collecting vCenter data.
3.  **vCenterNeo4j**: For syncing AD relationships.

## Step 1: Upload Model Schema

Before ingesting any data, you must upload the custom node definitions (`model.json`) to BloodHound.

**Command:**
```powershell
cd .\vCenterSchemaUploader
.\vCenterSchemaUploader.exe -s <BH_URL> -u <BH_USER> -p <BH_PASS>
```
*   `-s`: BloodHound URL (e.g., `http://localhost:8080`).
*   `-u`: BloodHound Username (e.g., `admin`).
*   `-p`: BloodHound Password.
*   **Note**: Ensure `model.json` is in the same directory or specify `-model <path>`.

## Step 2: vCenter Data Collection

Collect the infrastructure and permission data from vCenter.

**Command:**
```powershell
cd ..\vCenterHoundGo
.\vCenterHoundGo.exe -s <VCENTER_IP> -u <VCENTER_USER> -p <VCENTER_PASS> -o vcenter_raw.json
```
*   `-s`: vCenter IP/Hostname.
*   `-u`: vCenter Username.
*   `-p`: vCenter Password.
*   `-o`: Output file (recommended: `vcenter_raw.json`).

## Step 3: Data Ingestion

Upload the collected data file to BloodHound.

1.  Open the **BloodHound GUI** in your browser.
2.  Navigate to the **File Upload** section (or simply drag and drop).
3.  Upload the `vcenter_raw.json` generated in Step 2.
4.  Wait for the ingestion to finish.

## Step 4: AD Relationship Sync

Link the ingested vCenter nodes to existing Active Directory nodes (Users/Groups) in the Neo4j database.

**Command:**
```powershell
cd ..\vCenterNeo4j
.\vCenterNeo4j.exe -s <NEO4J_IP> -u <NEO4J_USER> -p <NEO4J_PASS> -sync
```
*   `-s`: Neo4j Host/IP (e.g., `192.168.3.20`).
*   `-u`: Neo4j Username (default: `neo4j`).
*   `-p`: Neo4j Password.
*   `-sync`: **Required** to perform the linking operation.

## Verification
You can verify the integration in BloodHound by running this Cypher query:
```cypher
MATCH p=()-[r:SyncsTovCenterUser]->() RETURN p LIMIT 25
```
This should show AD users connected to vCenter users.
