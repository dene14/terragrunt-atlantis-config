package cmd

import (
	"os"
	"path/filepath"
)

// Small filesystem helpers.
//
// Historically these came from github.com/gruntwork-io/terragrunt/util, but
// that package moved behind terragrunt's internal/ boundary. The equivalents
// below are tiny wrappers around the standard library, kept deliberately
// boring: no caching, no logging, no surprises.

// joinPath joins path segments and cleans the result.
func joinPath(elem ...string) string {
	return filepath.Join(elem...)
}

// fileExists returns true when path exists and is a regular file.
func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

// isDir returns true when path exists and is a directory.
func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// readFileAsString returns the full contents of path as a string.
func readFileAsString(path string) (string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}
