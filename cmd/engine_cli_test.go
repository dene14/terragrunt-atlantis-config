package cmd

import (
	"reflect"
	"strings"
	"testing"
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
// dependency edges come straight from terragrunt's discovery output.
func TestGenerateCLIEngineOrdering(t *testing.T) {
	terragruntCLIOrSkip(t)
	runTest(t, "golden/engine_cli_ordering.yaml", []string{
		"--engine", "cli",
		"--execution-order-groups",
		"--depends-on",
		"--create-project-name",
		"--root", "../test_examples/chained_dependencies",
	})
}
