package cmd

import (
	"strings"
)

// Preserved workflows (and other user-owned top-level keys such as
// allowed_regexp_prefixes or checkout_strategy) should survive regeneration
// exactly as written. Marshaling them through Go values would alphabetize
// keys, drop comments, and reorder sections, turning every regenerated
// atlantis.yaml into diff noise. Instead this file carries top-level
// sections over as raw text.

// managedTopLevelKeys are the keys this tool (re)computes on every run.
// Everything else present in the previous output file is user-owned and is
// preserved verbatim.
var managedTopLevelKeys = map[string]bool{
	"version":        true,
	"automerge":      true,
	"parallel_plan":  true,
	"parallel_apply": true,
	// projects and workflows have their own preservation mechanics
	// (--preserve-projects / --preserve-workflows) and are not covered by
	// the generic verbatim pass.
	"projects":  true,
	"workflows": true,
}

// extractTopLevelKeySection returns the raw text of a top-level YAML mapping
// key (e.g. "workflows") — indentation, comments and key order included — or
// "" when the key is absent. The returned text always ends with a newline.
func extractTopLevelKeySection(yamlText []byte, key string) string {
	// Line endings carry no meaning for preservation: normalize CRLF (files
	// written on Windows) up front, otherwise a trailing \r hides the key
	// header, a bare "\r" line would look like the next top-level key, and
	// the final CRLF conversion pass would produce "\r\r\n".
	lines := strings.Split(strings.ReplaceAll(string(yamlText), "\r\n", "\n"), "\n")

	header := key + ":"
	start := -1
	for i, line := range lines {
		trimmed := strings.TrimRight(line, " \t")
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
		if line == "" || strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t") || strings.HasPrefix(line, "-") {
			// "- …" lines are top-level sequence items belonging to the key
			// whose section we are collecting (YAML allows unindented list
			// items), not a new top-level key.
			continue
		}
		// first non-indented non-sequence line: either the next top-level
		// key or garbage — either way our section ends here
		end = i
		break
	}

	section := strings.Join(lines[start:end], "\n")
	return strings.TrimRight(section, " \t\n") + "\n"
}

// listTopLevelKeys returns the top-level YAML keys of a document in
// declaration order.
func listTopLevelKeys(yamlText []byte) []string {
	lines := strings.Split(strings.ReplaceAll(string(yamlText), "\r\n", "\n"), "\n")
	keys := []string{}
	for _, line := range lines {
		if line == "" || strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t") || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "-") || line == "---" {
			continue
		}
		idx := strings.Index(line, ":")
		if idx < 0 {
			continue
		}
		keys = append(keys, strings.TrimSpace(line[:idx]))
	}
	return keys
}

// preservedUserSections extracts every top-level section of the previous
// output file that this tool does not manage, verbatim and in original order.
// Sections explicitly overridden (e.g. by a flag) are skipped.
func preservedUserSections(previous []byte, overridden ...string) []string {
	if len(previous) == 0 {
		return nil
	}

	skip := map[string]bool{}
	for _, k := range overridden {
		skip[k] = true
	}

	sections := []string{}
	for _, key := range listTopLevelKeys(previous) {
		if managedTopLevelKeys[key] || skip[key] {
			continue
		}
		if section := extractTopLevelKeySection(previous, key); section != "" {
			sections = append(sections, section)
		}
	}
	return sections
}
