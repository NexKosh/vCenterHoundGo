package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"sort"
	"strings"
	"vcenterhoundgo/internal/bloodhound"
	"vcenterhoundgo/internal/collector"
	"vcenterhoundgo/internal/config"
	"vcenterhoundgo/internal/graph"
	"vcenterhoundgo/internal/output"
)

func main() {
	// 1. Parse Arguments
	server := flag.String("s", "", "vCenter server(s), comma-separated")
	user := flag.String("u", "", "Username")
	password := flag.String("p", "", "Password")
	port := flag.Int("P", 443, "Port")
	outPath := flag.String("o", "vcenter_graph.json", "Output file")

	debug := flag.Bool("debug", false, "Enable debug logging and extended summary")

	// BloodHound Integration
	bhURL := flag.String("bh-url", "", "BloodHound URL (e.g. https://bloodhound.example.com)")
	bhKeyID := flag.String("bh-key-id", "", "BloodHound Key ID")
	bhKeySecret := flag.String("bh-key-secret", "", "BloodHound Key Secret")

	flag.Parse()

	if *server == "" || *user == "" || *password == "" {
		fmt.Println("Usage: vCenterHound -s <host> -u <user> -p <pass>")
		flag.PrintDefaults()
		os.Exit(1)
	}

	// Resolve Domains if BH config provided
	var domainMap map[string]string
	if *bhURL != "" && *bhKeyID != "" && *bhKeySecret != "" {
		log.Println("Connecting to BloodHound to fetch domain map...")
		bhClient := bloodhound.NewClient(*bhURL, *bhKeyID, *bhKeySecret)
		dMap, err := bhClient.GetDomainMap()
		if err != nil {
			log.Printf("Warning: Failed to fetch domains from BloodHound: %v. Sync edges will be skipped.", err)
		} else {
			domainMap = dMap
			log.Printf("Retrieved %d domains from BloodHound", len(domainMap))
			if *debug {
				for nb, fqdn := range domainMap {
					log.Printf("[DEBUG] Map: %s -> %s", nb, fqdn)
				}
			}
		}
	} else {
		log.Println("BloodHound credentials not provided. Sync edges will be skipped.")
	}

	hosts := strings.Split(*server, ",")
	gb := graph.NewBuilder()

	log.Printf("Starting vCenterHound by Javier Azofra Ovejero")
	log.Printf("Targeting %d vCenter hosts", len(hosts))

	// 2. Collect from each vCenter (Sequentially for simplicity, or Parallel if needed)
	// Given we are refactoring for speed, we can parallelize across vCenters too!
	// But let's stick to parallel collection *within* each collector for stability first.

	for _, host := range hosts {
		host = strings.TrimSpace(host)
		if host == "" {
			continue
		}

		cfg := config.Config{
			Host:        host,
			User:        *user,
			Password:    *password,
			Port:        *port,
			OutputPath:  *outPath,
			Debug:       *debug,
			BHURL:       *bhURL,
			BHKeyID:     *bhKeyID,
			BHKeySecret: *bhKeySecret,
		}

		col := collector.NewCollector(cfg, gb, domainMap)
		if err := col.Connect(); err != nil {
			log.Printf("Failed to connect to %s: %v", host, err)
			continue
		}

		col.Collect()
	}

	// 3. Export
	log.Println("Exporting graph...")
	data := gb.Export()

	if err := output.WriteToFile(data, *outPath); err != nil {
		log.Fatalf("Failed to write output: %v", err)
	}

	log.Printf("Success! Graph saved to %s", *outPath)
	log.Printf("Stats: %d Nodes, %d Edges", len(data.Nodes), len(data.Edges))

	if *debug {
		printExtendedSummary(data)
	}
}

func printExtendedSummary(data graph.GraphData) {
	log.Println("--- Extended Summary ---")

	// Node Types
	nodeCounts := make(map[string]int)
	for _, n := range data.Nodes {
		for _, k := range n.Kinds {
			nodeCounts[k]++
		}
	}

	log.Println("Node Types:")
	var nodeKinds []string
	for k := range nodeCounts {
		nodeKinds = append(nodeKinds, k)
	}
	sort.Strings(nodeKinds)
	for _, k := range nodeKinds {
		log.Printf("  %s: %d", k, nodeCounts[k])
	}

	// Edge Types
	edgeCounts := make(map[string]int)
	for _, e := range data.Edges {
		edgeCounts[e.Kind]++
	}

	log.Println("Edge Types:")
	var edgeKinds []string
	for k := range edgeCounts {
		edgeKinds = append(edgeKinds, k)
	}
	sort.Strings(edgeKinds)
	for _, k := range edgeKinds {
		log.Printf("  %s: %d", k, edgeCounts[k])
	}
	log.Println("------------------------")
}
