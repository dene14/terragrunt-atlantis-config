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
	result, err := ParseStackHclFile(stackFile, ctx, tmpDir)
	require.NoError(t, err)

	assert.NotNil(t, result)
	assert.Equal(t, 2, len(result.Units))
	assert.Equal(t, "vpc", result.Units[0].Name)
	assert.Equal(t, "database", result.Units[1].Name)
	assert.Equal(t, 1, len(result.Stacks))
	assert.Equal(t, "production", result.Stacks[0].Name)
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

	def, err := ParseStackHclFile(stackFile, ctx, tmpDir)
	require.NoError(t, err)

	// Convert to stacks
	stacks := ConvertStackHclToStacks([]StackHclDefinition{*def}, tmpDir)

	assert.Equal(t, 1, len(stacks))
	// Stack name is the stack file's directory relative to the git root
	assert.Equal(t, ".", stacks[0].Name)
	assert.Equal(t, "Production stack", stacks[0].Description)
	assert.Equal(t, 1, len(stacks[0].Modules))
	assert.Equal(t, "vpc", stacks[0].Modules[0])
	assert.Equal(t, "terragrunt.stack.hcl", stacks[0].Source)
}

// TestParseStackHclFile_LiteralFallback ensures that stack files that cannot be
// fully evaluated (e.g. functions referencing files that do not exist at
// generation time) still get their statically-evaluable attributes parsed.
func TestParseStackHclFile_LiteralFallback(t *testing.T) {
	tmpDir := t.TempDir()
	stackFile := filepath.Join(tmpDir, "terragrunt.stack.hcl")

	// find_in_parent_folders("does/not/exist") fails at evaluation time, but
	// the unit's path attribute is a plain literal and must survive.
	content := `unit "vpc" {
  source = "${find_in_parent_folders("does/not/exist")}"
  path   = "vpc"
}

unit "db" {
  source = "../units/db"
  path   = "db"
}
`
	require.NoError(t, os.WriteFile(stackFile, []byte(content), 0644))

	terragruntOptions := options.NewTerragruntOptions()
	ctx := config.NewParsingContext(context.Background(), terragruntOptions)

	result, err := ParseStackHclFile(stackFile, ctx, tmpDir)
	require.NoError(t, err)
	require.Equal(t, 2, len(result.Units))
	assert.Equal(t, "vpc", result.Units[0].Name)
	require.NotNil(t, result.Units[0].Path)
	assert.Equal(t, "vpc", *result.Units[0].Path)
	assert.Equal(t, "db", result.Units[1].Name)
	require.NotNil(t, result.Units[1].Source)
	assert.Equal(t, "../units/db", *result.Units[1].Source)
}

// TestConvertStackHclToStacks_UnitSources ensures local unit source directories
// (catalog style) are watched by the stack while remote sources are skipped.
func TestConvertStackHclToStacks_UnitSources(t *testing.T) {
	tmpDir := t.TempDir()

	// Layout: live/prod/terragrunt.stack.hcl sourcing units from ../../units/*
	stackDir := filepath.Join(tmpDir, "live", "prod")
	require.NoError(t, os.MkdirAll(stackDir, 0755))
	require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, "units", "vpc"), 0755))

	content := `unit "vpc" {
  source = "../../units/vpc"
  path   = "vpc"
}

unit "app" {
  source = "git::https://github.com/example/app.git"
  path   = "app"
}
`
	stackFile := filepath.Join(stackDir, "terragrunt.stack.hcl")
	require.NoError(t, os.WriteFile(stackFile, []byte(content), 0644))

	terragruntOptions := options.NewTerragruntOptions()
	ctx := config.NewParsingContext(context.Background(), terragruntOptions)

	def, err := ParseStackHclFile(stackFile, ctx, tmpDir)
	require.NoError(t, err)

	stacks := ConvertStackHclToStacks([]StackHclDefinition{*def}, tmpDir)
	require.Equal(t, 1, len(stacks))

	stack := stacks[0]
	assert.Equal(t, "live/prod", stack.Name)
	assert.Equal(t, "live/prod/terragrunt.stack.hcl", stack.Source)
	// No terragrunt.hcl exists at the unit paths, so no members are recorded
	assert.Empty(t, stack.Modules)
	// The local catalog dir is watched; the remote source is not
	assert.Equal(t, []string{"units/vpc"}, stack.UnitSources)
}

