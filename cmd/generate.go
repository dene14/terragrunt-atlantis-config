package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"

	log "github.com/sirupsen/logrus"

	"github.com/ghodss/yaml"
	"github.com/spf13/cobra"
)

func uniqueStrings(str []string) []string {
	keys := make(map[string]bool)
	list := []string{}
	for _, entry := range str {
		if _, value := keys[entry]; !value {
			keys[entry] = true
			list = append(list, entry)
		}
	}
	return list
}

func main(cmd *cobra.Command, args []string) error {
	// Ensure the gitRoot has a trailing slash and is an absolute path
	absoluteGitRoot, err := filepath.Abs(gitRoot)
	if err != nil {
		return err
	}
	gitRoot = absoluteGitRoot + string(filepath.Separator)

	if err := validateEngine(); err != nil {
		return err
	}

	// Read in the old config, if it already exists
	oldConfig, err := readOldConfig()
	if err != nil {
		return err
	}
	config := AtlantisConfig{
		Version:                   3,
		AutoMerge:                 autoMerge,
		ParallelPlan:              parallel,
		ParallelApply:             parallel,
		DeleteSourceBranchOnMerge: deleteSourceBranchOnMerge,
	}
	if oldConfig != nil && preserveWorkflows {
		config.Workflows = oldConfig.Workflows
	}
	if oldConfig != nil && preserveProjects {
		config.Projects = oldConfig.Projects
	}

	cliProjects, err := generateProjectsWithCLIEngine(gitRoot)
	if err != nil {
		return err
	}

	if preserveProjects {
		// Same update-in-place semantics as before: projects that already
		// exist are refreshed by dir, new ones appended.
		for _, project := range cliProjects {
			updated := false
			for i := range config.Projects {
				if config.Projects[i].Dir == project.Dir {
					config.Projects[i] = project
					updated = true
					break
				}
			}
			if !updated {
				config.Projects = append(config.Projects, project)
			}
		}
	} else {
		config.Projects = append(config.Projects, cliProjects...)
	}

	if gitFilter != "" {
		filtered, err := filterProjectsByGitDiff(config.Projects, gitRoot, gitFilter)
		if err != nil {
			return err
		}
		log.Infof("--filter-git: kept %d of %d projects touched by %s...HEAD", len(filtered), len(config.Projects), gitFilter)
		config.Projects = filtered
	}

	// --exclude prunes projects after discovery, for both engines. Patterns
	// are directory selectors (root-, cwd-, or absolute form) — everything
	// beneath a matched directory is dropped, mirroring --filter's glob
	// semantics back-to-front. (See engine_cli.go dirGlobMatches.)
	if len(excludePaths) > 0 {
		normalised := normalizeFilterPaths(excludePaths, gitRoot)
		kept := make([]AtlantisProject, 0, len(config.Projects))
		for _, p := range config.Projects {
			dropped := false
			for _, ex := range normalised {
				if dirGlobMatches(ex, p.Dir) {
					dropped = true
					break
				}
			}
			if !dropped {
				kept = append(kept, p)
			} else {
				log.Debugf("--exclude dropped project %s", p.Dir)
			}
		}
		config.Projects = kept
	}

	// Sort the projects in config by Dir
	sort.Slice(config.Projects, func(i, j int) bool { return config.Projects[i].Dir < config.Projects[j].Dir })

	// Preserved workflows are carried over verbatim (see preserve_order.go):
	// emit the config without them, then append the original section, so key
	// order and comments survive regeneration untouched.
	preservedWorkflowsSection := ""
	if preserveWorkflows && config.Workflows != nil {
		if section := extractTopLevelKeySection(oldConfigRaw, "workflows"); section != "" {
			preservedWorkflowsSection = section
			config.Workflows = nil
		}
	}

	// User-owned top-level keys (allowed_regexp_prefixes, checkout_strategy,
	// delete_source_branch_on_merge, ...) also survive verbatim; explicit
	// flags take precedence over preservation.
	skipPreserved := []string{}
	if deleteSourceBranchOnMerge {
		skipPreserved = append(skipPreserved, "delete_source_branch_on_merge")
	}
	userSections := preservedUserSections(oldConfigRaw, skipPreserved...)

	// Convert config to YAML string
	yamlBytes, err := yaml.Marshal(&config)
	if err != nil {
		return err
	}

	// Assemble with plain "\n" first, then convert for windows in one final
	// pass (the json encoder emits "\n" on every OS:
	// https://github.com/golang/go/blob/master/src/encoding/json/stream.go#L211-L217)
	yamlString := string(yamlBytes)
	if preservedWorkflowsSection != "" {
		yamlString = strings.TrimRight(yamlString, "\n") + "\n" +
			strings.TrimRight(preservedWorkflowsSection, "\r\n") + "\n"
	}
	for _, section := range userSections {
		yamlString = strings.TrimRight(yamlString, "\n") + "\n" +
			strings.TrimRight(section, "\r\n") + "\n"
	}
	if strings.Contains(runtime.GOOS, "windows") {
		yamlString = strings.ReplaceAll(yamlString, "\n", "\r\n")
	}

	// Write output
	if len(outputPath) != 0 {
		// Ensure the directory exists before writing
		outputDir := filepath.Dir(outputPath)
		// Only create directory if it's not the current directory or empty
		// filepath.Dir returns "." for files in current directory, which already exists
		if outputDir != "." && outputDir != "" {
			if err := os.MkdirAll(outputDir, 0755); err != nil {
				return fmt.Errorf("failed to create output directory: %w", err)
			}
		}
		if err := os.WriteFile(outputPath, []byte(yamlString), 0644); err != nil {
			return fmt.Errorf("failed to write output file: %w", err)
		}
	} else {
		// The generated config is the program's payload, not a log line: it
		// goes to stdout so `generate | yq ...` pipelines work. Diagnostics
		// keep flowing to stderr via logrus.
		fmt.Println(yamlString)
	}

	return nil
}

