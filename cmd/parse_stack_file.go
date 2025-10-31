package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ghodss/yaml"
	log "github.com/sirupsen/logrus"
)

// ParseStackDefinitionFile reads and parses a stack definition file (YAML or JSON)
func ParseStackDefinitionFile(path string) (*StackDefinitionFile, error) {
	// Check if file exists
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil, fmt.Errorf("stack definition file not found: %s", path)
	}

	// Read file contents
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read stack definition file: %w", err)
	}

	// Parse YAML/JSON (ghodss/yaml handles both)
	var stackDef StackDefinitionFile
	if err := yaml.Unmarshal(data, &stackDef); err != nil {
		return nil, fmt.Errorf("failed to parse stack definition file: %w", err)
	}

	// Validate the definition
	if err := ValidateStackDefinition(&stackDef); err != nil {
		return nil, fmt.Errorf("invalid stack definition: %w", err)
	}

	log.Infof("Loaded %d stack definition(s) from %s", len(stackDef.Stacks), path)
	return &stackDef, nil
}

// ValidateStackDefinition validates a stack definition file
func ValidateStackDefinition(def *StackDefinitionFile) error {
	if def.Version != 1 {
		return fmt.Errorf("unsupported stack definition version: %d (expected 1)", def.Version)
	}

	if len(def.Stacks) == 0 {
		return fmt.Errorf("no stacks defined")
	}

	stackNames := make(map[string]bool)
	for i, stack := range def.Stacks {
		// Validate stack name
		if stack.Name == "" {
			return fmt.Errorf("stack at index %d has no name", i)
		}

		// Check for duplicate names
		if stackNames[stack.Name] {
			return fmt.Errorf("duplicate stack name: %s", stack.Name)
		}
		stackNames[stack.Name] = true

		// Validate that either Include patterns or explicit Modules are specified
		if len(stack.Include) == 0 && len(stack.Modules) == 0 {
			return fmt.Errorf("stack '%s' must specify either 'include' patterns or 'modules' list", stack.Name)
		}

		// Validate glob patterns (basic check)
		for _, pattern := range stack.Include {
			if pattern == "" {
				return fmt.Errorf("stack '%s' has empty include pattern", stack.Name)
			}
		}
	}

	return nil
}

// ConvertExternalStacksToStacks converts external stack configs to internal Stack structs
func ConvertExternalStacksToStacks(externalStacks []ExternalStackConfig, gitRoot string) []Stack {
	stacks := make([]Stack, 0, len(externalStacks))

	for _, extStack := range externalStacks {
		stack := Stack{
			Name:         extStack.Name,
			Description:  extStack.Description,
			Modules:      extStack.Modules,
			Dependencies: extStack.DependsOn,
			Source:       "external-file",
			AtlantisConfig: StackAtlantisConfig{
				Workflow:          extStack.Atlantis.Workflow,
				AutoPlan:          extStack.Atlantis.AutoPlan,
				Parallel:          extStack.Atlantis.Parallel,
				ApplyRequirements: extStack.Atlantis.ApplyRequirements,
				Workspace:         extStack.Atlantis.Workspace,
				TerraformVersion:  extStack.Atlantis.TerraformVersion,
			},
			ExecutionOrder: extStack.Atlantis.ExecutionOrderGroup,
		}

		stacks = append(stacks, stack)
	}

	return stacks
}

