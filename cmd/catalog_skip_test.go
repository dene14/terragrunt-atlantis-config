package cmd

import "testing"

// Catalog units referenced by stacks are resolvable only after
// `terragrunt stack generate`; evaluated standalone they fail. The
// library engine must skip them with a warning instead of killing the
// whole generation for the repo. (Mirror of production setup where a
// 380-module repo died because one unit needs stack runtime context.)
func TestCatalogUnitSkipsGently(t *testing.T) {
	runTest(t, "golden/catalog_unit_skip.yaml", []string{
		"--root", "../test_examples_issues/stack_catalog_units",
	})
}
