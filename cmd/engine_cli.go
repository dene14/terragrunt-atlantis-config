package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/hashicorp/hcl/v2/gohcl"
	"github.com/hashicorp/hcl/v2/hclparse"
	log "github.com/sirupsen/logrus"
)

// CLI engine: native Terragrunt discovery.
//
// The library engine embeds a pinned terragrunt parser. That works while the
// library's public API is stable, but terragrunt v1.x moved parsing behind
// internal packages on purpose: upstream wants the CLI, not the Go API, to be
// the integration surface. The CLI engine honors that design by asking the
// terragrunt binary themselves for discovery data:
//
//	terragrunt find --json --dependencies --reading
//
// Advantages over the library engine:
//   - works with any terragrunt version Atlantis is deployed with (v1.x
//     included), because parsing semantics are whatever the CLI implements
//   - stacks are discovered natively, including nested stacks
//   - "reading" reports every file a config consumed (include chains,
//     values files, ...), which makes autoplan triggers exact
//
// The trade-offs are documented in README.md: atlantis_* locals and
// per-module overrides require evaluating HCL, which the CLI engine
// deliberately does not do.

const (
	engineAuto    = "auto"
	engineLibrary = "library"
	engineCLI     = "cli"
)

var engine string

// cliComponent is one entry of `terragrunt find --json` output.
type cliComponent struct {
	Type         string   `json:"type"` // "unit" or "stack"
	Path         string   `json:"path"`
	Dependencies []string `json:"dependencies"`
	Reading      []string `json:"reading"`
}

// cliEngineError is returned for any CLI engine failure with actionable text.
func cliEngineError(format string, args ...interface{}) error {
	return fmt.Errorf("cli engine: "+format, args...)
}

// locateTerragruntCLI finds the terragrunt binary on PATH.
func locateTerragruntCLI() (string, error) {
	bin, err := exec.LookPath("terragrunt")
	if err != nil {
		return "", cliEngineError("terragrunt binary not found on PATH; " +
			"install terragrunt v1+ or use --engine=library")
	}
	return bin, nil
}

var cliVersionRegex = regexp.MustCompile(`v?(\d+)\.(\d+)\.(\d+)`)

// terragruntCLIMajor returns the major version of the terragrunt binary and
// whether the version could be determined at all.
func terragruntCLIMajor(bin string) (int, bool) {
	out, err := exec.Command(bin, "--version").Output()
	if err != nil {
		return 0, false
	}
	m := cliVersionRegex.FindSubmatch(out)
	if m == nil {
		return 0, false
	}
	major := 0
	fmt.Sscanf(string(m[1]), "%d", &major)
	return major, true
}

// resolveEngine turns the --engine flag into the engine actually used.
// auto prefers the CLI engine when a terragrunt v1+ binary is available and
// falls back to the library engine otherwise, so a new terragrunt install
// never breaks generation.
func resolveEngine(gitRoot string) string {
	switch engine {
	case engineLibrary, engineCLI:
		return engine
	case engineAuto:
		bin, err := locateTerragruntCLI()
		if err != nil {
			log.Debugf("engine=auto: no terragrunt binary, using library engine")
			return engineLibrary
		}
		if major, ok := terragruntCLIMajor(bin); ok && major >= 1 {
			log.Debugf("engine=auto: terragrunt v%d detected, using cli engine", major)
			return engineCLI
		}
		log.Debugf("engine=auto: terragrunt v0.x detected, using library engine")
		return engineLibrary
	default:
		return engineLibrary
	}
}

