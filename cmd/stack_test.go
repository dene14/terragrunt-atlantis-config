package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseStackDefinitionFile(t *testing.T) {
	tests := []struct {
		name        string
		content     string
		expectError bool
		numStacks   int
	}{
		{
			name: "valid basic stack definition",
			content: `version: 1
stacks:
  - name: test-stack
    modules:
      - app-a
      - app-b
    atlantis:
      workflow: default
      autoplan: true`,
			expectError: false,
			numStacks:   1,
		},
		{
			name: "multiple stacks with dependencies",
			content: `version: 1
stacks:
  - name: stack-a
    modules: [app-a]
    atlantis:
      workflow: default
      autoplan: true
  - name: stack-b
    modules: [app-b]
    depends_on: [stack-a]
    atlantis:
      workflow: default
      autoplan: true`,
			expectError: false,
			numStacks:   2,
		},
		{
			name: "stack with glob patterns",
			content: `version: 1
stacks:
  - name: production
    include:
      - "environments/production/**"
    exclude:
      - "environments/production/experimental/**"
    atlantis:
      workflow: production
      autoplan: false`,
			expectError: false,
			numStacks:   1,
		},
		{
			name: "invalid - no stacks",
			content: `version: 1
stacks: []`,
			expectError: true,
		},
		{
			name: "invalid - no name",
			content: `version: 1
stacks:
  - modules: [app-a]
    atlantis:
      workflow: default`,
			expectError: true,
		},
		{
			name: "invalid - no modules or include",
			content: `version: 1
stacks:
  - name: test-stack
    atlantis:
      workflow: default`,
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Write test file
			tmpfile := t.TempDir() + "/test-stack.yaml"
			err := writeFile(tmpfile, []byte(tt.content))
			require.NoError(t, err)

			// Parse file
			result, err := ParseStackDefinitionFile(tmpfile)

			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, result)
				assert.Equal(t, tt.numStacks, len(result.Stacks))
			}
		})
	}
}

