package cmd

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Regression test for https://github.com/transcend-io/terragrunt-atlantis-config/issues/420:
// without --output, the generated atlantis config must go to STDOUT so it can
// be piped to other tools; only diagnostics belong to STDERR.
func TestStdoutOutputWhenNoOutputFlag(t *testing.T) {
	if err := resetForRun(); err != nil {
		t.Fatal(err)
	}

	// RunWithFlags needs a real file to read back even without --output
	stub := filepath.Join("test_artifacts", t.Name()+".yaml")
	defer os.Remove(stub)
	if err := os.WriteFile(stub, []byte{}, 0644); err != nil {
		t.Fatal(err)
	}

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w

	_, runErr := RunWithFlags(stub, []string{"generate", "--root", filepath.Join("..", "test_examples", "basic_module")})

	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	os.Stdout = oldStdout
	captured, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}

	if runErr != nil {
		t.Fatalf("generate failed: %v", runErr)
	}

	out := string(captured)
	if !strings.Contains(out, "version: 3") {
		t.Fatalf("generated config not found on stdout:\n%s", out)
	}
	if strings.Contains(out, "level=info") {
		t.Fatalf("log lines leaked onto stdout:\n%s", out)
	}

	// sanity: stdout, not stderr, received the payload — so the empty stub
	// proves generate didn't write anything either
	stat, err := os.Stat(stub)
	if err != nil {
		t.Fatal(err)
	}
	if stat.Size() != 0 {
		t.Fatalf("without --output, the stub file must stay empty, got %d bytes", stat.Size())
	}
}

// With --output set, the config goes to the file and NOT to stdout; logs keep
// going to stderr. Companion case to TestStdoutOutputWhenNoOutputFlag.
func TestOutputFlagWritesFileNotStdout(t *testing.T) {
	if err := resetForRun(); err != nil {
		t.Fatal(err)
	}

	target := filepath.Join("test_artifacts", t.Name()+".yaml")
	defer os.Remove(target)

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w

	_, runErr := RunWithFlags(target, []string{
		"generate", "--output", target, "--root", filepath.Join("..", "test_examples", "basic_module"),
	})

	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	os.Stdout = oldStdout
	captured, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	if runErr != nil {
		t.Fatalf("generate failed: %v", runErr)
	}

	fileContent, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("reading output file: %v", err)
	}
	if !strings.Contains(string(fileContent), "version: 3") {
		t.Fatalf("--output file is missing the config:\n%s", fileContent)
	}
	if strings.Contains(string(captured), "version: 3") {
		t.Fatalf("config leaked onto stdout despite --output being set:\n%s", captured)
	}
}
