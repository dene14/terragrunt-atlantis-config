package cmd

import "testing"

// A classic terragrunt module nested anywhere under a stack directory is
// stack-owned: `terragrunt stack run` plans it (it is part of the stack's run
// queue), so it must NOT get its own Atlantis project. Mirrors the
// reusable-stack layout (qa-rise): stack "vpc" generates vpc/main, and a
// hand-written vpc/something coexists as a sibling — both planned by the one
// stack project.
func TestStackNestedClassicModuleFoldedIntoStack(t *testing.T) {
	runTest(t, "golden/stack_nested_classic.yaml", []string{
		"--engine", "library",
		"--enable-stacks",
		"--root", "../test_examples_issues/stack_nested_classic",
	})
}