package cmd

// Stack represents a logical grouping of Terragrunt modules
type Stack struct {
	// Unique name for the stack
	Name string

	// Optional description
	Description string

	// List of module paths belonging to this stack
	Modules []string

	// Stack dependencies (other stack names)
	Dependencies []string

	// Atlantis configuration for this stack
	AtlantisConfig StackAtlantisConfig

	// Execution order for this stack
	ExecutionOrder int

	// Source of stack definition (for debugging)
	Source string
}

// StackAtlantisConfig contains Atlantis-specific configuration for a stack
type StackAtlantisConfig struct {
	Workflow          string
	AutoPlan          bool
	Parallel          bool
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
	Parallel            bool     `yaml:"parallel" json:"parallel"`
	ApplyRequirements   []string `yaml:"apply_requirements,omitempty" json:"apply_requirements,omitempty"`
	ExecutionOrderGroup int      `yaml:"execution_order_group,omitempty" json:"execution_order_group,omitempty"`
	Workspace           string   `yaml:"workspace,omitempty" json:"workspace,omitempty"`
	TerraformVersion    string   `yaml:"terraform_version,omitempty" json:"terraform_version,omitempty"`
}

// StackManagerConfig configures the stack manager
type StackManagerConfig struct {
	GitRoot           string
	DefinitionFile    string
	InferFromDir      bool
	DirectoryDepth    int
	AllowMultiStack   bool
	StackMarkerFile   string
	ValidateCoverage  bool
}

// StackManager manages stack discovery and project generation
type StackManager struct {
	config            StackManagerConfig
	stacks            []Stack
	moduleToStacks    map[string][]string
	stackToModules    map[string][]string
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
func (sm *StackManager) DiscoverStacks() ([]Stack, error) {
	var discoveredStacks []Stack

	// Source 1: External definition file
	if sm.config.DefinitionFile != "" {
		stacks, err := sm.loadStackDefinitionFile()
		if err != nil {
			return nil, err
		}
		discoveredStacks = append(discoveredStacks, stacks...)
	}

	// Source 2: Directory inference
	if sm.config.InferFromDir {
		stacks, err := sm.inferStacksFromDirectory()
		if err != nil {
			return nil, err
		}
		discoveredStacks = append(discoveredStacks, stacks...)
	}

	// TODO: Source 3: HCL stack blocks
	// TODO: Source 4: Module-level tags

	sm.stacks = discoveredStacks
	return discoveredStacks, nil
}

// AssignModulesToStacks assigns terragrunt modules to stacks
func (sm *StackManager) AssignModulesToStacks(modules []string) (map[string][]string, error) {
	assignments := make(map[string][]string)

	for _, stack := range sm.stacks {
		for _, module := range modules {
			if sm.moduleMatchesStack(module, stack) {
				assignments[stack.Name] = append(assignments[stack.Name], module)
				sm.moduleToStacks[module] = append(sm.moduleToStacks[module], stack.Name)
			}
		}
	}

	sm.stackToModules = assignments
	return assignments, nil
}

// GenerateStackProject generates an Atlantis project for a stack
func (sm *StackManager) GenerateStackProject(stack Stack) (*AtlantisProject, error) {
	// Aggregate all dependencies from modules in the stack
	allDependencies := []string{
		"*.hcl",
		"*.tf*",
		"**/*.hcl",
		"**/*.tf*",
	}

	// Add stack-level dependencies
	for _, depStack := range stack.Dependencies {
		if modules, ok := sm.stackToModules[depStack]; ok {
			for _, module := range modules {
				// Convert to relative path from stack root
				allDependencies = append(allDependencies, module)
			}
		}
	}

	// Determine the directory for the stack project
	// This would typically be the common parent of all modules
	stackDir := sm.findCommonParent(stack.Modules)

	project := &AtlantisProject{
		Dir:              stackDir,
		Name:             stack.Name,
		Workflow:         stack.AtlantisConfig.Workflow,
		Workspace:        stack.AtlantisConfig.Workspace,
		TerraformVersion: stack.AtlantisConfig.TerraformVersion,
		Autoplan: AutoplanConfig{
			Enabled:      stack.AtlantisConfig.AutoPlan,
			WhenModified: uniqueStrings(allDependencies),
		},
	}

	if len(stack.AtlantisConfig.ApplyRequirements) > 0 {
		project.ApplyRequirements = &stack.AtlantisConfig.ApplyRequirements
	}

	if stack.ExecutionOrder > 0 {
		project.ExecutionOrderGroup = &stack.ExecutionOrder
	}

	// Generate depends_on if there are stack dependencies
	if len(stack.Dependencies) > 0 {
		project.DependsOn = stack.Dependencies
	}

	return project, nil
}

// Helper methods (stubs for now - would be implemented fully)

func (sm *StackManager) loadStackDefinitionFile() ([]Stack, error) {
	// TODO: Implement YAML/JSON parsing
	return []Stack{}, nil
}

func (sm *StackManager) inferStacksFromDirectory() ([]Stack, error) {
	// TODO: Implement directory-based inference
	return []Stack{}, nil
}

func (sm *StackManager) moduleMatchesStack(module string, stack Stack) bool {
	// TODO: Implement glob pattern matching for include/exclude
	return false
}

func (sm *StackManager) findCommonParent(modules []string) string {
	// TODO: Implement common parent directory finding
	return "."
}

// GetStackForModule returns the stack(s) a module belongs to
func (sm *StackManager) GetStackForModule(module string) []string {
	return sm.moduleToStacks[module]
}

// ValidateStackCoverage ensures all modules are assigned to at least one stack
func (sm *StackManager) ValidateStackCoverage(allModules []string) error {
	// TODO: Implement validation
	return nil
}


