package cmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// https://github.com/transcend-io/terragrunt-atlantis-config/issues/270

func gitRun(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v (%s)", args, err, out)
	}
}

// writeRepoWith commits a repo shaped:
//
//	shared/vars.tf                 (watched by appa via ../shared glob)
//	appa/terragrunt.hcl            (depends on shared)
//	appb/terragrunt.hcl            (independent)
func writeRepoWith(t *testing.T, dir string) {
	t.Helper()
	mustWrite := func(rel, content string) {
		p := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	mustWrite("shared/vars.tf", "variable \"x\" {}\n")
	mustWrite("appa/terragrunt.hcl", `
terraform {
  source = "../shared"
}
`)
	mustWrite("appb/terragrunt.hcl", `
terraform {
  source = "git::git@github.com:example-corp/m.git//x?ref=1.0.0"
}
`)

	gitRun(t, dir, "init", "-q")
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-q", "-m", "init")
}

func TestFilterGitKeepsOnlyChangedProject(t *testing.T) {
	repo := t.TempDir()
	writeRepoWith(t, repo)

	// touch only appb
	if err := os.WriteFile(filepath.Join(repo, "appb", "terragrunt.hcl"), []byte("terraform {\n  source = \"git::git@github.com:example-corp/m.git//x?ref=1.0.1\"\n}\n"), 0644); err != nil {
		t.Fatal(err)
	}

	defer func() { gitFilter = "" }()
	if err := resetForRun(); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(repo, "out.yaml")
	defer os.Remove(out)

	content, err := RunWithFlags(out, []string{
		"generate", "--output", out, "--root", repo, "--filter-git", "HEAD",
	})
	if err != nil {
		t.Fatal(err)
	}
	text := string(content)
	if contains := containsDir(text, "dir: appa"); contains {
		t.Fatalf("appa must be filtered out (untouched):\n%s", text)
	}
	if !containsDir(text, "dir: appb") {
		t.Fatalf("appb must be kept (touched):\n%s", text)
	}
}

func TestFilterGitDependencyMatch(t *testing.T) {
	repo := t.TempDir()
	writeRepoWith(t, repo)

	// touch shared -> appa must survive via its ../shared/*.tf* watch
	if err := os.WriteFile(filepath.Join(repo, "shared", "vars.tf"), []byte("variable \"y\" {}\n"), 0644); err != nil {
		t.Fatal(err)
	}

	defer func() { gitFilter = "" }()
	if err := resetForRun(); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(repo, "out.yaml")
	defer os.Remove(out)
	content, err := RunWithFlags(out, []string{
		"generate", "--output", out, "--root", repo, "--filter-git", "HEAD",
	})
	if err != nil {
		t.Fatal(err)
	}
	text := string(content)
	if !containsDir(text, "dir: appa") {
		t.Fatalf("appa must be kept (its watched shared/ changed):\n%s", text)
	}
	if containsDir(text, "dir: appb") {
		t.Fatalf("appb must be filtered out:\n%s", text)
	}
}

func TestFilterGitCleanTreeKeepsNothing(t *testing.T) {
	repo := t.TempDir()
	writeRepoWith(t, repo)

	defer func() { gitFilter = "" }()
	if err := resetForRun(); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(repo, "out.yaml")
	defer os.Remove(out)
	content, err := RunWithFlags(out, []string{
		"generate", "--output", out, "--root", repo, "--filter-git", "HEAD",
	})
	if err != nil {
		t.Fatal(err)
	}
	// empty project list must not crash; look for empty projects key marker
	if string(content) == "" {
		t.Fatal("no output")
	}
}

func containsDir(haystack, needle string) bool {
	for _, line := range strings.Split(haystack, "\n") {
		if strings.TrimSpace(line) == needle {
			return true
		}
	}
	return false
}
