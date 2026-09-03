package cmd

import (
	"context"
	"fmt"
	"path"
	"path/filepath"
	"strings"

	"github.com/gruntwork-io/terragrunt/config"
	"github.com/gruntwork-io/terragrunt/options"
	log "github.com/sirupsen/logrus"
)

// Stack represents a logical grouping of Terragrunt modules
type Stack struct {
	// Unique name for the stack. For stacks discovered from
	// terragrunt.stack.hcl files, this is the stack file's directory
	// relative to the repo root. For stacks from a definition file,
	// this is the explicitly declared name.
	Name string

	// Optional description
	Description string

	// Directories (relative to gitRoot) containing a terragrunt.hcl that
	// belong to this stack. These do not get individual Atlantis projects.
	Modules []string

	// Directories (relative to gitRoot) referenced by unit/nested-stack
	// `source` attributes. They only feed the stack project's autoplan
	// when_modified patterns and still get normal projects of their own.
	UnitSources []string

	// Glob patterns (relative to gitRoot) used to assign modules to this
	// stack. Only populated for stacks from an external definition file.
	Include []string
	Exclude []string

	// Absolute paths (slash-separated, may include glob suffixes) that should
	// additionally trigger the stack project when modified. Filled in from
	// the units' include chains, external dependency blocks, local terraform
	// module sources and cascaded dependencies of external targets.
	ExtraWatchPaths []string

	// Stack dependencies (other stack names)
	Dependencies []string

	// Atlantis configuration for this stack
	AtlantisConfig StackAtlantisConfig

	// Execution order for this stack
	ExecutionOrder int

	// Path of the terragrunt.stack.hcl file that defined this stack,
	// relative to gitRoot. Empty for stacks from an external definition file.
	Source string
}

// StackAtlantisConfig contains Atlantis-specific configuration for a stack
type StackAtlantisConfig struct {
	Workflow          string
	AutoPlan          bool
	ApplyRequirements []string
	Workspace         string
	TerraformVersion  string
}

// StackDefinitionFile represents the external YAML/JSON stack definition file
type StackDefinitionFile struct {
	Version int                   `yaml:"version" json:"version"`
	Stacks  []ExternalStackConfig `yaml:"stacks" json:"stacks"`
}

// ExternalStackConfig represents a stack defined in external file
type ExternalStackConfig struct {
	Name        string              `yaml:"name" json:"name"`
	Description string              `yaml:"description,omitempty" json:"description,omitempty"`
	Include     []string            `yaml:"include,omitempty" json:"include,omitempty"`
	Exclude     []string            `yaml:"exclude,omitempty" json:"exclude,omitempty"`
	Modules     []string            `yaml:"modules,omitempty" json:"modules,omitempty"`
	DependsOn   []string            `yaml:"depends_on,omitempty" json:"depends_on,omitempty"`
	Atlantis    AtlantisStackConfig `yaml:"atlantis,omitempty" json:"atlantis,omitempty"`
}

// AtlantisStackConfig represents Atlantis configuration in external file
type AtlantisStackConfig struct {
	Workflow            string   `yaml:"workflow,omitempty" json:"workflow,omitempty"`
	AutoPlan            bool     `yaml:"autoplan" json:"autoplan"`
	ApplyRequirements   []string `yaml:"apply_requirements,omitempty" json:"apply_requirements,omitempty"`
	ExecutionOrderGroup int      `yaml:"execution_order_group,omitempty" json:"execution_order_group,omitempty"`
	Workspace           string   `yaml:"workspace,omitempty" json:"workspace,omitempty"`
	TerraformVersion    string   `yaml:"terraform_version,omitempty" json:"terraform_version,omitempty"`
}

// StackManagerConfig configures the stack manager
type StackManagerConfig struct {
	GitRoot                 string
	DefinitionFile          string
	StackWorkflow           string // Default workflow for stack projects
	DefaultWorkflow         string // Fallback workflow if stack workflow not set
	CreateProjectName       bool   // Whether to include project name in generated config
	CreateWorkspace         bool   // Whether to generate a workspace per project
	AutoPlan                bool   // Global autoplan default, used when a stack does not set one
	DefaultTerraformVersion string
}

