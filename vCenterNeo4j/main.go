package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

func main() {
	uriURI := flag.String("uri", "", "Neo4j URI (e.g. bolt://localhost:7687). Overrides -s")
	server := flag.String("s", "localhost", "Neo4j Host address")
	username := flag.String("u", "neo4j", "Username")
	password := flag.String("p", "", "Password")
	sync := flag.Bool("sync", false, "Sync relationships to AD")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage of %s:\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "\nDescription:\n")
		fmt.Fprintf(os.Stderr, "  Connects to Neo4j to query vCenter nodes or sync relationships with Active Directory.\n")
		fmt.Fprintf(os.Stderr, "\nOptions:\n")
		flag.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\nExamples:\n")
		fmt.Fprintf(os.Stderr, "  # Query Only (Default):\n")
		fmt.Fprintf(os.Stderr, "  %s -s 192.168.3.20 -u neo4j -p password\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "\n  # Sync Relationships (AD <-> vCenter):\n")
		fmt.Fprintf(os.Stderr, "  %s -s 192.168.3.20 -u neo4j -p password -sync\n", os.Args[0])
	}

	flag.Parse()

	if *password == "" {
		fmt.Println("Please provide a password using -p")
		os.Exit(1)
	}

	target := *uriURI
	if target == "" {
		if strings.Contains(*server, "://") {
			target = *server
		} else {
			target = fmt.Sprintf("bolt://%s:7687", *server)
		}
	}

	fmt.Printf("Connecting to %s...\n", target)

	ctx := context.Background()
	driver, err := neo4j.NewDriverWithContext(target, neo4j.BasicAuth(*username, *password, ""))
	if err != nil {
		log.Fatalf("Failed to create driver: %v", err)
	}
	defer driver.Close(ctx)

	err = driver.VerifyConnectivity(ctx)
	if err != nil {
		log.Fatalf("Failed to verify connectivity: %v", err)
	}
	fmt.Println("Connected to Neo4j")

	if *sync {
		syncRelationships(ctx, driver)
		return
	}

	query := "MATCH (u:vCenter_User) RETURN u"

	// Existing query logic...
	result, err := neo4j.ExecuteQuery(ctx, driver,
		query,
		nil,
		neo4j.EagerResultTransformer,
		neo4j.ExecuteQueryWithDatabase("neo4j"))

	if err != nil {
		log.Fatalf("Failed to execute query: %v", err)
	}

	fmt.Printf("Found %d nodes\n", len(result.Records))

	for _, record := range result.Records {
		val, ok := record.Get("u")
		if !ok {
			log.Printf("Node 'u' not found in record")
			continue
		}

		node, ok := val.(neo4j.Node)
		if !ok {
			log.Printf("Value is not a neo4j.Node")
			continue
		}

		fmt.Printf("ID: %d, ElementID: %s, Labels: %v, Props: %v\n",
			node.Id, node.ElementId, node.Labels, node.Props)
	}
}

func syncRelationships(ctx context.Context, driver neo4j.DriverWithContext) {
	fmt.Println("Starting sync...")

	// 1. Get Domain Map (NetBIOS -> FQDN)
	domainMap := getDomainMap(ctx, driver)
	fmt.Printf("Domain Map: %v\n", domainMap)

	// 2. Sync Users
	syncNodes(ctx, driver, domainMap, "vCenter_User", "SyncsTovCenterUser", "User")

	// 3. Sync Groups
	syncNodes(ctx, driver, domainMap, "vCenter_Group", "SyncsTovCenterGroup", "Group")

	fmt.Println("Sync completed.")
}

func getDomainMap(ctx context.Context, driver neo4j.DriverWithContext) map[string]string {
	query := "MATCH (d:Domain) RETURN d.name"
	result, err := neo4j.ExecuteQuery(ctx, driver, query, nil, neo4j.EagerResultTransformer, neo4j.ExecuteQueryWithDatabase("neo4j"))
	if err != nil {
		log.Printf("Failed to get domains: %v", err)
		return nil
	}

	dMap := make(map[string]string)
	for _, record := range result.Records {
		val, ok := record.Get("d.name")
		if ok {
			fqdn := val.(string)
			// Heuristic: NetBIOS is first part of FQDN
			parts := strings.Split(fqdn, ".")
			if len(parts) > 0 {
				netbios := strings.ToUpper(parts[0])
				dMap[netbios] = strings.ToUpper(fqdn)
			}
		}
	}
	return dMap
}

func syncNodes(ctx context.Context, driver neo4j.DriverWithContext, domainMap map[string]string, vLabel, relType, adLabel string) {
	fmt.Printf("Syncing %s -> %s...\n", vLabel, adLabel)

	// Iterate valid vCenter nodes
	query := fmt.Sprintf("MATCH (v:%s) RETURN v", vLabel)
	result, err := neo4j.ExecuteQuery(ctx, driver, query, nil, neo4j.EagerResultTransformer, neo4j.ExecuteQueryWithDatabase("neo4j"))
	if err != nil {
		log.Printf("Failed to fetch %s: %v", vLabel, err)
		return
	}

	count := 0
	for _, record := range result.Records {
		val, ok := record.Get("v")
		if !ok {
			continue
		}
		node := val.(neo4j.Node)

		props := node.Props
		username, _ := props["username"].(string)
		domain, _ := props["domain"].(string)

		if username == "" || domain == "" {
			continue
		}

		// Resolve FQDN
		fqdn, ok := domainMap[strings.ToUpper(domain)]
		if !ok {
			// Skip if domain not mapped (e.g. local vsphere.local or unknown)
			continue
		}

		targetName := fmt.Sprintf("%s@%s", strings.ToUpper(username), fqdn)

		// Create Relationship
		// MATCH (ad:ADLabel {name: targetName}), (v) WHERE id(v) = $vid
		// MERGE (ad)-[:REL]->(v)

		cypher := fmt.Sprintf(`
			MATCH (ad:%s {name: $adName})
			MATCH (v:%s) WHERE id(v) = $vID
			MERGE (ad)-[:%s]->(v)
			RETURN count(ad)
		`, adLabel, vLabel, relType)

		params := map[string]any{
			"adName": targetName,
			"vID":    node.Id,
		}

		res, err := neo4j.ExecuteQuery(ctx, driver, cypher, params, neo4j.EagerResultTransformer, neo4j.ExecuteQueryWithDatabase("neo4j"))
		if err != nil {
			log.Printf("Error syncing %s: %v", targetName, err)
		} else {
			// Check if it actually matched
			if len(res.Records) > 0 {
				c := res.Records[0].Values[0].(int64)
				if c > 0 {
					count++
					fmt.Printf("Linked %s -> %s\n", targetName, node.ElementId)
				}
			}
		}
	}
	fmt.Printf("Synced %d %s relationships.\n", count, relType)
}
