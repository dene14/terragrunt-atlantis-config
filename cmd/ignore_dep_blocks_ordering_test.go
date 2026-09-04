package cmd

import "testing"

// Regression for
// https://github.com/transcend-io/terragrunt-atlantis-config/issues/426:
// --ignore-dependency-blocks strips dependency paths from autoplan watches
// (intended), which previously also zeroed execution_order_group and emptied
// depends_on (bug). Ordering must respect the edges regardless.
func TestIgnoreDependencyBlocksKeepsOrdering(t *testing.T) {
	runTest(t, "golden/ignore_dep_blocks_ordering.yaml", []string{
		"--ignore-dependency-blocks",
		"--execution-order-groups",
		"--depends-on",
		"--create-project-name",
		"--root", "../test_examples/chained_dependencies",
	})
}
