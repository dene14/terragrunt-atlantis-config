package cmd

import (
	"strings"
	"testing"
)

// Regression test for https://github.com/transcend-io/terragrunt-atlantis-config/issues/365:
// a module referencing the same dependency through several when_modified
// entries (dependency block + extra_arguments var files) must not list that
// dependency multiple times in depends_on.
func TestDependsOnDeduplicated(t *testing.T) {
	runTest(t, "golden/depends_on_duplicate.yaml", []string{
		"--depends-on",
		"--create-project-name",
		"--root", "../test_examples_issues/depends_on_duplicate",
	})
}

// Sanity: the same fix keeps execution_order_group values coherent on the
// chained example (each group still increments once per level).
func TestDependsOnDeduplicatedOrderGroupsIntact(t *testing.T) {
	if err := resetForRun(); err != nil {
		t.Fatal(err)
	}

	filename := "test_artifacts/dupgroups.yaml"
	content, err := RunWithFlags(filename, []string{
		"generate",
		"--output", filename,
		"--execution-order-groups",
		"--depends-on",
		"--create-project-name",
		"--root", "../test_examples_issues/depends_on_duplicate",
	})
	if err != nil {
		t.Fatal(err)
	}

	text := string(content)
	if strings.Count(text, "  - dep\n") > 1 {
		t.Fatalf("dependency listed more than once in depends_on:\n%s", text)
	}
	if !strings.Contains(text, "execution_order_group: 1") {
		t.Fatalf("expected app to be one group above dep:\n%s", text)
	}
}