func TestValidateStackDefinition(t *testing.T) {
	tests := []struct {
		name        string
		def         *StackDefinitionFile
		expectError bool
		errorMsg    string
	}{
		{
			name: "valid definition",
			def: &StackDefinitionFile{
				Version: 1,
				Stacks: []ExternalStackConfig{
					{
						Name:    "test-stack",
						Modules: []string{"app-a"},
					},
				},
			},
			expectError: false,
		},
		{
			name: "unsupported version",
			def: &StackDefinitionFile{
				Version: 2,
				Stacks: []ExternalStackConfig{
					{Name: "test", Modules: []string{"app-a"}},
				},
			},
			expectError: true,
			errorMsg:    "unsupported stack definition version",
		},
		{
			name: "duplicate stack names",
			def: &StackDefinitionFile{
				Version: 1,
				Stacks: []ExternalStackConfig{
					{Name: "test", Modules: []string{"app-a"}},
					{Name: "test", Modules: []string{"app-b"}},
				},
			},
			expectError: true,
			errorMsg:    "duplicate stack name",
		},
		{
			name: "no modules or include",
			def: &StackDefinitionFile{
				Version: 1,
				Stacks: []ExternalStackConfig{
					{Name: "test"},
				},
			},
			expectError: true,
			errorMsg:    "must specify either 'include' patterns or 'modules' list",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateStackDefinition(tt.def)

			if tt.expectError {
				assert.Error(t, err)
				if tt.errorMsg != "" {
					assert.Contains(t, err.Error(), tt.errorMsg)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestMatchGlobPattern(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		pattern  string
		expected bool
	}{
		{
			name:     "exact match",
			path:     "app/main.tf",
			pattern:  "app/main.tf",
			expected: true,
		},
		{
			name:     "wildcard match",
			path:     "app/main.tf",
			pattern:  "app/*.tf",
			expected: true,
		},
		{
			name:     "recursive match with **",
			path:     "environments/production/networking/vpc",
			pattern:  "environments/production/**",
			expected: true,
		},
		{
			name:     "recursive match with ** in middle",
			path:     "environments/production/region/us-east-1/vpc",
			pattern:  "environments/**/vpc",
			expected: true,
		},
		{
			name:     "no match - different prefix",
			path:     "staging/app/main.tf",
			pattern:  "production/**",
			expected: false,
		},
		{
			name:     "no match - different suffix",
			path:     "environments/production/app",
			pattern:  "environments/production/**/vpc",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := matchGlobPattern(tt.path, tt.pattern)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestMatchModuleToStacks(t *testing.T) {
	stacks := []ExternalStackConfig{
		{
			Name:    "production",
			Include: []string{"environments/production/**"},
		},
		{
			Name:    "staging",
			Include: []string{"environments/staging/**"},
		},
		{
			Name:    "shared",
			Modules: []string{"shared/vpc", "shared/dns"},
		},
	}

	tests := []struct {
		name           string
		modulePath     string
		expectedStacks []string
	}{
		{
			name:           "production module",
			modulePath:     "/repo/environments/production/app",
			expectedStacks: []string{"production"},
		},
		{
			name:           "staging module",
			modulePath:     "/repo/environments/staging/app",
			expectedStacks: []string{"staging"},
		},
		{
			name:           "shared module - explicit",
			modulePath:     "/repo/shared/vpc",
			expectedStacks: []string{"shared"},
		},
		{
			name:           "no match",
			modulePath:     "/repo/experimental/app",
			expectedStacks: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := MatchModuleToStacks(tt.modulePath, stacks, "/repo")
			assert.ElementsMatch(t, tt.expectedStacks, result)
		})
	}
}

func TestStackManager_DiscoverStacks(t *testing.T) {
	// Create temporary stack definition file
	tmpDir := t.TempDir()
	stackFile := tmpDir + "/atlantis-stacks.yaml"
	content := `version: 1
stacks:
  - name: test-stack
    modules:
      - app-a
      - app-b
    atlantis:
      workflow: default
      autoplan: true`

	err := writeFile(stackFile, []byte(content))
	require.NoError(t, err)

	// Create stack manager
	mgr := NewStackManager(StackManagerConfig{
		GitRoot:        tmpDir,
		DefinitionFile: stackFile,
	})

	// Discover stacks
	stacks, err := mgr.DiscoverStacks()
	require.NoError(t, err)
	assert.Len(t, stacks, 1)
	assert.Equal(t, "test-stack", stacks[0].Name)
}

func TestStackManager_GenerateStackProject(t *testing.T) {
	t.Run("with CreateProjectName enabled", func(t *testing.T) {
		mgr := NewStackManager(StackManagerConfig{
			GitRoot:           "/repo",
			CreateProjectName: true,
		})

		stack := Stack{
			Name:         "test-stack",
			Description:  "Test stack",
			Modules:      []string{"app-a", "app-b"},
			Dependencies: []string{"dependency-stack"},
			AtlantisConfig: StackAtlantisConfig{
				Workflow:          "test-workflow",
				AutoPlan:          true,
				ApplyRequirements: []string{"approved"},
				Workspace:         "test-workspace",
			},
			ExecutionOrder: 10,
		}

		project, err := mgr.GenerateStackProject(stack)
		require.NoError(t, err)
		assert.NotNil(t, project)
		assert.Equal(t, "test-stack", project.Name)
		assert.Equal(t, "test-workflow", project.Workflow)
		assert.Equal(t, "test-workspace", project.Workspace)
		assert.True(t, project.Autoplan.Enabled)
		assert.Equal(t, 10, *project.ExecutionOrderGroup)
		assert.Equal(t, []string{"dependency-stack"}, project.DependsOn)
	})

	t.Run("with CreateProjectName disabled", func(t *testing.T) {
		mgr := NewStackManager(StackManagerConfig{
			GitRoot:           "/repo",
			CreateProjectName: false,
		})

		stack := Stack{
			Name:         "test-stack",
			Description:  "Test stack",
			Modules:      []string{"app-a", "app-b"},
			Dependencies: []string{"dependency-stack"},
			AtlantisConfig: StackAtlantisConfig{
				Workflow:          "test-workflow",
				AutoPlan:          true,
				ApplyRequirements: []string{"approved"},
				Workspace:         "test-workspace",
			},
			ExecutionOrder: 10,
		}

		project, err := mgr.GenerateStackProject(stack)
		require.NoError(t, err)
		assert.NotNil(t, project)
		assert.Empty(t, project.Name, "Name should be empty when CreateProjectName is false")
		assert.Equal(t, "test-workflow", project.Workflow)
		assert.Equal(t, "test-workspace", project.Workspace)
		assert.True(t, project.Autoplan.Enabled)
		assert.Equal(t, 10, *project.ExecutionOrderGroup)
		// depends_on references project names, so without --create-project-name
		// there is nothing valid to reference
		assert.Empty(t, project.DependsOn)
	})
}

func TestStackManager_GenerateStackProject_GlobalFlags(t *testing.T) {
	mgr := NewStackManager(StackManagerConfig{
		GitRoot:                 "/repo",
		CreateProjectName:       true,
		CreateWorkspace:         true,
		AutoPlan:                true,
		DefaultTerraformVersion: "1.9.0",
		StackWorkflow:           "stack-wf",
	})

	t.Run("global flags are applied to HCL stacks", func(t *testing.T) {
		stack := Stack{
			Name:   "live/prod",
			Source: "live/prod/terragrunt.stack.hcl",
		}

		project, err := mgr.GenerateStackProject(stack)
		require.NoError(t, err)
		assert.Equal(t, "live/prod", project.Dir)
		assert.Equal(t, "live_prod", project.Name)
		assert.Equal(t, "live_prod", project.Workspace)
		assert.Equal(t, "stack-wf", project.Workflow)
		assert.True(t, project.Autoplan.Enabled, "global --autoplan should enable stack autoplan")
		assert.Equal(t, "1.9.0", project.TerraformVersion)
	})

	t.Run("explicit stack settings take precedence", func(t *testing.T) {
		stack := Stack{
			Name:   "live/staging",
			Source: "live/staging/terragrunt.stack.hcl",
			AtlantisConfig: StackAtlantisConfig{
				Workflow:         "custom-wf",
				Workspace:        "custom-ws",
				TerraformVersion: "1.5.0",
			},
		}

		project, err := mgr.GenerateStackProject(stack)
		require.NoError(t, err)
		assert.Equal(t, "custom-wf", project.Workflow)
		assert.Equal(t, "custom-ws", project.Workspace)
		assert.Equal(t, "1.5.0", project.TerraformVersion)
	})

	t.Run("unit sources outside the stack dir are watched", func(t *testing.T) {
		stack := Stack{
			Name:        "live/prod",
			Source:      "live/prod/terragrunt.stack.hcl",
			UnitSources: []string{"units/vpc", "units/app"},
		}

		project, err := mgr.GenerateStackProject(stack)
		require.NoError(t, err)
		assert.Equal(t, "live/prod", project.Dir)
		assert.Contains(t, project.Autoplan.WhenModified, "../../units/vpc/**/*.hcl")
		assert.Contains(t, project.Autoplan.WhenModified, "../../units/vpc/**/*.tf*")
		assert.Contains(t, project.Autoplan.WhenModified, "../../units/app/**/*.hcl")
	})

	t.Run("modules inside the stack dir are covered by base patterns", func(t *testing.T) {
		stack := Stack{
			Name:    "mystack",
			Source:  "mystack/terragrunt.stack.hcl",
			Modules: []string{"mystack/vpc", "mystack/app"},
		}

		project, err := mgr.GenerateStackProject(stack)
		require.NoError(t, err)
		assert.Equal(t, "mystack", project.Dir)
		assert.Equal(t, []string{"*.hcl", "*.tf*", "**/*.hcl", "**/*.tf*"}, project.Autoplan.WhenModified)
	})

	t.Run("dependency stack dirs are watched", func(t *testing.T) {
		mgr := NewStackManager(StackManagerConfig{GitRoot: "/repo"})
		mgr.stacks = []Stack{
			{Name: "shared", Source: "shared/terragrunt.stack.hcl", Modules: []string{"shared/vpc"}},
			{Name: "app", Source: "app/terragrunt.stack.hcl", Dependencies: []string{"shared"}},
		}

		project, err := mgr.GenerateStackProject(mgr.stacks[1])
		require.NoError(t, err)
		assert.Equal(t, "app", project.Dir)
		assert.Contains(t, project.Autoplan.WhenModified, "../shared/vpc/**/*.hcl")
	})
}

func TestStackManager_AssignModulesToStacks(t *testing.T) {
	// Use a real absolute path as gitRoot: on Windows "/repo" is not absolute,
	// so an absolute module path built from it could not be normalized.
	repoRoot := t.TempDir()
	absApp := filepath.Join(repoRoot, "mystack", "app", "terragrunt.hcl")

	mgr := NewStackManager(StackManagerConfig{GitRoot: repoRoot})
	mgr.stacks = []Stack{
		{
			Name:    "mystack",
			Source:  "mystack/terragrunt.stack.hcl",
			Modules: []string{"mystack/vpc", "mystack/app"},
		},
		{
			Name:    "prod-env",
			Include: []string{"environments/production/**"},
			Exclude: []string{"environments/production/experimental/**"},
		},
	}

	// terragrunt modules are usually discovered as paths to terragrunt.hcl files
	modules := []string{
		"mystack/vpc/terragrunt.hcl",
		absApp,
		"environments/production/app/terragrunt.hcl",
		"environments/production/experimental/db/terragrunt.hcl",
		"environments/staging/app/terragrunt.hcl",
	}

	assignments, err := mgr.AssignModulesToStacks(modules)
	require.NoError(t, err)

	assert.ElementsMatch(t, []string{"mystack/vpc", "mystack/app"}, assignments["mystack"])
	assert.ElementsMatch(t, []string{"environments/production/app"}, assignments["prod-env"])

	// GetStackForModule must work with file paths, directories and absolute paths
	assert.Equal(t, []string{"mystack"}, mgr.GetStackForModule("mystack/vpc/terragrunt.hcl"))
	assert.Equal(t, []string{"mystack"}, mgr.GetStackForModule(absApp))
	assert.Equal(t, []string{"prod-env"}, mgr.GetStackForModule("environments/production/app"))
	assert.Empty(t, mgr.GetStackForModule("environments/staging/app/terragrunt.hcl"))
	assert.Empty(t, mgr.GetStackForModule("environments/production/experimental/db/terragrunt.hcl"))
}

// Helper function
func writeFile(path string, content []byte) error {
	return os.WriteFile(path, content, 0644)
}
