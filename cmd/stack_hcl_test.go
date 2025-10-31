package cmd

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/gruntwork-io/terragrunt/config"
	"github.com/gruntwork-io/terragrunt/options"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseStackHclFile(t *testing.T) {
	tmpDir := t.TempDir()
	stackFile := filepath.Join(tmpDir, "terragrunt.stack.hcl")

	content := `unit "vpc" {
  path = "vpc"
}

unit "database" {
  path = "database"
}

stack "production" {
  description = "Production environment"
}
`

	err := os.WriteFile(stackFile, []byte(content), 0644)
	require.NoError(t, err)

	// Create necessary directories
	os.MkdirAll(filepath.Join(tmpDir, "vpc"), 0755)
	os.MkdirAll(filepath.Join(tmpDir, "database"), 0755)
	os.WriteFile(filepath.Join(tmpDir, "vpc", "terragrunt.hcl"), []byte("terraform { source = \".\" }"), 0644)
	os.WriteFile(filepath.Join(tmpDir, "database", "terragrunt.hcl"), []byte("terraform { source = \".\" }"), 0644)

	// Create parsing context
	terragruntOptions := options.NewTerragruntOptions()
	ctx := config.NewParsingContext(context.Background(), terragruntOptions)

	// Parse the stack file
	result, err := ParseStackHclFile(stackFile, ctx)
	require.NoError(t, err)

	assert.NotNil(t, result)
	assert.Equal(t, 2, len(result.Units))
	assert.Equal(t, "vpc", result.Units[0].Name)
	assert.Equal(t, "database", result.Units[1].Name)
	assert.NotNil(t, result.StackBlock)
	assert.Equal(t, "production", result.StackBlock.Name)
}

func TestFindStackHclFiles(t *testing.T) {
	tmpDir := t.TempDir()

	// Create nested directory structure with stack files
	os.MkdirAll(filepath.Join(tmpDir, "env", "prod"), 0755)
	os.MkdirAll(filepath.Join(tmpDir, "env", "staging"), 0755)

	os.WriteFile(filepath.Join(tmpDir, "env", "prod", "terragrunt.stack.hcl"), []byte(`unit "app" { path = "app" }`), 0644)
	os.WriteFile(filepath.Join(tmpDir, "env", "staging", "terragrunt.stack.hcl"), []byte(`unit "app" { path = "app" }`), 0644)

	stackFiles, err := FindStackHclFiles(tmpDir)
	require.NoError(t, err)

	assert.Equal(t, 2, len(stackFiles))
}

func TestConvertStackHclToStacks(t *testing.T) {
	tmpDir := t.TempDir()
	stackFile := filepath.Join(tmpDir, "terragrunt.stack.hcl")

	content := `unit "vpc" {
  path = "vpc"
}

stack "production" {
  description = "Production stack"
}
`

	err := os.WriteFile(stackFile, []byte(content), 0644)
	require.NoError(t, err)

	// Create unit directories with terragrunt.hcl files
	vpcDir := filepath.Join(tmpDir, "vpc")
	os.MkdirAll(vpcDir, 0755)
	os.WriteFile(filepath.Join(vpcDir, "terragrunt.hcl"), []byte("terraform { source = \".\" }"), 0644)

	// Parse the stack file
	terragruntOptions := options.NewTerragruntOptions()
	ctx := config.NewParsingContext(context.Background(), terragruntOptions)

	def, err := ParseStackHclFile(stackFile, ctx)
	require.NoError(t, err)

	// Convert to stacks
	stacks := ConvertStackHclToStacks([]StackHclDefinition{*def}, tmpDir)

	assert.Equal(t, 1, len(stacks))
	assert.Equal(t, "production", stacks[0].Name)
	assert.Equal(t, "Production stack", stacks[0].Description)
	assert.Equal(t, 1, len(stacks[0].Modules))
	assert.Equal(t, "terragrunt.stack.hcl", stacks[0].Source)
}