// TestParseStackHclFile_NestedStacks ensures nested stack blocks expose their
// source and path attributes.
func TestParseStackHclFile_NestedStacks(t *testing.T) {
	tmpDir := t.TempDir()

	require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, "stacks", "base"), 0755))
	stackFile := filepath.Join(tmpDir, "terragrunt.stack.hcl")
	content := `stack "base" {
  source = "./stacks/base"
  path   = "base"
}

unit "app" {
  source = "./app"
  path   = "app"
}
`
	require.NoError(t, os.WriteFile(stackFile, []byte(content), 0644))

	terragruntOptions := options.NewTerragruntOptions()
	ctx := config.NewParsingContext(context.Background(), terragruntOptions)

	result, err := ParseStackHclFile(stackFile, ctx, tmpDir)
	require.NoError(t, err)
	require.Equal(t, 1, len(result.Stacks))
	assert.Equal(t, "base", result.Stacks[0].Name)
	require.NotNil(t, result.Stacks[0].Source)
	assert.Equal(t, "./stacks/base", *result.Stacks[0].Source)
	require.NotNil(t, result.Stacks[0].Path)
	assert.Equal(t, "base", *result.Stacks[0].Path)

	stacks := ConvertStackHclToStacks([]StackHclDefinition{*result}, tmpDir)
	require.Equal(t, 1, len(stacks))
	// The nested stack's local source dir is watched by the parent stack
	assert.Contains(t, stacks[0].UnitSources, "stacks/base")
}

// TestFindStackHclFiles_SkipsGeneratedDirs ensures terragrunt-internal and
// VCS directories are not searched for stack files.
func TestFindStackHclFiles_SkipsGeneratedDirs(t *testing.T) {
	tmpDir := t.TempDir()

	dirs := []string{
		filepath.Join(tmpDir, "live", "prod"),
		filepath.Join(tmpDir, "live", "prod", ".terragrunt-stack", "vpc"),
		filepath.Join(tmpDir, ".git"),
		filepath.Join(tmpDir, "units", "vpc", ".terragrunt-cache", "abc123"),
	}
	for _, d := range dirs {
		require.NoError(t, os.MkdirAll(d, 0755))
		require.NoError(t, os.WriteFile(filepath.Join(d, "terragrunt.stack.hcl"), []byte(""), 0644))
	}

	stackFiles, err := FindStackHclFiles(tmpDir)
	require.NoError(t, err)
	assert.Equal(t, []string{filepath.Join(tmpDir, "live", "prod", "terragrunt.stack.hcl")}, stackFiles)
}

// TestParseAnchoredUnitConfig verifies that a unit config is evaluated as if
// located at its generated (anchor) location: includes resolve against the
// anchor's parent directories, dependency blocks anchor at the anchor dir,
// and local terraform module sources are collected. The anchor does not
// exist on disk — exactly like before `terragrunt stack generate`.
func TestParseAnchoredUnitConfig(t *testing.T) {
	root := t.TempDir()

	// Layout:
	//   root/stack/cloud.hcl                      <- include via find_in_parent_folders from anchor
	//   root/stack/prod/                          <- stack dir (units generate here)
	//   root/catalog/peering/terragrunt.hcl       <- the unit source we read
	//   root/modules/routing/main.tf              <- local module source of the unit
	require.NoError(t, os.MkdirAll(filepath.Join(root, "stack", "prod"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "stack", "cloud.hcl"), []byte("locals {}"), 0644))
	catalogDir := filepath.Join(root, "catalog", "peering")
	require.NoError(t, os.MkdirAll(catalogDir, 0755))
	moduleDir := filepath.Join(root, "modules", "routing")
	require.NoError(t, os.MkdirAll(moduleDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(moduleDir, "main.tf"), []byte(""), 0644))

	unitConfig := `include "cloud" {
  path = find_in_parent_folders("cloud.hcl")
}

dependency "vpc" {
  config_path = "../main"
}

dependency "other" {
  config_path = "../../outside/resolver"
}

locals {}

terraform {
  source = "${get_repo_root()}/modules/routing"
}
`
	sourceFile := filepath.Join(catalogDir, "terragrunt.hcl")
	require.NoError(t, os.WriteFile(sourceFile, []byte(unitConfig), 0644))

	anchorFile := filepath.Join(root, "stack", "prod", "peering", "terragrunt.hcl")
	paths := anchoredUnitWatchPaths{}
	parseAnchoredUnitConfig(sourceFile, anchorFile, root, map[string]bool{}, &paths)

	assert.Equal(t, []string{filepath.Join(root, "stack", "cloud.hcl")}, paths.includes)
	assert.Contains(t, paths.deps, filepath.Join(root, "stack", "prod", "main"))
	assert.Contains(t, paths.deps, filepath.Join(root, "stack", "outside", "resolver"))
	assert.Equal(t, []string{moduleDir}, paths.tfDirs)
}

