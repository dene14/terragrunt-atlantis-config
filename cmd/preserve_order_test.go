package cmd

import (
	"strings"
	"testing"
)

func TestExtractTopLevelKeySection(t *testing.T) {
	doc := `version: 3
projects:
- dir: x
workflows:
  zzz: {}
  aaa: {}
other_key: 1
`
	got := extractTopLevelKeySection([]byte(doc), "workflows")
	if !strings.Contains(got, "zzz:") || !strings.Contains(got, "aaa:") {
		t.Fatalf("section missing entries: %q", got)
	}
	if strings.Contains(got, "other_key") || strings.Contains(got, "projects") {
		t.Fatalf("section leaked neighbors: %q", got)
	}
	if !strings.HasPrefix(got, "workflows:") {
		t.Fatalf("section must start at the key: %q", got)
	}
}

func TestExtractTopLevelKeySectionMissing(t *testing.T) {
	if got := extractTopLevelKeySection([]byte("version: 3\n"), "workflows"); got != "" {
		t.Fatalf("expected empty, got %q", got)
	}
}

func TestExtractTopLevelKeySectionAtEOF(t *testing.T) {
	doc := "version: 3\nworkflows:\n  w:\n    plan:\n      steps: []\n"
	got := extractTopLevelKeySection([]byte(doc), "workflows")
	if !strings.Contains(got, "steps") {
		t.Fatalf("truncated trailing section: %q", got)
	}
}
