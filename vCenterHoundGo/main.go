package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"strings"

	"vCenterHoundGo/collector"
	"vCenterHoundGo/graph"
)

func main() {
	serverPtr := flag.String("s", "", "vCenter server(s) - comma-separated")
	userPtr := flag.String("u", "", "Username")
	passPtr := flag.String("p", "", "Password")
	portPtr := flag.Int("P", 443, "Port number")
	outPtr := flag.String("o", "vcenter_graph.json", "Output file")
	verbosePtr := flag.Bool("v", false, "Verbose logging")
	modePtr := flag.String("mode", "collect", "Execution mode: collect")

	// Long flags
	flag.StringVar(serverPtr, "server", "", "vCenter server(s)")
	flag.StringVar(userPtr, "user", "", "Username")
	flag.StringVar(passPtr, "password", "", "Password")
	flag.IntVar(portPtr, "port", 443, "Port")
	flag.StringVar(outPtr, "output", "vcenter_graph.json", "Output file")
	flag.BoolVar(verbosePtr, "verbose", false, "Verbose logging")

	flag.Parse()

	if *serverPtr == "" || *userPtr == "" || *passPtr == "" {
		flag.Usage()
		os.Exit(1)
	}

	if *modePtr != "collect" {
		log.Printf("Unknown mode: %s. Currently only 'collect' is supported.", *modePtr)
		os.Exit(1)
	}

	if *verbosePtr {
		// Set logging to debug if needed, but standard log is simple enough
		log.SetFlags(log.LstdFlags | log.Lshortfile)
	}

	banner := `
█ █ █▀▀ █▀▀ █▀█ ▀█▀ █▀▀ █▀▄ █ █ █▀█ █ █ █▀█ █▀▄
▀▄▀ █   █▀▀ █ █  █  █▀▀ █▀▄ █▀█ █ █ █ █ █ █ █ █
 ▀  ▀▀▀ ▀▀▀ ▀ ▀  ▀  ▀▀▀ ▀ ▀ ▀ ▀ ▀▀▀ ▀▀▀ ▀ ▀ ▀▀ 
vCenterHoundGo - vCenter to BloodHound Graph Converter
`
	fmt.Println(banner)

	servers := strings.Split(*serverPtr, ",")
	var connectors []*collector.Collector
	mergedGraph := graph.NewGraphBuilder()

	for _, host := range servers {
		host = strings.TrimSpace(host)
		if host == "" {
			continue
		}

		cfg := collector.VCenterConfig{
			Host:     host,
			User:     *userPtr,
			Password: *passPtr,
			Port:     *portPtr,
		}

		// Each collector builds into its own builder? Or shared?
		// Python matches merged graph at the end.
		// Go implementation: simple to have 1 builder or merge them.
		// If we use shared builder, we must be careful with concurrency if we were parallel,
		// but here we are sequential.

		// Let's use individual collectors to isolate failures, then merge.
		gb := graph.NewGraphBuilder() // Individual builder
		col := collector.NewCollector(cfg, gb)

		log.Printf("Starting collection for %s...", host)
		if err := col.Collect(); err != nil {
			log.Printf("Failed to collect from %s: %v", host, err)
			continue
		}

		connectors = append(connectors, col)

		// Merge into mergedGraph
		for _, node := range gb.NodesByID {
			// Manually merge to avoid double-prefixing Kinds (EnsureNode re-formats Kinds)
			if existing, exists := mergedGraph.NodesByID[node.ID]; exists {
				// Merge properties
				for k, v := range node.Properties {
					if _, has := existing.Properties[k]; !has {
						existing.Properties[k] = v
					}
				}
			} else {
				// Copy node directly
				mergedGraph.NodesByID[node.ID] = node
			}
		}
		for _, edge := range gb.Edges {
			// Convert start/end map to ID string for AddEdge
			// Remove prefix from kind if present? AddEdge adds it again.
			// Ideally we shouldn't Format twice.
			// But GraphBuilder.AddEdge takes raw Kind.
			// GraphEdge struct stores Formatted Kind.
			// So merging requires raw Kind? Or we just append Edges directly?
			// Direct append is safer for exact copy.
			mergedGraph.Edges = append(mergedGraph.Edges, edge)
		}
	}

	if len(connectors) == 0 {
		log.Println("No data collected from any vCenter")
		os.Exit(1)
	}

	// Save output
	log.Println("Saving output files...")

	finalOutput := mergedGraph.ToOutput()

	// Sanitize output (convert nil to "", list elements, etc. - matching Python logic)
	sanitizeGraph(&finalOutput)

	// Custom encoder to disable HTML escaping
	var buf strings.Builder
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")

	if err := enc.Encode(finalOutput); err != nil {
		log.Fatalf("Error marshaling JSON: %v", err)
	}

	data := []byte(buf.String())

	// Write with BOM for UTF-8-SIG compatibility
	bom := []byte{0xEF, 0xBB, 0xBF}
	fullData := append(bom, data...)

	if err := ioutil.WriteFile(*outPtr, fullData, 0644); err != nil {
		log.Fatalf("Error writing file: %v", err)
	}

	log.Printf("Graph saved: %s", *outPtr)
	log.Printf("Total nodes: %d", len(finalOutput.Graph.Nodes))
	log.Printf("Total edges: %d", len(finalOutput.Graph.Edges))
	log.Println("vCenterHoundGo collection completed successfully")
}

func sanitizeGraph(output *graph.FinalOutput) {
	for i := range output.Graph.Nodes {
		sanitizeProperties(output.Graph.Nodes[i].Properties)
	}
	for i := range output.Graph.Edges {
		sanitizeProperties(output.Graph.Edges[i].Properties)
	}
}

func sanitizeProperties(props map[string]interface{}) {
	for k, v := range props {
		props[k] = sanitizeValue(v)
	}
}

func sanitizeValue(v interface{}) interface{} {
	if v == nil {
		return ""
	}
	switch val := v.(type) {
	case []interface{}:
		newSlice := make([]interface{}, len(val))
		for i, item := range val {
			newSlice[i] = sanitizeValue(item)
		}
		return newSlice
	case []string:
		// Convert strings to interface slice loop? Or just return as is (strings cant be nil)
		return val
	case map[string]interface{}:
		for k, subV := range val {
			val[k] = sanitizeValue(subV)
		}
		return val
	case bool:
		return val
	case int, int32, int64, float32, float64:
		return val
	case string:
		return val
	default:
		// Fallback for complex types -> string
		return fmt.Sprintf("%v", val)
	}
}
