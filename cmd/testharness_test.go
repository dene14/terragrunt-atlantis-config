package cmd

import (
	"os"
	"path/filepath"
	"testing"
)

// Minimal test harness for the post-library suite. resetForRun used to live in
// generate_test.go (deleted with that file); this is the fresh equivalent for
// the remaining collision-unaware globals.
func resetForRun() error {
	pwd, err := os.Getwd()
	if err != nil {
		return err
	}

	gitRoot = pwd
	autoPlan = false
	autoMerge = false
	cascadeDependencies = true
	ignoreDependencyBlocks = false
	parallel = true
	createWorkspace = false
	createProjectName = false
	preserveWorkflows = true
	preserveProjects = false
	defaultWorkflow = ""
	filterPaths = []string{}
	excludePaths = nil
	outputPath = ""
	defaultTerraformVersion = ""
	defaultTerraformDistribution = ""
	dependsOn = false
	executionOrderGroups = false
	engine = engineAuto
	defaultApplyRequirements = []string{}
	deleteSourceBranchOnMerge = false
	gitFilter = ""
	stackWorkflow = ""
	return nil
}

// runTest runs generate, compares output with a golden file. Every remaining
// golden test targets the CLI engine; without a terragrunt v1+ binary on PATH
// these skip (engine=auto resolves to cli).
func runTest(t *testing.T, goldenFile string, args []string) {
	terragruntCLIOrSkip(t)

	if err := resetForRun(); err != nil {
		t.Errorf("Failed to reset default flags: %v", err)
		return
	}

	// Ensure test_artifacts directory exists
	if err := os.MkdirAll("test_artifacts", 0755); err != nil {
		t.Errorf("Failed to create test_artifacts directory: %v", err)
		return
	}

	artifact := "test_artifacts/" + filepath.Base(goldenFile) + ".run.yaml"
	current, err := RunWithFlags(artifact, append([]string{"generate", "--engine", "cli", "--output", artifact}, args...))
	if err != nil {
		t.Error(err)
		return
	}

	goldenBytes, err := os.ReadFile(goldenFile)
	if err != nil {
		t.Errorf("Failed to read golden file: %v", err)
		return
	}

	if string(current) != string(goldenBytes) {
		t.Errorf("Output did not match golden\n\nExpected:\n%s\n\nGot:\n%s", goldenBytes, current)
	}
}
