package cmd

import "testing"

func TestMatchGlob(t *testing.T) {
	cases := []struct {
		pattern, name string
		want          bool
	}{
		{"terra*m", "terraform", true},
		{"terra*r", "terraform", false},
		{"terraform/*/qa", "terraform/aws/qa", true},
		{"terraform/*/qa", "terraform/aws/qa/sub", false}, // Match is whole-path
		{"terraform/*/qa", "terraform/aws/prod", false},
		{"**", "anything/at/all", true},
		{"terraform/**", "terraform", true}, // ** eats zero segments
		{"terraform/**/qa", "terraform/qa", true},
		{"terraform/**/qa", "terraform/a/b/c/qa", true},
		{"a/*/c", "a/b/c", true},
		{"a/*/c", "a/b/x/c", false},
		{"a/?", "a/b", true},
		{"a/?", "a/bb", false},
	}
	for _, c := range cases {
		if got := matchGlob(c.pattern, c.name); got != c.want {
			t.Errorf("matchGlob(%q, %q) = %v, want %v", c.pattern, c.name, got, c.want)
		}
	}
}
