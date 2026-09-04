package cmd

import "testing"

// Feature: https://github.com/transcend-io/terragrunt-atlantis-config/issues/386
// atlantis_terraform_distribution locals override; flag default; tofu-friendly.

// Locals win without any flag involvement.
func TestTerraformDistributionLocal(t *testing.T) {
	runTest(t, "golden/terraform_distribution_local.yaml", []string{
		"--root", "../test_examples_issues/terraform_distribution",
	})
}

// The flag sets a default for every project.
func TestTerraformDistributionFlag(t *testing.T) {
	// package-level flag state must not leak into other suite tests
	defer func() { defaultTerraformDistribution = "" }()

	runTest(t, "golden/terraform_distribution_flag.yaml", []string{
		"--root", "../test_examples_issues/terraform_distribution",
		"--terraform-distribution", "tofu",
	})
}

// TODO(coverage): CLI engine honors the flag natively (no locals evaluation
// by design); covered indirectly by engine goldens with terraform_version.
