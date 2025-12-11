package output

import (
	"encoding/json"
	"os"
	"vcenterhoundgo/internal/graph"
)

// Output structure for JSON file
type Output struct {
	Graph graph.GraphData `json:"graph"`
}

// WriteToFile writes the graph data to a JSON file
func WriteToFile(data graph.GraphData, filename string) error {
	out := Output{
		Graph: data,
	}

	file, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer file.Close()

	enc := json.NewEncoder(file)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}
