package cmd

import (
	"testing"
)

// https://github.com/transcend-io/terragrunt-atlantis-config/issues/191

func TestFindProjectCycleDetectsCycle(t *testing.T) {
	projects := []AtlantisProject{
		{Dir: "a", Autoplan: AutoplanConfig{WhenModified: []string{"../b/terragrunt.hcl"}}},
		{Dir: "b", Autoplan: AutoplanConfig{WhenModified: []string{"../c/terragrunt.hcl"}}},
		{Dir: "c", Autoplan: AutoplanConfig{WhenModified: []string{"../a/terragrunt.hcl"}}},
	}

	cycle := findProjectCycle(projects)
	if len(cycle) == 0 {
		t.Fatal("expected a cycle to be detected")
	}
	if cycle[0] != cycle[len(cycle)-1] {
		t.Fatalf("cycle must return to its start, got %v", cycle)
	}
}

func TestFindProjectCycleAcyclicIsQuiet(t *testing.T) {
	projects := []AtlantisProject{
		{Dir: "a", Autoplan: AutoplanConfig{WhenModified: []string{"../b/terragrunt.hcl"}}},
		{Dir: "b", Autoplan: AutoplanConfig{WhenModified: []string{"../c/terragrunt.hcl"}}},
		{Dir: "c"},
	}
	if cycle := findProjectCycle(projects); cycle != nil {
		t.Fatalf("false positive: %v", cycle)
	}
}

func TestFindProjectCycleSelfLoop(t *testing.T) {
	projects := []AtlantisProject{
		{Dir: "a", Autoplan: AutoplanConfig{WhenModified: []string{"terragrunt.hcl"}}},
	}
	if cycle := findProjectCycle(projects); cycle != nil {
		t.Fatalf("self-directory watches are not cycles: %v", cycle)
	}
}
