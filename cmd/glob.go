package cmd

import (
	"path"
	"strings"
)

// matchGlob matches slash-separated glob patterns against slash-separated
// paths. Supported syntax: "*" (one segment), "?" and character classes via
// path.Match inside a segment, and "**" (zero or more whole segments).
//
// We do not delegate to doublestar/path/filepath here on purpose: those split
// on the host OS path separator, which made results platform-dependent (a bug
// that surfaced in windows CI).
func matchGlob(pattern, name string) bool {
	segs := strings.Split(pattern, "/")
	parts := strings.Split(name, "/")
	return matchSegments(segs, parts)
}

func matchSegments(pattern []string, name []string) bool {
	if len(pattern) == 0 {
		return len(name) == 0
	}
	head, tail := pattern[0], pattern[1:]

	if head == "**" {
		// `**` consumes zero or more name segments
		if matchSegments(tail, name) {
			return true
		}
		return len(name) > 0 && matchSegments(pattern, name[1:])
	}

	if len(name) == 0 {
		return false
	}
	matched, err := path.Match(head, name[0])
	if err != nil {
		return false
	}
	return matched && matchSegments(tail, name[1:])
}
