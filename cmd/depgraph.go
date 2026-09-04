package cmd

import (
	"path/filepath"
	"sync"
)

// Dependency edges for ordering (--execution-order-groups / --depends-on)
// ------------------------------------------------------
//
// Ordering used to be computed purely from each project's emitted
// when_modified entries. That broke once --ignore-dependency-blocks was set:
// the flag (correctly) keeps dependency blocks out of the autoplan watch
// list, but ordering is about apply/plan *sequence*, which must still respect
// those edges. This small registry tracks dependency-block edges discovered
// during parsing independently from what ends up in when_modified.

var depGraph = struct {
	sync.Mutex
	// project dir (relative to gitRoot) -> dependency file paths relative
	// to that project dir (same shape as when_modified entries)
	edges map[string][]string
}{edges: map[string][]string{}}

// resetDepGraph clears the edge registry; called once per generate run.
func resetDepGraph() {
	depGraph.Lock()
	defer depGraph.Unlock()
	depGraph.edges = map[string][]string{}
}

// recordDepBlockEdges notes that the config file at configPath declares
// dependency-block edges to the given config files (absolute paths).
func recordDepBlockEdges(configPath string, depConfigPaths []string) {
	if len(depConfigPaths) == 0 || gitRoot == "" {
		return
	}

	projectDirAbs := filepath.Dir(configPath)
	projectDirRel, err := filepath.Rel(gitRoot, projectDirAbs)
	if err != nil {
		return
	}
	projectDirRel = filepath.ToSlash(projectDirRel)
	if projectDirRel == "" {
		projectDirRel = "."
	}

	relEdges := make([]string, 0, len(depConfigPaths))
	for _, dep := range depConfigPaths {
		rel, err := filepath.Rel(projectDirAbs, dep)
		if err != nil {
			continue
		}
		relEdges = append(relEdges, filepath.ToSlash(rel))
	}

	depGraph.Lock()
	defer depGraph.Unlock()
	depGraph.edges[projectDirRel] = uniqueStrings(append(depGraph.edges[projectDirRel], relEdges...))
}

// orderingInputs returns every entry ordering should consider for a project:
// the emitted watch entries plus dependency-block edges that may have been
// intentionally excluded from the watch list.
func orderingInputs(project AtlantisProject) []string {
	watch := project.Autoplan.WhenModified

	depGraph.Lock()
	defer depGraph.Unlock()
	extra := depGraph.edges[project.Dir]
	if len(extra) == 0 {
		return watch
	}

	out := make([]string, 0, len(watch)+len(extra))
	out = append(out, watch...)
	out = append(out, extra...)
	return uniqueStrings(out)
}
