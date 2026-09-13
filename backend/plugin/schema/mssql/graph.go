package mssql

import (
	"slices"

	"github.com/pkg/errors"
)

// dependencyGraph orders DDL statements so that an object is created before the
// objects that depend on it.
type dependencyGraph struct {
	nodes map[string]struct{}
	edges []graphEdge
}

type graphEdge struct {
	start string
	end   string
}

func newDependencyGraph() *dependencyGraph {
	return &dependencyGraph{nodes: make(map[string]struct{})}
}

func (g *dependencyGraph) addNode(id string) {
	g.nodes[id] = struct{}{}
}

func (g *dependencyGraph) addEdge(start, end string) {
	g.addNode(start)
	g.addNode(end)
	g.edges = append(g.edges, graphEdge{start: start, end: end})
}

// topologicalSort returns the node ids so that every edge starts before it
// ends. Ties are broken lexicographically to keep the output stable. Nodes that
// take part in a cycle make the whole graph unsortable and are reported as an
// error so the caller can fall back to a deterministic order.
func (g *dependencyGraph) topologicalSort() ([]string, error) {
	inDegree := make(map[string]int, len(g.nodes))
	outEdges := make(map[string][]string)
	for _, edge := range g.edges {
		inDegree[edge.end]++
		outEdges[edge.start] = append(outEdges[edge.start], edge.end)
	}

	var queue []string
	for id := range g.nodes {
		if inDegree[id] == 0 {
			queue = append(queue, id)
		}
	}
	slices.Sort(queue)

	result := make([]string, 0, len(g.nodes))
	for len(queue) > 0 {
		node := queue[0]
		queue = queue[1:]
		for _, next := range outEdges[node] {
			inDegree[next]--
			if inDegree[next] == 0 {
				queue = append(queue, next)
			}
		}
		result = append(result, node)
	}

	if len(result) != len(g.nodes) {
		return nil, errors.New("the dependency graph has a cycle")
	}
	return result, nil
}
