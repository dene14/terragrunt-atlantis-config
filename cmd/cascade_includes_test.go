package cmd

import "testing"

// Regression lock for
// https://github.com/transcend-io/terragrunt-atlantis-config/issues/265:
// dependency blocks declared in an *included* file must appear in
// when_modified even with --cascade-dependencies=false. The terragrunt
// v0.99.5 library merges included configs before we inspect blocks, which
// resolves the original report; this test pins the behavior.
func TestCascadeFalseKeepsIncludedDependencyBlocks(t *testing.T) {
	runTest(t, "golden/cascade_includes_nocascade.yaml", []string{
		"--cascade-dependencies=false",
		"--root", "../test_examples_issues/cascade_includes",
	})
}