// StackManager manages stack discovery and project generation
type StackManager struct {
	config         StackManagerConfig
	stacks         []Stack
	moduleToStacks map[string][]string
	stackToModules map[string][]string
}

// NewStackManager creates a new stack manager
func NewStackManager(config StackManagerConfig) *StackManager {
	return &StackManager{
		config:         config,
		stacks:         []Stack{},
		moduleToStacks: make(map[string][]string),
		stackToModules: make(map[string][]string),
	}
}

// DiscoverStacks discovers all stacks from configured sources
// Priority order:
// 1. HCL stack files (terragrunt.stack.hcl) - the native Terragrunt stacks feature
// 2. External definition file (YAML/JSON) - alternative, explicitly configured method
func (sm *StackManager) DiscoverStacks() ([]Stack, error) {
	var discoveredStacks []Stack

	// Source 1: HCL stack files (terragrunt.stack.hcl)
	stacks, err := sm.loadStackHclFiles()
	if err != nil {
		return nil, err
	}
	discoveredStacks = append(discoveredStacks, stacks...)

	// Source 2: External definition file (YAML/JSON)
	if sm.config.DefinitionFile != "" {
		stacks, err := sm.loadStackDefinitionFile()
		if err != nil {
			return nil, err
		}
		discoveredStacks = append(discoveredStacks, stacks...)
	}

	sm.stacks = discoveredStacks
	return discoveredStacks, nil
}

// normalizeModuleDir converts a terragrunt module reference (which may be an
// absolute path, a path to a terragrunt.hcl file, or a directory) into a
// slash-separated directory path relative to gitRoot.
func (sm *StackManager) normalizeModuleDir(module string) string {
	dir := module
	if filepath.IsAbs(dir) {
		if rel, err := filepath.Rel(sm.config.GitRoot, dir); err == nil {
			dir = rel
		}
	}
	dir = filepath.ToSlash(dir)
	base := path.Base(dir)
	if base == "terragrunt.hcl" || base == "terragrunt.hcl.json" {
		dir = path.Dir(dir)
	}
	dir = strings.TrimSuffix(dir, "/")
	return dir
}

// AssignModulesToStacks assigns terragrunt modules to stacks
func (sm *StackManager) AssignModulesToStacks(modules []string) (map[string][]string, error) {
	assignments := make(map[string][]string)

	for _, stack := range sm.stacks {
		for _, module := range modules {
			moduleDir := sm.normalizeModuleDir(module)
			if sm.moduleMatchesStack(moduleDir, stack) {
				assignments[stack.Name] = append(assignments[stack.Name], moduleDir)
				sm.moduleToStacks[moduleDir] = append(sm.moduleToStacks[moduleDir], stack.Name)
			}
		}
	}

	sm.stackToModules = assignments
	return assignments, nil
}

