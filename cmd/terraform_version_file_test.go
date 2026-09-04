package cmd

import (
	"os"
	"path/filepath"
	"testing"
)

// Feature: https://github.com/transcend-io/terragrunt-atlantis-config/issues/282

func TestFindTfenvVersion(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "env", "app"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, tfenvVersionFile), []byte("1.9.8\n"), 0644); err != nil {
		t.Fatal(err)
	}

	if got := findTfenvVersion(filepath.Join(root, "env", "app"), root+"/"); got != "1.9.8" {
		t.Fatalf("expected walk-up lookup to find 1.9.8, got %q", got)
	}
	if err := os.WriteFile(filepath.Join(root, "env", tfenvVersionFile), []byte("1.10.0"), 0644); err != nil {
		t.Fatal(err)
	}
	if got := findTfenvVersion(filepath.Join(root, "env", "app"), root+"/"); got != "1.10.0" {
		t.Fatalf("expected nearest file to win (1.10.0), got %q", got)
	}
}

func TestFindTfenvVersionStopsAtRoot(t *testing.T) {
	root := t.TempDir()
	// version file exists outside the boundary; search must not escape root
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, tfenvVersionFile), []byte("9.9.9"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "app"), 0755); err != nil {
		t.Fatal(err)
	}
	if got := findTfenvVersion(filepath.Join(root, "app"), root+"/"); got != "" {
		t.Fatalf("must not escape the root boundary, got %q", got)
	}
}

func TestResolveTerraformVersionPrecedence(t *testing.T) {
	if err := resetForRun(); err != nil {
		t.Fatal(err)
	}
	prevFlag := defaultTerraformVersion
	defer func() { defaultTerraformVersion = prevFlag }()
	defaultTerraformVersion = "from-flag"

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, tfenvVersionFile), []byte("from-file"), 0644); err != nil {
		t.Fatal(err)
	}

	if got := resolveTerraformVersion("", root); got != "from-file" {
		t.Fatalf("file must beat flag, got %q", got)
	}
	if got := resolveTerraformVersion("from-local", root); got != "from-local" {
		t.Fatalf("local must beat file, got %q", got)
	}
	if got := resolveTerraformVersion("", t.TempDir()); got != "from-flag" {
		t.Fatalf("flag is the fallback, got %q", got)
	}
}

// Golden: file pins apply in the library engine, local override still wins.
func TestTerraformVersionFileGolden(t *testing.T) {
	runTest(t, "golden/terraform_version_file.yaml", []string{
		"--root", "../test_examples_issues/terraform_version_file",
	})
}

// Same in the CLI engine (skipped without a terragrunt v1 binary).
func TestTerraformVersionFileCLIEngine(t *testing.T) {
	terragruntCLIOrSkip(t)
	runTest(t, "golden/terraform_version_file_cli.yaml", []string{
		"--engine", "cli",
		"--root", "../test_examples_issues/terraform_version_file",
	})
}