// runTerragruntFind shells out to `terragrunt find` anchored at root and
// decodes the JSON component list.
func runTerragruntFind(ctx context.Context, bin, root string) ([]cliComponent, error) {
	args := []string{
		"--log-disable",
		"--no-color",
		"--working-dir", root,
		"find",
		"--format=json",
		"--dependencies",
		"--reading",
	}
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Dir = root

	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, cliEngineError("`terragrunt find` failed: %v\n%s", err, strings.TrimSpace(stderr.String()))
	}

	var components []cliComponent
	if err := json.Unmarshal([]byte(stdout.String()), &components); err != nil {
		return nil, cliEngineError("could not parse `terragrunt find` output as JSON: %v", err)
	}

	// terragrunt emits platform-native path separators; normalize every
	// path-shaped field to forward slashes right at the boundary so the whole
	// engine (and golden fixtures) is OS-independent. Note: an unconditional
	// replace is deliberate — filepath.ToSlash only converts the host OS'
	// separator, which means backslashes in Windows-produced JSON would stay
	// backslashes when processing that data off-Windows (e.g. in tests).
	norm := func(s string) string { return strings.ReplaceAll(s, "\\", "/") }
	for i := range components {
		components[i].Path = norm(components[i].Path)
		for j, dep := range components[i].Dependencies {
			components[i].Dependencies[j] = norm(dep)
		}
		for j, r := range components[i].Reading {
			components[i].Reading[j] = norm(r)
		}
	}
	return components, nil
}

// filterComponents keeps components whose path is equal to or below any of
// the filter expressions, mirroring the library engine's --filter semantics
// (paths or globs relative to the root).
func filterComponents(components []cliComponent, filters []string) []cliComponent {
	if len(filters) == 0 {
		return components
	}
	kept := []cliComponent{}
	for _, c := range components {
		for _, f := range filters {
			f = filepath.ToSlash(f)
			matched, _ := filepath.Match(f, c.Path)
			if matched || c.Path == f || strings.HasPrefix(c.Path, strings.TrimSuffix(f, "/")+"/") {
				kept = append(kept, c)
				break
			}
		}
	}
	return kept
}

// componentWatchFiles computes autoplan when_modified entries for one
// discovered component, relative to its own directory (Atlantis semantics).
func componentWatchFiles(c cliComponent, componentDir string) []string {
	watch := []string{"*.hcl", "*.tf*"}

	if c.Type == "stack" {
		// A stack watches its whole subtree: unit references inside
		// terragrunt.stack.hcl may point anywhere underneath it.
		watch = append(watch, "**/*.hcl", "**/*.tf*")
	}

	// Files terragrunt itself reported as read while parsing this component:
	// include chains, values files, shared hcl. Exact file watches — no glob
	// guessing for the parts terragrunt already knows.
	for _, r := range c.Reading {
		entry := watchEntryForPath(c.Path, r)
		// An own-directory .hcl/.tf* file is already covered by the base
		// patterns above; listing it again only adds noise.
		if filepath.Dir(entry) == "." {
			if okH, _ := filepath.Match("*.hcl", entry); okH {
				continue
			}
			if okT, _ := filepath.Match("*.tf*", entry); okT {
				continue
			}
		}
		watch = append(watch, entry)
	}
	return watch
}

// stackSourceProbe statically reads only the `source` attributes of unit and
// stack blocks from a terragrunt.stack.hcl file. Discovery is the CLI's job,
// but the CLI reports no "this stack depends on its unit sources" edges, so
// without this probe editing a shared unit catalog would never re-trigger the
// stack's Atlantis project. The probe is intentionally literal-only: sources
// computed by functions can't be known without generating the stack.
type stackSourceProbe struct {
	Units []struct {
		Name   string  `hcl:"name,label"`
		Source *string `hcl:"source,attr"`
	} `hcl:"unit,block"`
	Stacks []struct {
		Name   string  `hcl:"name,label"`
		Source *string `hcl:"source,attr"`
	} `hcl:"stack,block"`
}

// stackLocalSourceDirs returns directories referenced as local sources by the
// given stack file, resolved to paths relative to the discovery root.
// Remote sources (git, s3, ...) cannot be watched in-repo and are skipped.
func stackLocalSourceDirs(stackFile string, root string) []string {
	raw, err := readFileAsString(stackFile)
	if err != nil {
		return nil
	}

	file, diags := hclparse.NewParser().ParseHCL([]byte(raw), stackFile)
	if diags != nil && diags.HasErrors() {
		return nil
	}

	probe := stackSourceProbe{}
	// Literal-only decode: diagnostics from non-literal sources (function
	// calls, interpolations) are expected and simply leave those fields unset.
	_ = gohcl.DecodeBody(file.Body, nil, &probe)

	dirs := []string{}
	stackDir := filepath.Dir(stackFile)
	collect := func(source *string) {
		if source == nil || *source == "" {
			return
		}
		src := *source
		if !filepath.IsAbs(src) {
			src = filepath.Clean(filepath.Join(stackDir, src))
		}
		if rel, err := filepath.Rel(root, src); err == nil && !strings.HasPrefix(rel, "..") {
			dirs = append(dirs, filepath.ToSlash(rel))
		}
	}
	for _, u := range probe.Units {
		collect(u.Source)
	}
	for _, s := range probe.Stacks {
		collect(s.Source)
	}
	return dirs
}

