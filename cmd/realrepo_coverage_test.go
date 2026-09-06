package cmd

import "testing"

// Flags/shapes observed on a real 385-project repository.

// mock_outputs are used at scale to mock dependency outputs during plan.
// The wiring of when_modified triggers must include the dependency's config
// even with mocks declared.
func TestMockOutputsDependencies(t *testing.T) {
	runTest(t, "golden/mock_outputs.yaml", []string{
		"--root", "../test_examples_issues/mock_outputs",
	})
}

// Multiple dependency blocks can share a label (terragrunt warns, uses the
// last); the generator must still build the whole project list and keep both
// watch entries.
func TestDuplicateDependencyLabelsTolerated(t *testing.T) {
	runTest(t, "golden/duplicate_dependency_labels.yaml", []string{
		"--root", "../test_examples_issues/duplicate_dependency_labels",
	})
}

// The exact pre-workflow-hook invocation shape used on production Atlantis.
func TestProductionHookShape(t *testing.T) {
	runTest(t, "golden/hook_shape.yaml", []string{
		"--engine", "library",
		"--autoplan", "--parallel",
		"--workflow", "terragrunt",
		"--enable-stacks", "--stack-workflow", "terragrunt-stack",
		"--execution-order-groups",
		"--root", "../test_examples/stacks_hcl_example",
	})
}
