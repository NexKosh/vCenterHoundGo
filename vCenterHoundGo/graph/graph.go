package graph

import (
	"fmt"
	"sort"
	"strings"
)

// GraphNode represents a node in the graph
type GraphNode struct {
	Kinds      []string               `json:"kinds"`
	ID         string                 `json:"id"`
	Properties map[string]interface{} `json:"properties"`
}

// GraphEdge represents an edge in the graph
type GraphEdge struct {
	Kind       string                 `json:"kind"`
	StartID    map[string]string      `json:"start"`
	EndID      map[string]string      `json:"end"`
	Properties map[string]interface{} `json:"properties"`
}

// GraphStructure for final JSON output
type GraphStructure struct {
	Nodes []GraphNode `json:"nodes"`
	Edges []GraphEdge `json:"edges"`
}

// FinalOutput represents the root JSON object
type FinalOutput struct {
	Graph GraphStructure `json:"graph"`
}

// GraphBuilder builds the graph structure
type GraphBuilder struct {
	NodesByID map[string]*GraphNode
	Edges     []GraphEdge
	EdgeKeys  map[string]bool
}

// NewGraphBuilder initializes a new GraphBuilder
func NewGraphBuilder() *GraphBuilder {
	return &GraphBuilder{
		NodesByID: make(map[string]*GraphNode),
		Edges:     []GraphEdge{},
		EdgeKeys:  make(map[string]bool),
	}
}

// Constants for Prefixes
const (
	NodePrefix = "vCenter_"
	EdgePrefix = "vCenter_"
)

// NodeTypeMap mappings
var NodeTypeMap = map[string]string{
	"vCenter":           "vCenter",
	"RootFolder":        "RootFolder",
	"Datacenter":        "Datacenter",
	"Cluster":           "Cluster",
	"ESXiHost":          "ESXiHost",
	"ResourcePool":      "ResourcePool",
	"vApp":              "vApp",
	"VM":                "VM",
	"VMTemplate":        "VMTemplate",
	"Datastore":         "Datastore",
	"DatastoreCluster":  "DatastoreCluster",
	"Network":           "Network",
	"StandardPortgroup": "StandardPortgroup",
	"DVSwitch":          "DVSwitch",
	"DVPortgroup":       "DVPortgroup",
	"Principal":         "Principal",
	"User":              "User",
	"Group":             "Group",
	"Privilege":         "Privilege",
	"Role":              "Role",
	"Folder":            "Folder",
	"IdentityDomain":    "IdentityDomain",
}

// EdgeTypeMap mappings
var EdgeTypeMap = map[string]string{
	"CONTAINS":       "Contains",
	"HOSTS":          "Hosts",
	"HAS_PERMISSION": "HasPermission",
	"MEMBER_OF":      "MemberOf",
	"USES_DATASTORE": "UsesDatastore",
	"USES_NETWORK":   "UsesNetwork",
	"HAS_DATASTORE":  "HasDatastore",
	"HAS_NETWORK":    "HasNetwork",
	"MOUNTS":         "Mounts",
	"HAS_PRIVILEGE":  "HasPrivilege",
}

// FormatNodeKind formats the node kind with prefix
func (gb *GraphBuilder) FormatNodeKind(kind string) string {
	mapped, ok := NodeTypeMap[kind]
	if !ok {
		mapped = kind
	}
	cleaned := strings.ReplaceAll(mapped, ".", "_")
	cleaned = strings.ReplaceAll(cleaned, "-", "_")
	cleaned = strings.ReplaceAll(cleaned, " ", "_")

	if NodePrefix != "" {
		return NodePrefix + cleaned
	}
	return cleaned
}

// FormatEdgeKind formats the edge kind with prefix
func (gb *GraphBuilder) FormatEdgeKind(kind string) string {
	mapped, ok := EdgeTypeMap[kind]
	if !ok {
		mapped = kind
	}
	if EdgePrefix != "" {
		return EdgePrefix + mapped
	}
	return mapped
}

// PropsToKey converts edge properties to a string key for deduplication
func (gb *GraphBuilder) propsToKey(props map[string]interface{}) string {
	if len(props) == 0 {
		return ""
	}
	keys := make([]string, 0, len(props))
	for k := range props {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	parts := make([]string, len(keys))
	for i, k := range keys {
		val := props[k]
		parts[i] = fmt.Sprintf("%v:%v", k, val)
	}
	return strings.Join(parts, "|")
}

// AddEdge adds an edge to the graph with deduplication
func (gb *GraphBuilder) AddEdge(kind string, startID string, endID string, properties map[string]interface{}) {
	if properties == nil {
		properties = make(map[string]interface{})
	}

	propsKey := gb.propsToKey(properties)
	edgeKey := fmt.Sprintf("%s:%s:%s:%s", kind, startID, endID, propsKey)

	if gb.EdgeKeys[edgeKey] {
		return // Skip duplicate
	}

	gb.EdgeKeys[edgeKey] = true
	formattedKind := gb.FormatEdgeKind(kind)

	edge := GraphEdge{
		Kind:       formattedKind,
		StartID:    map[string]string{"value": startID},
		EndID:      map[string]string{"value": endID},
		Properties: properties,
	}
	gb.Edges = append(gb.Edges, edge)
}

// EnsureNode ensures a node exists in the graph, updating properties if it does
func (gb *GraphBuilder) EnsureNode(kinds []string, nodeID string, properties map[string]interface{}) *GraphNode {
	if existing, exists := gb.NodesByID[nodeID]; exists {
		// Update kinds (union)
		existingKindMap := make(map[string]bool)
		for _, k := range existing.Kinds {
			existingKindMap[k] = true
		}

		for _, k := range kinds {
			formatted := gb.FormatNodeKind(k)
			if !existingKindMap[formatted] {
				existing.Kinds = append(existing.Kinds, formatted)
				existingKindMap[formatted] = true
			}
		}

		// Update properties (overwrite existing, like Python .update())
		for k, v := range properties {
			existing.Properties[k] = v
		}

		return existing
	}

	formattedKinds := make([]string, len(kinds))
	for i, k := range kinds {
		formattedKinds[i] = gb.FormatNodeKind(k)
	}

	node := &GraphNode{
		Kinds:      formattedKinds,
		ID:         nodeID,
		Properties: properties,
	}
	gb.NodesByID[nodeID] = node
	return node
}

// ToOutput returns the final graph structure
func (gb *GraphBuilder) ToOutput() FinalOutput {
	nodes := make([]GraphNode, 0, len(gb.NodesByID))
	for _, node := range gb.NodesByID {
		nodes = append(nodes, *node)
	}
	return FinalOutput{
		Graph: GraphStructure{
			Nodes: nodes,
			Edges: gb.Edges,
		},
	}
}
