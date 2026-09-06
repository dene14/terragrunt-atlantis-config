package cmd

// Engine contract tests: any behavior visible in atlantis.yaml must be
// identical between the library engine and the cli engine, regardless of how
// each computes it. This suite would have caught the #34 directory-glob
// regression on day one.
//
// Construed as contract (engines must agree):
//   - project discovery set ("dir" values) for a given fixture + flags
//   - --filter behavior: leaf globs, middle-directory globs, subtree dirs
//   - --execution-order-groups values and --depends-on names
//
// Exempt by design (not asserted here):
//   - atlantis_* locals overrides (the CLI cannot evaluate HCL locals)
//   - atlantis_terraform_version locals (same reason); the .terraform-version
//     file applies to both engines and so is NOT exempted
//   - stack specifics (library: custom parsing; cli: native terragrunt)

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"

	"github.com/ghodss/yaml"
)

func runEngineContract(t *testing.T, engine string, args []string) AtlantisConfig {
	t.Helper()
	if engine == engineCLI {
		terragruntCLIOrSkip(t)
	}
	if err := resetForRun(); err != nil {
		t.Fatal(err)
	}

	filename := filepath.Join("test_artifacts", fmt.Sprintf("contract-%s-%d.yaml", engine, os.Getpid()))
	defer os.Remove(filename)

	content, err := RunWithFlags(filename, append(append([]string{"generate"}, args...), "--engine", engine, "--output", filename))
	if err != nil {
		t.Fatalf("[%s] generate failed: %v", engine, err)
	}

	cfg := &AtlantisConfig{}
	if err := yaml.Unmarshal(content, cfg); err != nil {
		t.Fatalf("[%s] output not yaml: %v", engine, err)
	}
	return *cfg
}

func projectDirs(cfg AtlantisConfig) []string {
	dirs := []string{}
	for _, p := range cfg.Projects {
		dirs = append(dirs, p.Dir)
	}
	sort.Strings(dirs)
	return uniqueStrings(dirs)
}

func groupsByDir(cfg AtlantisConfig) map[string]int {
	out := map[string]int{}
	for _, p := range cfg.Projects {
		if p.ExecutionOrderGroup != nil {
			out[p.Dir] = *p.ExecutionOrderGroup
		}
	}
	return out
}

func dependsByDir(cfg AtlantisConfig) map[string][]string {
	out := map[string][]string{}
	for _, p := range cfg.Projects {
		if p.DependsOn != nil {
			cp := append([]string{}, p.DependsOn...)
			sort.Strings(cp)
			out[p.Dir] = cp
		}
	}
	return out
}

type contractCase struct {
	name string
	root string
	args []string
	// rootUnderIssues points into test_examples_issues rather than
	// test_examples (the fixture mirrors a production topology, which we
	// don't want appearing inside high-level whole-tree goldens)
	rootUnderIssues bool
}

var engineContractCases = []contractCase{
	{name: "leaf modules, dependency-chained", root: "chained_dependencies"},
	// pre-regression the CLI engine answered this with an empty set:
	{name: "filter glob addresses a whole directory subtree", root: "chained_dependencies", args: []string{"--filter", "../test_examples/chained_dependencies/depender*"}},
	{name: "filter singles a leaf dir exactly", root: "chained_dependencies", args: []string{"--filter", "../test_examples/chained_dependencies/depender"}},
	{name: "execution order groups + depends-on match", root: "chained_dependencies", args: []string{"--execution-order-groups", "--depends-on", "--create-project-name"}},
	{name: "include-heavy real-world-shaped tree", root: "terragrunt-infrastructure-live-example"},
	// the production shape from the incident: a directory mid-tree selected by glob
	{name: "mid-tree dir glob filter", root: "terragrunt-infrastructure-live-example", args: []string{"--filter", "../test_examples/terragrunt-infrastructure-live-example/non-prod/us-east-1"}},
	{name: "mid-tree dir glob wildcard at top", root: "terragrunt-infrastructure-live-example", args: []string{"--filter", "../test_examples/terragrunt-infrastructure-live-example/*/us-east-1"}},
	// stack file + pre-generated unit dirs on disk + catalog unit dir on
	// disk must collapse into exactly ONE project (the stack), on both engines
	{name: "stack keeps generated units invisible", root: "stack_generated_units", args: []string{"--enable-stacks"}, rootUnderIssues: true},
}

func TestEngineContractIdenticalProjectSets(t *testing.T) {
	for _, tc := range engineContractCases {
		t.Run(tc.name, func(t *testing.T) {
			base := "../test_examples/"
			if tc.rootUnderIssues {
				base = "../test_examples_issues/"
			}
			rootFlag := []string{"--root", base + tc.root}
			args := append(rootFlag, tc.args...)

			libCfg := runEngineContract(t, engineLibrary, args)
			cliCfg := runEngineContract(t, engineCLI, args)

			if dirsLib, dirsCli := projectDirs(libCfg), projectDirs(cliCfg); !reflect.DeepEqual(dirsLib, dirsCli) {
				t.Fatalf("project dir sets diverge:\nlibrary: %v\ncli:     %v", dirsLib, dirsCli)
			}
			if gLib, gCli := groupsByDir(libCfg), groupsByDir(cliCfg); !reflect.DeepEqual(gLib, gCli) {
				t.Fatalf("execution_order_groups diverge:\nlibrary: %v\ncli:     %v", gLib, gCli)
			}
			if dLib, dCli := dependsByDir(libCfg), dependsByDir(cliCfg); !reflect.DeepEqual(dLib, dCli) {
				t.Fatalf("depends_on diverge:\nlibrary: %v\ncli:     %v", dLib, dCli)
			}
			t.Logf("engines agree on %d projects for %q", len(projectDirs(libCfg)), tc.name)
		})
	}
}