// MatchModuleToStacks determines which stacks a module belongs to based on patterns
func MatchModuleToStacks(modulePath string, stacks []ExternalStackConfig, gitRoot string) []string {
	matchedStacks := []string{}

	// Normalize module path to be relative to gitRoot
	relPath, err := filepath.Rel(gitRoot, modulePath)
	if err != nil {
		log.Warnf("Failed to get relative path for %s: %v", modulePath, err)
		relPath = modulePath
	}
	relPath = filepath.ToSlash(relPath)

	for _, stack := range stacks {
		// Check explicit module list first
		if len(stack.Modules) > 0 {
			for _, explicitModule := range stack.Modules {
				// Normalize explicit module path
				explicitModule = filepath.ToSlash(explicitModule)
				if strings.HasSuffix(relPath, explicitModule) || relPath == explicitModule {
					matchedStacks = append(matchedStacks, stack.Name)
					break
				}
			}
			continue
		}

		// Check include/exclude patterns
		included := false
		for _, pattern := range stack.Include {
			if matchGlobPattern(relPath, pattern) {
				included = true
				break
			}
		}

		if !included {
			continue
		}

		// Check exclusions
		excluded := false
		for _, pattern := range stack.Exclude {
			if matchGlobPattern(relPath, pattern) {
				excluded = true
				break
			}
		}

		if !excluded {
			matchedStacks = append(matchedStacks, stack.Name)
		}
	}

	return matchedStacks
}

// matchGlobPattern performs glob pattern matching
// This is a simple implementation - could be enhanced with filepath.Match or doublestar
func matchGlobPattern(path, pattern string) bool {
	// Convert glob pattern to Go's filepath.Match format
	// Handle ** for recursive matching
	if strings.Contains(pattern, "**") {
		// Simple recursive match: if pattern is "a/**/b", match any path containing "a" and "b"
		parts := strings.Split(pattern, "**")
		if len(parts) == 2 {
			prefix := strings.TrimSuffix(parts[0], "/")
			suffix := strings.TrimPrefix(parts[1], "/")

			if prefix != "" && !strings.HasPrefix(path, prefix) {
				return false
			}
			if suffix != "" && !strings.HasSuffix(path, suffix) {
				return false
			}
			return true
		}
	}

	// Use standard filepath.Match for simple patterns
	matched, err := filepath.Match(pattern, path)
	if err != nil {
		log.Warnf("Invalid glob pattern '%s': %v", pattern, err)
		return false
	}

	return matched
}

// FindStackDependencies resolves dependencies between stacks
func FindStackDependencies(stacks []Stack, projectMap map[string][]string) map[string][]string {
	stackDeps := make(map[string][]string)

	for _, stack := range stacks {
		deps := []string{}

		// Add explicitly declared dependencies
		deps = append(deps, stack.Dependencies...)

		// Add implicit dependencies based on module dependencies
		// (This would require analyzing Terragrunt dependencies)
		// TODO: Implement implicit dependency detection

		stackDeps[stack.Name] = uniqueStrings(deps)
	}

	return stackDeps
}

// GenerateStackDefinitionTemplate generates a template stack definition file
func GenerateStackDefinitionTemplate(outputPath string) error {
	template := `version: 1
stacks:
  # Example environment-based stack
  - name: production-environment
    description: Complete production infrastructure
    
    # Use glob patterns to include modules
    include:
      - "environments/production/**"
    
    # Optionally exclude specific paths
    exclude:
      - "environments/production/experimental/**"
    
    # Stack dependencies
    depends_on:
      - shared-services
    
    # Atlantis configuration
    atlantis:
      workflow: production
      autoplan: true
      parallel: true
      apply_requirements:
        - approved
        - mergeable
      execution_order_group: 10
  
  # Example service-based stack
  - name: auth-service
    description: Authentication service infrastructure
    
    # Or specify explicit module paths
    modules:
      - services/auth/api
      - services/auth/database
      - services/auth/cache
    
    atlantis:
      workflow: service
      autoplan: true
      execution_order_group: 20

  # Example shared infrastructure stack
  - name: shared-services
    include:
      - "shared/**"
    atlantis:
      workflow: shared
      autoplan: false
      apply_requirements:
        - approved
      execution_order_group: 1
`

	if err := os.WriteFile(outputPath, []byte(template), 0644); err != nil {
		return fmt.Errorf("failed to write template file: %w", err)
	}

	log.Infof("Generated stack definition template at %s", outputPath)
	return nil
}