// GenerateStackProject generates an Atlantis project for a stack
func (sm *StackManager) GenerateStackProject(stack Stack) (*AtlantisProject, error) {
	// Determine the directory for the stack project.
	// - HCL-defined stacks: the directory containing terragrunt.stack.hcl.
	//   A workflow for such a project is expected to run Terragrunt stack
	//   commands there (e.g. `terragrunt stack generate` + `stack run`).
	// - Externally defined stacks: the common parent directory of the
	//   stack's member modules (including modules matched via
	//   include/exclude patterns during assignment).
	memberDirs := append([]string{}, stack.Modules...)
	memberDirs = append(memberDirs, sm.stackToModules[stack.Name]...)

	var stackDir string
	if stack.Source != "" {
		stackDir = filepath.ToSlash(filepath.Dir(filepath.FromSlash(stack.Source)))
		if stackDir == "" {
			stackDir = "."
		}
	} else if len(memberDirs) > 0 {
		stackDir = sm.findCommonParent(memberDirs)
	} else {
		stackDir = "."
	}

	// autoplan when_modified patterns. The base patterns cover everything
	// inside the stack directory; directories watched by this stack that
	// live outside of it (e.g. a shared units catalog) are added as
	// relative patterns.
	watchedDirs := append([]string{}, memberDirs...)
	watchedDirs = append(watchedDirs, stack.UnitSources...)
	for _, depName := range stack.Dependencies {
		for _, s := range sm.stacks {
			if s.Name == depName {
				watchedDirs = append(watchedDirs, s.Modules...)
				watchedDirs = append(watchedDirs, sm.stackToModules[s.Name]...)
				watchedDirs = append(watchedDirs, s.UnitSources...)
			}
		}
	}

	cleanGitRoot := filepath.Clean(sm.config.GitRoot)
	absStackDir := filepath.Join(cleanGitRoot, filepath.FromSlash(stackDir))

	relativeDependencies := []string{
		"*.hcl",
		"*.tf*",
		"**/*.hcl",
		"**/*.tf*",
	}
	for _, dir := range watchedDirs {
		absDir := filepath.Join(cleanGitRoot, filepath.FromSlash(dir))
		rel, err := filepath.Rel(absStackDir, absDir)
		if err != nil || rel == "." || rel == "" {
			continue
		}
		relSlash := filepath.ToSlash(rel)
		if !strings.HasPrefix(relSlash, "..") {
			// inside the stack directory, already covered by ** patterns
			continue
		}
		relativeDependencies = append(relativeDependencies,
			relSlash+"/**/*.hcl",
			relSlash+"/**/*.tf*")
	}

	// Additional watch targets collected from the unit configs themselves:
	// include chains, external dependencies (incl. cascaded) and local
	// terraform module sources. Entries are absolute and may carry globs.
	for _, watchPath := range stack.ExtraWatchPaths {
		rel, err := filepath.Rel(absStackDir, filepath.FromSlash(watchPath))
		if err != nil {
			continue
		}
		relSlash := filepath.ToSlash(rel)
		if relSlash == "." || !strings.HasPrefix(relSlash, "..") {
			// inside the stack directory, already covered
			continue
		}
		relativeDependencies = append(relativeDependencies, relSlash)
	}

	// Determine workflow: stack config > stack workflow flag > default workflow flag
	workflow := stack.AtlantisConfig.Workflow
	if workflow == "" && sm.config.StackWorkflow != "" {
		workflow = sm.config.StackWorkflow
	} else if workflow == "" && sm.config.DefaultWorkflow != "" {
		workflow = sm.config.DefaultWorkflow
	}

	// Autoplan: explicit stack setting wins, otherwise follow the global flag
	autoPlanEnabled := stack.AtlantisConfig.AutoPlan || sm.config.AutoPlan

	terraformVersion := stack.AtlantisConfig.TerraformVersion
	if terraformVersion == "" {
		terraformVersion = sm.config.DefaultTerraformVersion
	}

	// Sanitized name used for the project name and workspace (same rules as
	// for regular projects)
	projectName := projectNameRegex.ReplaceAllString(stack.Name, "_")

	project := &AtlantisProject{
		Dir:              stackDir,
		Workflow:         workflow,
		TerraformVersion: terraformVersion,
		Autoplan: AutoplanConfig{
			Enabled:      autoPlanEnabled,
			WhenModified: uniqueStrings(relativeDependencies),
		},
	}

	// Only set Name if createProjectName flag is enabled (consistent with regular projects)
	if sm.config.CreateProjectName {
		project.Name = projectName
	}

	workspace := stack.AtlantisConfig.Workspace
	if workspace == "" && sm.config.CreateWorkspace {
		workspace = projectName
	}
	project.Workspace = workspace

	if len(stack.AtlantisConfig.ApplyRequirements) > 0 {
		project.ApplyRequirements = &stack.AtlantisConfig.ApplyRequirements
	}

	if stack.ExecutionOrder > 0 {
		project.ExecutionOrderGroup = &stack.ExecutionOrder
	}

	// depends_on references project names, which only exist when project
	// names are generated
	if len(stack.Dependencies) > 0 && sm.config.CreateProjectName {
		dependsOn := make([]string, 0, len(stack.Dependencies))
		for _, dep := range stack.Dependencies {
			dependsOn = append(dependsOn, projectNameRegex.ReplaceAllString(dep, "_"))
		}
		project.DependsOn = dependsOn
	}

	return project, nil
}

// Helper methods

