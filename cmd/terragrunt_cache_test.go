package cmd

import "testing"

// Regression guard for
// https://github.com/transcend-io/terragrunt-atlantis-config/issues/434:
// .terragrunt-cache contains copies of real configs (and generated root.hcl
// files); walking it used to emit bogus projects pointing into the cache.
func TestTerragruntCacheIgnored(t *testing.T) {
	runTest(t, "golden/terragrunt_cache_ignored.yaml", []string{
		"--root", "../test_examples_issues/terragrunt_cache_polution",
	})
}
