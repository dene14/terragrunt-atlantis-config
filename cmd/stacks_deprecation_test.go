package cmd

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	log "github.com/sirupsen/logrus"
)

// --enable-stacks is being deprecated (removal planned for v1.27, alongside
// the library engine); a migration warning must fire when it is set.
func TestStacksDeprecationWarning(t *testing.T) {
	if err := resetForRun(); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	origOut := log.StandardLogger().Out
	log.SetOutput(&buf)
	defer log.SetOutput(origOut)

	_, err := RunWithFlags("test_artifacts/stacks_deprecation.yaml", []string{
		"generate", "--enable-stacks",
		"--output", "test_artifacts/stacks_deprecation.yaml",
		"--root", filepath.Join("..", "test_examples", "basic_module"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "deprecated: --enable-stacks will be removed in v1.27") {
		t.Fatalf("expected --enable-stacks deprecation warning, got:\n%s", buf.String())
	}
}