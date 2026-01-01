package postprocess

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"
	"vcenterhoundgo/internal/bloodhound"
	"vcenterhoundgo/internal/graph"
	"vcenterhoundgo/internal/output"
)

type Processor struct {
	BHClient  *bloodhound.Client
	DomainMap map[string]string
}

func NewProcessor(bhURL, bhKeyID, bhKeySecret string) *Processor {
	var client *bloodhound.Client
	if bhURL != "" {
		client = bloodhound.NewClient(bhURL, bhKeyID, bhKeySecret)
	}
	return &Processor{
		BHClient:  client,
		DomainMap: make(map[string]string),
	}
}

func (p *Processor) Run(inputPath, outputPath string) error {
	// 1. Fetch Domain Map if BH Client is available
	if p.BHClient != nil {
		log.Println("Connecting to BloodHound to fetch domain map...")
		dMap, err := p.BHClient.GetDomainMap()
		if err != nil {
			log.Printf("Warning: Failed to fetch domains from BloodHound: %v. Sync edges will be skipped.", err)
		} else {
			p.DomainMap = dMap
			log.Printf("Retrieved %d domains from BloodHound", len(p.DomainMap))
		}
	} else {
		log.Println("No BloodHound credentials provided. Skipping domain sync.")
	}

	// 2. Read Input JSON
	log.Printf("Reading input graph from %s...", inputPath)
	data, err := os.ReadFile(inputPath)
	if err != nil {
		return fmt.Errorf("failed to read input file: %v", err)
	}

	var wrapper struct {
		Graph graph.GraphData `json:"graph"`
	}
	if err := json.Unmarshal(data, &wrapper); err != nil {
		return fmt.Errorf("failed to parse input JSON: %v", err)
	}
	graphData := wrapper.Graph

	log.Printf("Loaded graph with %d nodes and %d edges.", len(graphData.Nodes), len(graphData.Edges))

	// Reconstruct Builder to add edges easily
	gb := graph.NewBuilder()
	// Populate builder with existing nodes/edges
	countNodes := 0
	countEdges := 0
	for _, n := range graphData.Nodes {
		// We need to parse Kinds properly, stripping "vCenter_" prefix if strictly following Builder logic,
		// but Builder.AddRawNode accepts kinds as is.
		// Ideally we use EnsureNode but that adds prefix. AddRawNode is safer for existing data.
		gb.AddRawNode(n.Kinds, n.ID, n.Properties)
		countNodes++
	}
	for _, e := range graphData.Edges {
		gb.AddRawEdgeWithMatch(e.Kind, e.StartID, e.StartMatchBy, e.EndID, e.EndMatchBy, e.Properties)
		countEdges++
	}
	log.Printf("[DEBUG] Builder re-populated with %d nodes and %d edges.", countNodes, countEdges)

	// 3. Process for Sync Edges
	processedCount := 0
	newEdges := 0

	for _, node := range graphData.Nodes {
		var isUser, isGroup bool
		for _, k := range node.Kinds {
			if k == "vCenter_User" {
				isUser = true
			} else if k == "vCenter_Group" {
				isGroup = true
			}
		}

		if !isUser && !isGroup {
			continue
		}

		// Extract properties
		name, _ := node.Properties["name"].(string)
		username, _ := node.Properties["username"].(string)
		domain, _ := node.Properties["domain"].(string)
		id := node.ID

		if name == "" || domain == "" {
			continue
		}

		processedCount++

		// Check Domain Map
		if fqdn, ok := p.DomainMap[strings.ToUpper(domain)]; ok {
			adPrincipalID := fmt.Sprintf("%s@%s", strings.ToUpper(username), fqdn)

			if isUser {
				// Check if edge already exists? Builder handles deduplication by key.
				gb.AddRawEdgeWithMatch("SyncsTovCenterUser", adPrincipalID, "name", id, "", nil)
				newEdges++
			} else if isGroup {
				gb.AddRawEdgeWithMatch("SyncsTovCenterGroup", adPrincipalID, "name", id, "", nil)
				newEdges++
			}
		}
	}

	log.Printf("Processed %d principals. Added/Ensured %d sync edges.", processedCount, newEdges)

	// 4. Export and Save
	finalData := gb.Export()

	// Write to file using output package to ensure consistent format
	if err := output.WriteToFile(finalData, outputPath); err != nil {
		return fmt.Errorf("failed to write output file: %v", err)
	}

	log.Printf("Successfully saved processed graph to %s", outputPath)
	return nil
}
