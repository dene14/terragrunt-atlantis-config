package cmd

import (
	"os"
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
	mgr := NewStackManager(StackManagerConfig{
		GitRoot: "/repo",
	})

	stack := Stack{
		Name:         "test-stack",
		Description:  "Test stack",
		Modules:      []string{"app-a", "app-b"},
		Dependencies: []string{"dependency-stack"},
		AtlantisConfig: StackAtlantisConfig{
			Workflow:          "test-workflow",
			AutoPlan:          true,
			Parallel:          true,
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
}

// Helper function
func writeFile(path string, content []byte) error {
	return os.WriteFile(path, content, 0644)
}

