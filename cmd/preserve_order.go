package cmd

import (
	"strings"
)

// Preserved workflows should survive regeneration exactly as the user wrote
// them. Marshaling them through a map would alphabetize keys and drop
// comments, which turns every regenerated atlantis.yaml into diff noise
// against the previous one. Instead this file carries top-level sections
// over as raw text.

// extractTopLevelKeySection returns the raw text of a top-level YAML mapping
// key (e.g. "workflows") — indentation, comments and key order included — or
// "" when the key is absent. The returned text always ends with a newline.
func extractTopLevelKeySection(yamlText []byte, key string) string {
	lines := strings.Split(string(yamlText), "\n")

	header := key + ":"
	start := -1
	for i, line := range lines {
		// The previous output file may be CRLF-normalized (Windows checkouts
		// and our own --output on Windows), so trailing \r must not hide keys.
		trimmed := strings.TrimRight(line, " \t\r")
		if trimmed == header {
			start = i
			break
		}
		// Tolerate "workflows: {}"-style inline content by still treating the
		// key as present at the top level.
		if strings.HasPrefix(trimmed, header+" ") && !strings.HasPrefix(line, " ") && !strings.HasPrefix(line, "\t") {
			start = i
			break
		}
	}
	if start == -1 {
		return ""
	}

	end := len(lines)
	for i := start + 1; i < len(lines); i++ {
		line := lines[i]
		if line == "" || strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t") {
			continue
		}
		// first non-indented line: either the next top-level key or garbage —
		// either way our section ends here
		end = i
		break
	}

	section := strings.Join(lines[start:end], "\n")
	return strings.TrimRight(section, " \t\r\n") + "\n"
}