var gitRoot string
var autoPlan bool
var autoMerge bool
var ignoreDependencyBlocks bool
var parallel bool
var deleteSourceBranchOnMerge bool
var createWorkspace bool
var createProjectName bool
var defaultTerraformVersion string
var defaultTerraformDistribution string
var defaultWorkflow string
var stackWorkflow string

// projectNameRegex sanitizes directory or stack names into valid Atlantis
// project names and workspace names.
var projectNameRegex = regexp.MustCompile(`[^a-zA-Z0-9_-]+`)
var filterPaths []string
var excludePaths []string
var gitFilter string
var outputPath string
var preserveWorkflows bool
var preserveProjects bool
var cascadeDependencies bool
var defaultApplyRequirements []string
var executionOrderGroups bool
var dependsOn bool

// generateCmd represents the generate command
var generateCmd = &cobra.Command{
	Use:   "generate",
	Short: "Makes atlantis config",
	Long:  `Logs Yaml representing Atlantis config to stderr`,
	// Test is needed to confirm that if --depends on is set, --create-project-name is also set.
	PreRun: func(cmd *cobra.Command, args []string) {
		dependsOn, _ := cmd.Flags().GetBool("depends-on")
		if dependsOn {
			cmd.MarkFlagRequired("create-project-name")
		}
	},
	RunE: main,
}

