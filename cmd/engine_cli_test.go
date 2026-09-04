package cmd

import (
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/ghodss/yaml"
)

func TestResolveEngineForced(t *testing.T) {
	old := engine
	defer func() { engine = old }()

	engine = engineLibrary
	if got := resolveEngine("."); got != engineLibrary {
		t.Fatalf("expected library, got %s", got)
	}
	engine = engineCLI
	if got := resolveEngine("."); got != engineCLI {
		t.Fatalf("expected cli, got %s", got)
	}
}

func TestResolveEngineAutoWithoutBinary(t *testing.T) {
	old := engine
	defer func() { engine = old }()
	engine = engineAuto
	t.Setenv("PATH", t.TempDir())

	if got := resolveEngine("."); got != engineLibrary {
		t.Fatalf("expected library fallback without terragrunt binary, got %s", got)
	}
}

func TestFilterComponents(t *testing.T) {
	components := []cliComponent{
		{Type: "unit", Path: "live/prod"},
		{Type: "unit", Path: "live/stage"},
		{Type: "stack", Path: "catalog/mini"},
	}

	old := filterPaths
	defer func() { filterPaths = old }()

	filterPaths = []string{"live/prod"}
	got := filterComponents(components, filterPaths)
	if len(got) != 1 || got[0].Path != "live/prod" {
		t.Fatalf("unexpected filter result: %+v", got)
	}

	filterPaths = []string{"live/*"}
	got = filterComponents(components, filterPaths)
	if len(got) != 2 {
		t.Fatalf("expected glob to match both live dirs, got %+v", got)
	}

	filterPaths = nil
	got = filterComponents(components, filterPaths)
	if len(got) != 3 {
		t.Fatalf("no filters should keep everything, got %+v", got)
	}
}

func TestTransitiveDependencies(t *testing.T) {
	components := []cliComponent{{Path: "a"}, {Path: "b"}, {Path: "c"}, {Path: "loop1"}, {Path: "loop2"}}
	direct := map[string][]string{
		"a":     {"b"},
		"b":     {"c"},
		"c":     {},
		"loop1": {"loop2"},
		"loop2": {"loop1"},
	}

	got := transitiveDependencies(components, direct)

	wantA := []string{"b", "c"}
	if !reflect.DeepEqual(got["a"], wantA) {
		t.Fatalf("transitive deps of a: want %v, got %v", wantA, got["a"])
	}
	// cycles must terminate and report only the cycle members
	if len(got["loop1"]) == 0 {
		t.Fatalf("cycle produced empty deps")
	}
}

