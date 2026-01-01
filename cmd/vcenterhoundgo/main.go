package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"sort"
	"strings"
	"vcenterhoundgo/internal/collector"
	"vcenterhoundgo/internal/config"
	"vcenterhoundgo/internal/graph"
	"vcenterhoundgo/internal/output"
	"vcenterhoundgo/internal/postprocess"
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

	// Mode Selection
	mode := flag.String("mode", "collect", "Execution mode: 'collect' (vCenter only) or 'process' (BloodHound sync)")
	inputPath := flag.String("i", "vcenter_graph.json", "Input file for process mode")

	flag.Parse()

	if *mode == "process" {
		runProcessor(*inputPath, *outPath, *bhURL, *bhKeyID, *bhKeySecret)
		return
	}

	// Default to 'collect' mode
	if *server == "" || *user == "" || *password == "" {
		fmt.Println("Usage (Collect Mode): vCenterHound -mode collect -s <host> -u <user> -p <pass>")
		fmt.Println("Usage (Process Mode): vCenterHound -mode process -i <input.json> -o <output.json> -bh-url ...")
		flag.PrintDefaults()
		os.Exit(1)
	}

	hosts := strings.Split(*server, ",")
	gb := graph.NewBuilder()

	log.Printf("Starting vCenterHound (Collect Mode)")
	log.Printf("Targeting %d vCenter hosts", len(hosts))

	for _, host := range hosts {
		host = strings.TrimSpace(host)
		if host == "" {
			continue
		}

		cfg := config.Config{
			Host:       host,
			User:       *user,
			Password:   *password,
			Port:       *port,
			OutputPath: *outPath,
			Debug:      *debug,
			// BH params ignored in collect mode
		}

		// Pass nil map for direct collection (no sync edges in this phase)
		col := collector.NewCollector(cfg, gb, nil)
		if err := col.Connect(); err != nil {
			log.Printf("Failed to connect to %s: %v", host, err)
			continue
		}

		col.Collect()
	}

	// Export
	log.Println("Exporting raw graph...")
	data := gb.Export()

	if err := output.WriteToFile(data, *outPath); err != nil {
		log.Fatalf("Failed to write output: %v", err)
	}

	log.Printf("Success! Raw graph saved to %s", *outPath)
	log.Printf("Stats: %d Nodes, %d Edges", len(data.Nodes), len(data.Edges))

	if *debug {
		printExtendedSummary(data)
	}
}

func runProcessor(input, output, bhURL, bhID, bhSecret string) {
	log.Printf("Starting vCenterHound (Process Mode)")
	proc := postprocess.NewProcessor(bhURL, bhID, bhSecret)
	if err := proc.Run(input, output); err != nil {
		log.Fatalf("Fatal error during processing: %v", err)
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