// watchEntryForPath renders one known file path (relative to the discovery
// root) as a when_modified entry relative to the component's directory.
func watchEntryForPath(componentDir, filePath string) string {
	rel, err := filepath.Rel(componentDir, filePath)
	if err != nil {
		return filepath.ToSlash(filePath)
	}
	return filepath.ToSlash(rel)
}

// dependencyWatchDirs renders dependency directories as when_modified globs
// relative to the component's directory.
func dependencyWatchDirs(componentDir string, deps []string) []string {
	watch := []string{}
	for _, dep := range deps {
		relDir := watchEntryForPath(componentDir, dep)
		watch = append(watch, relDir+"/**/*.hcl", relDir+"/**/*.tf*")
	}
	return watch
}

// transitiveDependencies expands direct dependency edges through the full
// component graph (the CLI reports direct edges only), so the engine can
// honor --cascade-dependencies both ways.
func transitiveDependencies(components []cliComponent, direct map[string][]string) map[string][]string {
	known := make(map[string]bool, len(components))
	for _, c := range components {
		known[c.Path] = true
	}

	memo := map[string][]string{}
	var visit func(path string, seen map[string]bool) []string
	visit = func(path string, seen map[string]bool) []string {
		if done, ok := memo[path]; ok {
			return done
		}
		if seen[path] {
			return nil // dependency cycle; terragrunt will reject it at run time
		}
		seen[path] = true
		out := []string{}
		for _, d := range direct[path] {
			out = append(out, d)
			out = append(out, visit(d, seen)...)
		}
		memo[path] = uniqueStrings(out)
		return memo[path]
	}

	result := map[string][]string{}
	for _, c := range components {
		result[c.Path] = visit(c.Path, map[string]bool{})
	}
	return result
}

// cliEngineProjects converts discovered components into Atlantis projects.
func cliEngineProjects(components []cliComponent, root string) ([]AtlantisProject, error) {
	before := len(components)
	components = filterComponents(components, filterPaths)
	if len(filterPaths) > 0 && len(components) == 0 && before > 0 {
		return nil, cliEngineError("--filter %q matched no discovered components", strings.Join(filterPaths, ", "))
	}

	direct := make(map[string][]string, len(components))
	for _, c := range components {
		direct[c.Path] = c.Dependencies
	}

	depGraph := direct
	if cascadeDependencies {
		depGraph = transitiveDependencies(components, direct)
	}

	projects := make([]AtlantisProject, 0, len(components))
	for _, c := range components {
		dir := filepath.ToSlash(c.Path)
		if dir == "" {
			dir = "."
		}

		watch := componentWatchFiles(c, c.Path)
		if !ignoreDependencyBlocks {
			watch = append(watch, dependencyWatchDirs(c.Path, depGraph[c.Path])...)
		}
		if c.Type == "stack" {
			// The CLI does not report edges from a stack to the unit sources
			// it generates from; watch them explicitly (see stackSourceProbe).
			stackFile := filepath.Join(root, c.Path, "terragrunt.stack.hcl")
			for _, srcDir := range stackLocalSourceDirs(stackFile, root) {
				if srcDir == c.Path {
					continue
				}
				relDir := watchEntryForPath(c.Path, srcDir)
				watch = append(watch, relDir+"/**/*.hcl", relDir+"/**/*.tf*")
			}
		}

		workflow := defaultWorkflow
		if c.Type == "stack" && stackWorkflow != "" {
			workflow = stackWorkflow
		}

		// Sort for deterministic output: terragrunt's discovery order depends
		// on the filesystem walk, which differs between OSes. A generator must
		// emit byte-stable atlantis.yaml for a given commit.
		sortedWatch := uniqueStrings(watch)
		sort.Strings(sortedWatch)

		project := AtlantisProject{
			Dir:                   dir,
			Workflow:              workflow,
			TerraformVersion:      defaultTerraformVersion,
			TerraformDistribution: defaultTerraformDistribution,
			Autoplan: AutoplanConfig{
				Enabled:      autoPlan,
				WhenModified: sortedWatch,
			},
		}

		if len(defaultApplyRequirements) > 0 {
			reqs := defaultApplyRequirements
			project.ApplyRequirements = &reqs
		}

		projectName := projectNameRegex.ReplaceAllString(project.Dir, "_")
		if createProjectName {
			project.Name = projectName
		}
		if createWorkspace {
			project.Workspace = projectName
		}

		projects = append(projects, project)
	}

	// Ordering features work straight off the discovered dependency graph —
	// the cli engine has exact edges, so it does not need to re-derive them
	// from when_modified globs like the library engine does.
	if executionOrderGroups || dependsOn {
		applyCLIOrdering(projects, direct)
	}

	if executionOrderGroups {
		// Same presentation order as the library engine: by group then dir
		sort.Slice(projects, func(i, j int) bool {
			a, b := projects[i].ExecutionOrderGroup, projects[j].ExecutionOrderGroup
			if a != nil && b != nil && *a != *b {
				return *a < *b
			}
			return projects[i].Dir < projects[j].Dir
		})
	} else {
		sort.Slice(projects, func(i, j int) bool { return projects[i].Dir < projects[j].Dir })
	}
	return projects, nil
}

