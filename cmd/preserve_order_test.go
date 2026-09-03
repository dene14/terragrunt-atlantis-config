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

func TestExtractTopLevelKeySectionCRLF(t *testing.T) {
	// Files written by the generator itself on Windows are CRLF-normalized;
	// they must round-trip identically.
	doc := "version: 3\r\nworkflows:\r\n  zzz: {}\r\n  aaa: {}\r\nother: 1\r\n"
	got := extractTopLevelKeySection([]byte(doc), "workflows")
	if !strings.Contains(got, "zzz:") || !strings.Contains(got, "aaa:") {
		t.Fatalf("CRLF section missing entries: %q", got)
	}
	if strings.Contains(got, "other:") {
		t.Fatalf("CRLF section leaked neighbors: %q", got)
	}
}

func TestExtractTopLevelKeySectionCRLFBlankLines(t *testing.T) {
	// A CRLF file whose workflows section contains an empty line: the empty
	// line is a bare "\r" after splitting on "\n" and previously truncated
	// the section or produced lone-\r artifacts on re-marshal.
	doc := "version: 3\r\nprojects: []\r\nworkflows:\r\n  zzz: {}\r\n\r\n  aaa: {}\r\n"
	got := extractTopLevelKeySection([]byte(doc), "workflows")
	if !strings.Contains(got, "zzz:") || !strings.Contains(got, "aaa:") {
		t.Fatalf("section truncated at CRLF blank line: %q", got)
	}
	if strings.ContainsRune(got, '\r') {
		t.Fatalf("section must be LF-normalized: %q", got)
	}
}