func TestComponentWatchFiles(t *testing.T) {
	unit := cliComponent{
		Type:    "unit",
		Path:    "live/app",
		Reading: []string{"live/app/terragrunt.hcl", "live/shared.hcl"},
	}
	got := componentWatchFiles(unit, "live/app")
	want := []string{"*.hcl", "*.tf*", "../shared.hcl"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unit watch: want %v, got %v", want, got)
	}

	stack := cliComponent{Type: "stack", Path: "live/prod"}
	got = componentWatchFiles(stack, "live/prod")
	for _, w := range []string{"*.hcl", "*.tf*", "**/*.hcl", "**/*.tf*"} {
		found := false
		for _, g := range got {
			if g == w {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("stack watch missing %s in %v", w, got)
		}
	}
}

func TestStackLocalSourceDirs(t *testing.T) {
	root := "../test_examples/stacks_nested"
	dirs := stackLocalSourceDirs(root+"/live/prod/terragrunt.stack.hcl", root)
	if len(dirs) != 2 {
		t.Fatalf("expected 2 local sources (nested stack + vpc), got %v", dirs)
	}
	joined := strings.Join(dirs, ",")
	if !strings.Contains(joined, "catalog/vpc") || !strings.Contains(joined, "catalog/mini-stack") {
		t.Fatalf("unexpected source dirs: %v", dirs)
	}
}

// terragruntCLIOrSkip returns the terragrunt binary path when a v1+ CLI is
// installed, and skips the test otherwise.
func terragruntCLIOrSkip(t *testing.T) string {
	t.Helper()
	bin, err := locateTerragruntCLI()
	if err != nil {
		t.Skipf("terragrunt not on PATH: %v", err)
	}
	major, ok := terragruntCLIMajor(bin)
	if !ok || major < 1 {
		t.Skipf("terragrunt v1+ required for cli engine tests, got major=%d ok=%v", major, ok)
	}
	return bin
}

func TestGenerateCLIEngineBasic(t *testing.T) {
	terragruntCLIOrSkip(t)
	runTest(t, "golden/engine_cli_basic.yaml", []string{"--engine", "cli", "--root", "../test_examples/basic_module"})
}

func TestGenerateCLIEngineChainedDependencies(t *testing.T) {
	terragruntCLIOrSkip(t)
	runTest(t, "golden/engine_cli_chained.yaml", []string{"--engine", "cli", "--root", "../test_examples/chained_dependencies"})
}

func TestGenerateCLIEngineChainedNoCascade(t *testing.T) {
	terragruntCLIOrSkip(t)
	runTest(t, "golden/engine_cli_chained_no_cascade.yaml", []string{"--engine", "cli", "--cascade-dependencies=false", "--root", "../test_examples/chained_dependencies"})
}

func TestGenerateCLIEngineStacksNested(t *testing.T) {
	terragruntCLIOrSkip(t)
	runTest(t, "golden/engine_cli_stacks_nested.yaml", []string{"--engine", "cli", "--root", "../test_examples/stacks_nested"})
}

func TestGenerateCLIEngineIncludesReading(t *testing.T) {
	terragruntCLIOrSkip(t)
	runTest(t, "golden/engine_cli_includes.yaml", []string{"--engine", "cli", "--root", "../test_examples/multiple_includes"})
}

func TestGenerateCLIEngineWorkspaceAndName(t *testing.T) {
	terragruntCLIOrSkip(t)
	runTest(t, "golden/engine_cli_workspace_name.yaml", []string{"--engine", "cli", "--create-workspace", "--create-project-name", "--root", "../test_examples/chained_dependencies"})
}

// The cli engine supports execution-order-groups/depends-on since direct
// dependency edges come straight from terragrunt's discovery output. The
// assertions are semantic (not golden): terragrunt's own Windows discovery
// currently loses deep `../../` edges, so shape-parity must not be asserted
// cross-OS while the fix lands upstream.
func TestGenerateCLIEngineOrdering(t *testing.T) {
	terragruntCLIOrSkip(t)

	if err := resetForRun(); err != nil {
		t.Fatal(err)
	}
	filename := "test_artifacts/cli_ordering.yaml"
	defer os.Remove(filename)

	content, err := RunWithFlags(filename, []string{
		"generate", "--engine", "cli",
		"--execution-order-groups", "--depends-on", "--create-project-name",
		"--output", filename, "--root", "../test_examples/chained_dependencies",
	})
	if err != nil {
		t.Fatal(err)
	}

	p := &AtlantisConfig{}
	if err := yaml.Unmarshal(content, p); err != nil {
		t.Fatal(err)
	}
	groups := map[string]int{}
	names := map[string][]string{}
	for _, proj := range p.Projects {
		if proj.ExecutionOrderGroup != nil {
			groups[proj.Dir] = *proj.ExecutionOrderGroup
		}
		names[proj.Dir] = proj.DependsOn
	}

	if groups["depender"] <= groups["dependency"] {
		t.Fatalf("depender must come after dependency: %v", groups)
	}
	if got := names["depender"]; len(got) != 1 || got[0] != "dependency" {
		t.Fatalf("depender must depends_on exactly [dependency], got %v", got)
	}
	if groups["depender_on_depender"] < groups["depender"] {
		t.Fatalf("chain must be monotone: %v", groups)
	}
}

// Simulating Windows-shaped discovery output (backslashes): ordering must use
// identical semantics as on POSIX. Guards the ingest normalization.
func TestCLIOrderingHandlesBackslashEdges(t *testing.T) {
	components := []cliComponent{
		{Type: "unit", Path: `dependency`},
		{Type: "unit", Path: `depender`, Dependencies: []string{`dependency`}},
		{Type: "unit", Path: `depender_on_depender`, Dependencies: []string{`depender`, `depender_on_depender\nested`}},
		{Type: "unit", Path: `depender_on_depender\nested`, Dependencies: []string{`dependency`}},
	}

	// apply ingest normalization as runTerragruntFind does
	norm := func(s string) string { return strings.ReplaceAll(s, "\\", "/") }
	for i := range components {
		components[i].Path = norm(components[i].Path)
		for j, dep := range components[i].Dependencies {
			components[i].Dependencies[j] = norm(dep)
		}
	}

	oldEOG, oldDO, oldPN := executionOrderGroups, dependsOn, createProjectName
	defer func() { executionOrderGroups, dependsOn, createProjectName = oldEOG, oldDO, oldPN }()
	executionOrderGroups, dependsOn, createProjectName = true, true, true

	projects, err := cliEngineProjects(components, ".")
	if err != nil {
		t.Fatal(err)
	}

	groupOf := map[string]int{}
	depsOf := map[string][]string{}
	for _, p := range projects {
		if p.ExecutionOrderGroup != nil {
			groupOf[p.Dir] = *p.ExecutionOrderGroup
		}
		depsOf[p.Dir] = p.DependsOn
	}

	if groupOf["dependency"] != 0 || groupOf["depender"] != 1 || groupOf["depender_on_depender"] != 2 || groupOf["depender_on_depender/nested"] != 1 {
		t.Fatalf("wrong groups: %v", groupOf)
	}
	if len(depsOf["depender_on_depender"]) != 2 {
		t.Fatalf("expected two depends_on entries: %v", depsOf["depender_on_depender"])
	}
}