// TestEnrichStackWithUnitDetails checks external dependency classification,
// cascading and catalog-source exclusion inputs.
func TestEnrichStackWithUnitDetails(t *testing.T) {
	root := t.TempDir()

	// Layout:
	//   cloud.hcl                              <- include file, ancestor of the stack dir
	//   stack/prod/terragrunt.stack.hcl        <- the stack (units generate inline)
	//   stack/catalog/worker/terragrunt.hcl    <- unit source (catalog)
	//   shared/terragrunt.hcl                  <- external dependency (regular module)
	require.NoError(t, os.WriteFile(filepath.Join(root, "cloud.hcl"), []byte("locals {}"), 0644))

	stackDir := filepath.Join(root, "stack", "prod")
	require.NoError(t, os.MkdirAll(stackDir, 0755))
	stackFile := filepath.Join(stackDir, "terragrunt.stack.hcl")
	require.NoError(t, os.WriteFile(stackFile, []byte(`unit "worker" {
  source = "../catalog/worker"
  path   = "worker"
}
`), 0644))

	catalogDir := filepath.Join(root, "stack", "catalog", "worker")
	require.NoError(t, os.MkdirAll(catalogDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(catalogDir, "terragrunt.hcl"), []byte(`include "cloud" {
  path = find_in_parent_folders("cloud.hcl")
}

dependency "shared" {
  config_path = "../../../shared"
}
`), 0644))

	require.NoError(t, os.MkdirAll(filepath.Join(root, "shared"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "shared", "terragrunt.hcl"), []byte(`terraform { source = "." }`), 0644))

	terragruntOptions := options.NewTerragruntOptions()
	ctx := config.NewParsingContext(context.Background(), terragruntOptions)
	def, err := ParseStackHclFile(stackFile, ctx, root)
	require.NoError(t, err)

	stacks := ConvertStackHclToStacks([]StackHclDefinition{*def}, root)
	require.Equal(t, 1, len(stacks))
	stack := stacks[0]

	// catalog dir is a watched unit source
	assert.Equal(t, []string{"stack/catalog/worker"}, stack.UnitSources)

	oldCascade := cascadeDependencies
	cascadeDependencies = true
	defer func() { cascadeDependencies = oldCascade }()

	EnrichStackWithUnitDetails(&stack, *def, root)

	// watch paths are slash-normalized internally
	slash := func(p string) string { return filepath.ToSlash(p) }
	assert.Contains(t, stack.ExtraWatchPaths, slash(filepath.Join(root, "cloud.hcl")))
	// external dependency is recorded (and cascaded: shared's *.tf* pattern)
	assert.Contains(t, stack.ExtraWatchPaths, slash(filepath.Join(root, "shared", "terragrunt.hcl")))
	assert.Contains(t, stack.ExtraWatchPaths, slash(filepath.Join(root, "shared", "*.tf*")))
}

// TestStackManager_IsStackSourceDir verifies catalog dirs are excluded from
// regular project generation.
func TestStackManager_IsStackSourceDir(t *testing.T) {
	mgr := NewStackManager(StackManagerConfig{GitRoot: "/repo"})
	mgr.stacks = []Stack{
		{Name: "live/prod", UnitSources: []string{"units/vpc", "units/app"}},
	}

	assert.True(t, mgr.IsStackSourceDir("units/vpc"))
	assert.True(t, mgr.IsStackSourceDir("units/vpc/terragrunt.hcl"))
	assert.True(t, mgr.IsStackSourceDir("units/vpc/submodule"))
	assert.False(t, mgr.IsStackSourceDir("units/db"))
	assert.False(t, mgr.IsStackSourceDir("live/prod"))
}

// TestFindStackHclFiles_UnreadableDir ensures discovery tolerates directories
// it cannot read (e.g. stale root-owned .terragrunt-cache from container runs).
func TestFindStackHclFiles_UnreadableDir(t *testing.T) {
	tmpDir := t.TempDir()

	goodDir := filepath.Join(tmpDir, "live", "prod")
	require.NoError(t, os.MkdirAll(goodDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(goodDir, "terragrunt.stack.hcl"), []byte(""), 0644))

	brokenDir := filepath.Join(tmpDir, "live", ".terragrunt-cache", "abc")
	require.NoError(t, os.MkdirAll(brokenDir, 0700))
	require.NoError(t, os.WriteFile(filepath.Join(brokenDir, "terragrunt.stack.hcl"), []byte(""), 0644))
	require.NoError(t, os.Chmod(filepath.Join(tmpDir, "live", ".terragrunt-cache"), 0000))
	t.Cleanup(func() { os.Chmod(filepath.Join(tmpDir, "live", ".terragrunt-cache"), 0755) })

	stackFiles, err := FindStackHclFiles(tmpDir)
	require.NoError(t, err)
	assert.Equal(t, []string{filepath.Join(goodDir, "terragrunt.stack.hcl")}, stackFiles)
}
