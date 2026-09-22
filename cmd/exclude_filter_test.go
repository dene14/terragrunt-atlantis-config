package cmd

import "testing"

// https://github.com/transcend-io/terragrunt-atlantis-config/issues/248
// --exclude subtracts directories (and subtrees) from the discovered set.
func TestExcludeSubtractsProjects(t *testing.T) {
	terragruntCLIOrSkip(t)
	defer func() { excludePaths = nil }() // not covered by resetForRun
	runTest(t, "golden/exclude_dep.yaml", []string{
		"--exclude", "dep",
		"--root", "../test_examples_issues/depends_on_duplicate",
	})
}

// Exclude composes with --filter: keep both dep & app by filter, drop app.
func TestExcludeComposesWithFilter(t *testing.T) {
	terragruntCLIOrSkip(t)
	defer func() { excludePaths = nil }()
	if err := resetForRun(); err != nil {
		t.Fatal(err)
	}
	filename := "test_artifacts/exclude_compose.yaml"
	content, err := RunWithFlags(filename, []string{
		"generate",
		"--output", filename,
		"--filter", "dep*",
		"--exclude", "app",
		"--root", "../test_examples_issues/depends_on_duplicate",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := string(content); len(got) == 0 {
		t.Fatal("no output")
	}
	// dep must survive, app must not — both engines can only do this if they
	// apply scope after project discovery, which is the shared pipeline.
	if !containsDir(string(content), "dir: dep") || containsDir(string(content), "dir: app") {
		t.Fatalf("exclude+filter composition wrong:\n%s", content)
	}
}
