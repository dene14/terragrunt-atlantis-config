package cmd

import "testing"

// Complex-layout integration coverage on the CLI engine: a repo tree with
// chained modules, shared parents, an azureprod-only branch and a stack with
// integer materialized units. Validates the whole-directory containment of
// stacks, the filter/exclude interplay, and ordering on top.
func TestCLIComplexLayoutDefault(t *testing.T) {
	runTest(t, "golden/complex_default.yaml", []string{"--engine", "cli", "--root", "../test_examples/complex_layout"})
}

func TestCLIComplexLayoutFilterEnv(t *testing.T) {
	runTest(t, "golden/complex_filter_env.yaml", []string{
		"--engine", "cli", "--root", "../test_examples/complex_layout",
		"--filter", "providers/aws/*/us-east-1",
	})
}

func TestCLIComplexLayoutOrdering(t *testing.T) {
	runTest(t, "golden/complex_ordering.yaml", []string{
		"--engine", "cli", "--root", "../test_examples/complex_layout",
		"--execution-order-groups", "--depends-on", "--create-project-name",
	})
}

func TestCLIComplexLayoutExcludeNested(t *testing.T) {
	runTest(t, "golden/complex_exclude.yaml", []string{
		"--engine", "cli", "--root", "../test_examples/complex_layout",
		"--exclude", "live/**/observability*",
	})
}

func TestCLIComplexLayoutStackWorkflow(t *testing.T) {
	runTest(t, "golden/complex_stackworkflow.yaml", []string{
		"--engine", "cli", "--root", "../test_examples/complex_layout",
		"--stack-workflow", "terragrunt-stack",
	})
}

// Flag surface on the same tree: name/workspace generation, ordering,
// apply_requirements propagation, autoplan+automerge blocks.
func TestCLIComplexLayoutFlagsCompose(t *testing.T) {
	runTest(t, "golden/complex_flags.yaml", []string{
		"--engine", "cli", "--root", "../test_examples/complex_layout",
		"--autoplan", "--automerge", "--create-workspace", "--create-project-name",
		"--apply-requirements", "approved",
	})
}
