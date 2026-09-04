package cmd

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/bmatcuk/doublestar"
)

// --filter-git limits generation to projects whose autoplan triggers were
// touched by the diff between a git ref and HEAD (upstream issue
// transcend-io/terragrunt-atlantis-config#270). Effectively the same
// decision Atlantis' autoplan would make, but computed before writing
// atlantis.yaml instead of on the Atlantis server.

// gitChangedFiles returns paths changed between ref and HEAD. Paths come
// back anchored at root (not necessarily the git repository root: git always
// reports against the repo root, so results are re-anchored).
func gitChangedFiles(root, ref string) ([]string, error) {
	// Where are we inside the repository? ("" when root is the repo root)
	repoRootOut, err := exec.Command("git", "-C", root, "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return nil, fmt.Errorf("git rev-parse failed: %w", err)
	}
	repoRoot := strings.TrimSpace(string(repoRootOut))
	// Windows temp dirs arrive in 8.3 short form (RUNNER~1) while git answers
	// with the canonical long path; Rel'ing across those never matches.
	if resolved, err := filepath.EvalSymlinks(repoRoot); err == nil {
		repoRoot = resolved
	}
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		root = resolved
	}
	relInside, err := filepath.Rel(repoRoot, root)
	if err != nil {
		return nil, err
	}
	relInside = filepath.ToSlash(relInside)

	argsList := [][]string{
		{"diff", "--name-only", ref + "...HEAD"}, // committed delta on the branch
		{"diff", "--name-only", ref},             // worktree delta (uncommitted local work)
	}
	// Committed delta (merge-base form), then worktree delta: a pre-workflow
	// hook runs on a clean checkout (first form enough), while humans running
	// locally often haven't committed yet (second form). Union of both.
	var lastErr error
	seen := map[string]bool{}
	files := []string{}
	for _, args := range argsList {
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		out, err := cmd.Output()
		if err != nil {
			lastErr = err
			continue
		}

		for _, line := range strings.Split(string(out), "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			rela, err := filepath.Rel(relInside, filepath.ToSlash(line))
			if err != nil || strings.HasPrefix(rela, "..") || seen[rela] {
				continue // outside our root or already counted
			}
			seen[rela] = true
			files = append(files, filepath.ToSlash(rela))
		}
	}
	if lastErr != nil && len(files) == 0 {
		return nil, fmt.Errorf("git diff failed for ref %q: %w", ref, lastErr)
	}
	return files, nil
}

// projectTouchedBy reports whether any changed file lands inside a project's
// autoplan watch: the file is at dir + one of its when_modified globs.
func projectTouchedBy(project AtlantisProject, changed []string, root string) bool {
	base := project.Dir
	for _, file := range changed {
		for _, watch := range project.Autoplan.WhenModified {
			// Patterns are dir-relative; normalize "../foo" segments by
			// cleaning the join.
			pattern := filepath.ToSlash(filepath.Clean(filepath.Join(base, watch)))
			matched, err := doublestar.Match(pattern, file)
			if err == nil && matched {
				return true
			}
		}
	}
	return false
}

// filterProjectsByGitDiff keeps only projects touched by the diff. Note this
// intentionally differs from --filter-matching-nothing semantics: a clean
// diff producing zero projects is a valid outcome here.
func filterProjectsByGitDiff(projects []AtlantisProject, root, ref string) ([]AtlantisProject, error) {
	changed, err := gitChangedFiles(root, ref)
	if err != nil {
		return nil, err
	}

	kept := make([]AtlantisProject, 0, len(projects))
	for _, p := range projects {
		if projectTouchedBy(p, changed, root) {
			kept = append(kept, p)
		}
	}
	return kept, nil
}
