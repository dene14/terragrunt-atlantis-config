package cmd

import (
	"strings"
	"testing"
)

// Regression test for
// https://github.com/transcend-io/terragrunt-atlantis-config/issues/435:
// a --filter that matches nothing must fail loudly instead of emitting an
// empty atlantis.yaml.
func TestFilterMatchingNothingErrors(t *testing.T) {
	if err := resetForRun(); err != nil {
		t.Fatal(err)
	}

	_, err := RunWithFlags("/dev/null", []string{
		"generate",
		"--root", "../test_examples/basic_module",
		"--filter", "../nonexistent-dir-*",
	})
	if err == nil {
		t.Fatal("expected an error for a filter matching no directories")
	}
	if !strings.Contains(err.Error(), "matched no directories") {
		t.Fatalf("unexpected error text: %v", err)
	}
}

// A filter that DOES match must keep working through the same code path.
func TestFilterMatchingStillWorks(t *testing.T) {
	if err := resetForRun(); err != nil {
		t.Fatal(err)
	}

	if _, err := RunWithFlags("test_artifacts/filterOK.yaml", []string{
		"generate",
		"--root", "../test_examples/chained_dependencies",
		"--filter", "../test_examples/chained_dependencies/depender",
		"--output", "test_artifacts/filterOK.yaml",
	}); err != nil {
		t.Fatalf("valid filter must not error: %v", err)
	}
}

// CLI engine variant of the same guarantee (skipped without terragrunt v1).
func TestCLIFilterMatchingNothingErrors(t *testing.T) {
	terragruntCLIOrSkip(t)
	if err := resetForRun(); err != nil {
		t.Fatal(err)
	}

	_, err := RunWithFlags("/dev/null", []string{
		"generate",
		"--engine", "cli",
		"--root", "../test_examples/basic_module",
		"--filter", "nothing-here-*",
	})
	if err == nil {
		t.Fatal("expected an error for a filter matching no components")
	}
	if !strings.Contains(err.Error(), "matched no discovered components") {
		t.Fatalf("unexpected error text: %v", err)
	}
}
