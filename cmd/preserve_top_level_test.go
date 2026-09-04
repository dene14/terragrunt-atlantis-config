package cmd

import (
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Covers:
//
//	https://github.com/transcend-io/terragrunt-atlantis-config/issues/339 (allowed_regexp_prefixes)
//	https://github.com/transcend-io/terragrunt-atlantis-config/issues/417 (delete_source_branch_on_merge)
//
// User-owned top-level keys must survive regeneration verbatim.
func TestUserOwnedTopLevelKeysPreserved(t *testing.T) {
	if err := resetForRun(); err != nil {
		t.Fatal(err)
	}
	deleteSourceBranchOnMerge = false
	defer func() { deleteSourceBranchOnMerge = false }()

	filename := filepath.Join("test_artifacts", fmt.Sprintf("%d.yaml", rand.Int()))
	defer os.Remove(filename)

	old := `allowed_regexp_prefixes:
- ^stage
- ^prod/\w+$
delete_source_branch_on_merge: true
checkout_strategy: merge
`
	if err := os.WriteFile(filename, []byte(old), 0644); err != nil {
		t.Fatal(err)
	}

	content, err := RunWithFlags(filename, []string{
		"generate", "--output", filename, "--root", filepath.Join("..", "test_examples", "basic_module"),
	})
	if err != nil {
		t.Fatal(err)
	}

	norm := strings.ReplaceAll(string(content), "\r\n", "\n")
	for _, want := range []string{
		"allowed_regexp_prefixes:",
		"- ^stage",
		"- ^prod/\\w+$",
		"delete_source_branch_on_merge: true",
		"checkout_strategy: merge",
	} {
		if !strings.Contains(norm, want) {
			t.Fatalf("regenerated config lost %q:\n%s", want, norm)
		}
	}
}

// An explicit flag must win over a previously preserved value.
func TestDeleteSourceBranchOnMergeFlagOverridesPreserved(t *testing.T) {
	if err := resetForRun(); err != nil {
		t.Fatal(err)
	}
	deleteSourceBranchOnMerge = true
	defer func() { deleteSourceBranchOnMerge = false }()

	filename := filepath.Join("test_artifacts", fmt.Sprintf("%d.yaml", rand.Int()))
	defer os.Remove(filename)

	if err := os.WriteFile(filename, []byte("delete_source_branch_on_merge: false\n"), 0644); err != nil {
		t.Fatal(err)
	}

	content, err := RunWithFlags(filename, []string{
		"generate", "--output", filename, "--delete-source-branch-on-merge",
		"--root", filepath.Join("..", "test_examples", "basic_module"),
	})
	if err != nil {
		t.Fatal(err)
	}

	norm := strings.ReplaceAll(string(content), "\r\n", "\n")
	if strings.Count(norm, "delete_source_branch_on_merge") != 1 {
		t.Fatalf("key must appear exactly once (flag wins, no duplicate):\n%s", norm)
	}
	if !strings.Contains(norm, "delete_source_branch_on_merge: true") {
		t.Fatalf("flag value must override preserved value:\n%s", norm)
	}
}
