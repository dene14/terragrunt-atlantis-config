package cmd

import "testing"

// A stack owns its whole directory subtree: anything under a
// terragrunt.stack.hcl dir — including user-authored local additions that are
// NOT named by the stack file — is planned by `terragrunt stack run` and must
// not become its own Atlantis project.
//
//	stack file declares unit "vpc" { path = "main" } and "peering" { ... }
//	disk: live/prod/main          (generated content -> suppressed)
//	      live/prod/local-addons  (local addition -> ALSO suppressed now)
//	      units/vpc               (catalog source -> suppressed)
func TestStackLocalAdditionsFoldedIntoStack(t *testing.T) {
	runTest(t, "golden/stack_local_additions.yaml", []string{
		"--engine", "library",
		"--enable-stacks",
		"--root", "../test_examples_issues/stack_generated_units",
	})
}