func init() {
	rootCmd.AddCommand(generateCmd)

	pwd, err := os.Getwd()
	if err != nil {
		log.Fatal(err)
	}

	generateCmd.PersistentFlags().BoolVar(&autoPlan, "autoplan", false, "Enable auto plan. Default is disabled")
	generateCmd.PersistentFlags().BoolVar(&autoMerge, "automerge", false, "Enable auto merge. Default is disabled")
	generateCmd.PersistentFlags().BoolVar(&deleteSourceBranchOnMerge, "delete-source-branch-on-merge", false, "Tell Atlantis to delete the source branch after merge. Overrides any preserved value from a previous atlantis.yaml")
	generateCmd.PersistentFlags().BoolVar(&ignoreDependencyBlocks, "ignore-dependency-blocks", false, "When true, dependencies found in `dependency` blocks will be ignored")
	generateCmd.PersistentFlags().BoolVar(&parallel, "parallel", true, "Enables plans and applys to happen in parallel. Default is enabled")
	generateCmd.PersistentFlags().BoolVar(&createWorkspace, "create-workspace", false, "Use different workspace for each project. Default is use default workspace")
	generateCmd.PersistentFlags().BoolVar(&createProjectName, "create-project-name", false, "Add different name for each project. Default is false")
	generateCmd.PersistentFlags().BoolVar(&preserveWorkflows, "preserve-workflows", true, "Preserves workflows from old output files. Default is true")
	generateCmd.PersistentFlags().BoolVar(&preserveProjects, "preserve-projects", false, "Preserves projects from old output files to enable incremental builds. Default is false")
	generateCmd.PersistentFlags().BoolVar(&cascadeDependencies, "cascade-dependencies", true, "When true, dependencies will cascade, meaning that a module will be declared to depend not only on its dependencies, but all dependencies of its dependencies all the way down. Default is true")
	generateCmd.PersistentFlags().StringVar(&defaultWorkflow, "workflow", "", "Name of the workflow to be customized in the atlantis server. Default is to not set")
	generateCmd.PersistentFlags().StringSliceVar(&defaultApplyRequirements, "apply-requirements", []string{}, "Requirements that must be satisfied before `atlantis apply` can be run. Currently the only supported requirements are `approved` and `mergeable`. Can be overridden by locals")
	generateCmd.PersistentFlags().StringVar(&outputPath, "output", "", "Path of the file where configuration will be generated. Default is not to write to file")
	generateCmd.PersistentFlags().StringSliceVar(&filterPaths, "filter", []string{}, "Comma-separated paths or glob expressions to the directories you want scope down the config for. Default is all files in root.")
	generateCmd.PersistentFlags().StringSliceVar(&excludePaths, "exclude", []string{}, "Comma-separated paths or glob expressions to subtract from the discovered projects. Directories and everything beneath a matched directory are dropped.")
	generateCmd.PersistentFlags().StringVar(&gitFilter, "filter-git", "", "Only include projects whose autoplan triggers were touched between the given git ref and HEAD (e.g. origin/main). Works with both engines.")
	generateCmd.PersistentFlags().StringVar(&gitRoot, "root", pwd, "Path to the root directory of the git repo you want to build config for. Default is current dir")
	generateCmd.PersistentFlags().StringVar(&defaultTerraformVersion, "terraform-version", "", "Default terraform version to specify for all modules. Can be overriden by locals")
	generateCmd.PersistentFlags().StringVar(&defaultTerraformDistribution, "terraform-distribution", "", "Default terraform distribution to specify for all modules (e.g. 'tofu'). Can be overriden by the atlantis_terraform_distribution locals")
	generateCmd.PersistentFlags().BoolVar(&executionOrderGroups, "execution-order-groups", false, "Computes execution_order_groups for projects")
	generateCmd.PersistentFlags().BoolVar(&dependsOn, "depends-on", false, "Computes depends_on for projects. Requires --create-project-name.")
	generateCmd.PersistentFlags().StringVar(&engine, "engine", engineAuto, "Parsing engine: 'cli' discovers via the terragrunt binary (terragrunt v1.x required). 'auto' is accepted as an alias of cli. 'library' was removed in v1.27.0.")
	generateCmd.PersistentFlags().StringVar(&stackWorkflow, "stack-workflow", "", "Default workflow name for stack projects (stacks always used when terragrunt.stack.hcl is present). If not set, uses the value from --workflow flag")
}

// Runs a set of arguments, returning the output
func RunWithFlags(filename string, args []string) ([]byte, error) {
	rootCmd.SetArgs(args)
	err := rootCmd.Execute()
	if err != nil {
		return nil, err
	}

	return os.ReadFile(filename)
}
