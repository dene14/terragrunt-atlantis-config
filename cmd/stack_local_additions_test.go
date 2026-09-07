package cmd

import "testing"

// A stack's own materialized unit dirs must never become standalone
// projects, while user-authored additions living inside the stack directory
// (NOT named by the stack file) must keep their own project.
//
//	stack file declares unit "vpc" { path = "main" } and "peering" { ... }
//	disk: live/prod/main  (generated content -> suppressed)
//	      live/prod/local-addons (hand-written -> kept)
//	      units/vpc (catalog source -> suppressed)
//
// Regression for "generated-in-stack units become projects"; sharpens it so
// genuine local additions still emit projects.
func TestStackLocalAdditionsKept(t *testing.T) {
	runTest(t, "golden/stack_local_additions.yaml", []string{
		"--engine", "library",
		"--enable-stacks",
		"--root", "../test_examples_issues/stack_generated_units",
	})
}