// intPtr returns a fresh pointer for a group value.
func intPtr(v int) *int { return &v }

// applyCLIOrdering assigns execution_order_group and depends_on from the
// direct dependency edges reported by terragrunt (kept projects only).
func applyCLIOrdering(projects []AtlantisProject, direct map[string][]string) {
	included := make(map[string]*AtlantisProject, len(projects))
	for i := range projects {
		included[projects[i].Dir] = &projects[i]
	}

	var namesOf []string
	groups := map[string]int{}

	changed := true
	for rounds := 0; changed && rounds <= len(projects); rounds++ {
		changed = false
		for i := range projects {
			dir := projects[i].Dir
			best := groups[dir]
			for _, depDir := range direct[dir] {
				if _, ok := included[depDir]; !ok || depDir == dir {
					continue
				}
				if groups[depDir]+1 > best {
					best = groups[depDir] + 1
				}
			}
			if best != groups[dir] || (executionOrderGroups && projects[i].ExecutionOrderGroup == nil) {
				groups[dir] = best
				if executionOrderGroups {
					projects[i].ExecutionOrderGroup = intPtr(best)
				}
				changed = true
			}
		}
	}
	if changed {
		log.Warn("Computing execution_order_groups failed. Probably cycle exists")
	}

	if dependsOn {
		for i := range projects {
			if projects[i].Name == "" {
				continue
			}
			namesOf = namesOf[:0]
			seen := map[string]bool{}
			for _, depDir := range direct[projects[i].Dir] {
				dep, ok := included[depDir]
				if !ok || dep.Name == "" || depDir == projects[i].Dir || seen[dep.Name] {
					continue
				}
				seen[dep.Name] = true
				namesOf = append(namesOf, dep.Name)
			}
			projects[i].DependsOn = append([]string(nil), namesOf...)
		}
	}
}

// generateProjectsWithCLIEngine is the cli engine's discovery+projection
// pipeline, producing the same AtlantisProject values the library engine
// produces so all downstream merging/writing logic is shared.
func generateProjectsWithCLIEngine(root string) ([]AtlantisProject, error) {
	if len(projectHclFiles) > 0 {
		return nil, cliEngineError("--project-hcl-files is not supported with --engine=cli")
	}

	bin, err := locateTerragruntCLI()
	if err != nil {
		return nil, err
	}

	ctx := context.Background()
	components, err := runTerragruntFind(ctx, bin, root)
	if err != nil {
		return nil, err
	}

	nUnits, nStacks := 0, 0
	for _, c := range components {
		if c.Type == "stack" {
			nStacks++
		} else {
			nUnits++
		}
	}
	log.Infof("cli engine: discovered %d units and %d stacks via terragrunt", nUnits, nStacks)

	return cliEngineProjects(components, root)
}
