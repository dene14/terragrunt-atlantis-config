package cmd

import (
	"os"
	"path/filepath"
	"strings"

	log "github.com/sirupsen/logrus"
)

// tfenvVersionFile is the conventional tfenv/pipelines file holding a single
// terraform/tofu version, e.g. "1.9.8".
const tfenvVersionFile = ".terraform-version"

// findTfenvVersion walks up from dir looking for tfenvVersionFile, stopping
// at (and including) gitRoot. Returns the trimmed version string.
func findTfenvVersion(dir, root string) string {
	// Callers reach us with a mix of absolute and relative paths depending
	// on the code path; compare in absolute space or the boundary check
	// silently never matches.
	for _, p := range []*string{&dir, &root} {
		if abs, err := filepath.Abs(*p); err == nil {
			*p = abs
		}
	}
	dir = filepath.Clean(dir)
	root = filepath.Clean(root)

	for {
		candidate := filepath.Join(dir, tfenvVersionFile)
		raw, err := os.ReadFile(candidate)
		if err == nil {
			v := strings.TrimSpace(string(raw))
			if v != "" {
				return v
			}
		}

		if dir == root || !strings.HasPrefix(dir, root+string(filepath.Separator)) {
			return ""
		}
		dir = filepath.Dir(dir)
	}
}

// resolveTerraformVersion picks the terraform version for a project using the
// documented precedence:
//
//	atlantis_terraform_version local  >  .terraform-version file  >  --terraform-version flag
func resolveTerraformVersion(localValue, projectDirAbs string) string {
	if localValue != "" {
		return localValue
	}
	if gitRoot != "" && projectDirAbs != "" {
		if v := findTfenvVersion(projectDirAbs, gitRoot); v != "" {
			log.Debugf("project in %s pinned to terraform %s via %s", projectDirAbs, v, tfenvVersionFile)
			return v
		}
	}
	return defaultTerraformVersion
}