// loadStackHclFiles discovers and parses terragrunt.stack.hcl files
func (sm *StackManager) loadStackHclFiles() ([]Stack, error) {
	// Find all terragrunt.stack.hcl files
	stackFiles, err := FindStackHclFiles(sm.config.GitRoot)
	if err != nil {
		return nil, fmt.Errorf("failed to find stack HCL files: %w", err)
	}

	if len(stackFiles) == 0 {
		// No stack files found, that's okay
		return []Stack{}, nil
	}

	// Create parsing context - use empty options for now
	terragruntOptions := options.NewTerragruntOptions()
	ctx := config.NewParsingContext(context.Background(), terragruntOptions)

	// Parse each stack file
	stackDefinitions := []StackHclDefinition{}
	for _, stackFile := range stackFiles {
		def, err := ParseStackHclFile(stackFile, ctx, sm.config.GitRoot)
		if err != nil {
			log.Warnf("Failed to parse stack HCL file %s: %v", stackFile, err)
			continue
		}
		stackDefinitions = append(stackDefinitions, *def)
	}

	// Convert to internal Stack structs and enrich with unit-level detail
	// (include chains, external dependencies, terraform module sources)
	stacks := ConvertStackHclToStacks(stackDefinitions, sm.config.GitRoot)
	for i := range stacks {
		EnrichStackWithUnitDetails(&stacks[i], stackDefinitions[i], sm.config.GitRoot)
	}
	log.Infof("Discovered %d stack(s) from %d terragrunt.stack.hcl file(s)", len(stacks), len(stackFiles))

	return stacks, nil
}

func (sm *StackManager) loadStackDefinitionFile() ([]Stack, error) {
	// Parse the stack definition file (YAML/JSON)
	stackDef, err := ParseStackDefinitionFile(sm.config.DefinitionFile)
	if err != nil {
		return nil, err
	}

	// Convert external stack configs to internal Stack structs
	stacks := ConvertExternalStacksToStacks(stackDef.Stacks, sm.config.GitRoot)
	return stacks, nil
}

func (sm *StackManager) moduleMatchesStack(moduleDir string, stack Stack) bool {
	// Check explicit member list. A member directory matches itself and
	// anything nested underneath it.
	for _, member := range stack.Modules {
		member = sm.normalizeModuleDir(member)
		if moduleDir == member || strings.HasPrefix(moduleDir, member+"/") {
			return true
		}
	}

	// Check include/exclude patterns (external definition files)
	if len(stack.Include) == 0 && len(stack.Exclude) == 0 {
		return false
	}

	included := len(stack.Include) == 0
	for _, pattern := range stack.Include {
		if matchGlobPattern(moduleDir, filepath.ToSlash(pattern)) {
			included = true
			break
		}
	}
	if !included {
		return false
	}

	for _, pattern := range stack.Exclude {
		if matchGlobPattern(moduleDir, filepath.ToSlash(pattern)) {
			return false
		}
	}

	return true
}

// findCommonParent returns the common parent directory (relative to gitRoot)
// of the given module paths. Returns "." when there is no common subdirectory.
func (sm *StackManager) findCommonParent(modules []string) string {
	if len(modules) == 0 {
		return "."
	}

	common := strings.Split(sm.normalizeModuleDir(modules[0]), "/")
	for _, module := range modules[1:] {
		parts := strings.Split(sm.normalizeModuleDir(module), "/")
		i := 0
		for i < len(common) && i < len(parts) && common[i] == parts[i] {
			i++
		}
		common = common[:i]
		if len(common) == 0 {
			return "."
		}
	}

	if len(common) == 0 || common[0] == "." {
		return "."
	}
	return strings.Join(common, "/")
}

// GetStackForModule returns the name(s) of the stack(s) a module belongs to.
// The module may be given as a terragrunt.hcl file path or a directory,
// absolute or relative to gitRoot.
func (sm *StackManager) GetStackForModule(module string) []string {
	return sm.moduleToStacks[sm.normalizeModuleDir(module)]
}

// IsStackSourceDir reports whether the module dir is a directory used as a
// local `source` by some stack's units (a catalog/template directory). Such
// directories are watched by the stack project but do not get individual
// Atlantis projects. Only consulted with --enable-stacks.
func (sm *StackManager) IsStackSourceDir(module string) bool {
	dir := sm.normalizeModuleDir(module)
	for _, stack := range sm.stacks {
		for _, source := range stack.UnitSources {
			if dir == source || strings.HasPrefix(dir, source+"/") {
				return true
			}
		}
	}
	return false
}
