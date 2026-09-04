package cmd

import (
	"path/filepath"
	"strings"
)

// Cycle detection for the project dependency graph (upstream issue
// transcend-io/terragrunt-atlantis-config#191). Previously a cycle only
// surfaced as "Computing execution_order_groups failed" with no names.

// findProjectCycle returns one cycle (as a path of project dirs, first==last)
// or nil if the induced graph is acyclic. Standard iterative DFS over the
// among-project edges.
func findProjectCycle(projects []AtlantisProject) []string {
	edges := map[string][]string{}
	for _, p := range projects {
		for _, dep := range orderingInputs(p) {
			depPath := filepath.ToSlash(filepath.Dir(filepath.Join(p.Dir, dep)))
			if depPath == p.Dir {
				continue
			}
			edges[p.Dir] = append(edges[p.Dir], depPath)
		}
	}

	const (
		white = iota // unvisited
		gray         // on current DFS stack
		black        // fully explored
	)
	color := map[string]int{}
	stack := []string{}

	var visit func(string) []string
	visit = func(node string) []string {
		color[node] = gray
		stack = append(stack, node)
		for _, next := range edges[node] {
			if color[next] == gray {
				// copy the cycle slice to detach it from the live stack
				for i, n := range stack {
					if n == next {
						cycle := append([]string{}, stack[i:]...)
						return append(cycle, next)
					}
				}
			}
			if color[next] == white {
				if cycle := visit(next); cycle != nil {
					return cycle
				}
			}
		}
		stack = stack[:len(stack)-1]
		color[node] = black
		return nil
	}

	for _, p := range projects {
		if color[p.Dir] == white {
			if cycle := visit(p.Dir); cycle != nil {
				return cycle
			}
		}
	}
	return nil
}

// formatProjectCycle renders a cycle for log output: a -> b -> c -> a
func formatProjectCycle(cycle []string) string {
	return strings.Join(cycle, " -> ")
}
