package cmd

import (
	"regexp"
	"sort"

	"github.com/hashicorp/go-getter"
	log "github.com/sirupsen/logrus"

	"github.com/ghodss/yaml"
	"github.com/gruntwork-io/terragrunt/pkg/config"
	"github.com/gruntwork-io/terragrunt/pkg/options"
	"github.com/spf13/cobra"

	"golang.org/x/sync/errgroup"
	"golang.org/x/sync/semaphore"
	"golang.org/x/sync/singleflight"

	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
)

// Parse env vars into a map
func getEnvs() map[string]string {
	envs := os.Environ()
	m := make(map[string]string)

	for _, env := range envs {
		results := strings.SplitN(env, "=", 2)
		m[results[0]] = results[1]
	}

	return m
}

// Terragrunt imports can be relative or absolute
// This makes relative paths absolute
func makePathAbsolute(path string, parentPath string) string {
	if strings.HasPrefix(path, filepath.ToSlash(gitRoot)) {
		return path
	}

	parentDir := filepath.Dir(parentPath)
	return filepath.Join(parentDir, path)
}

var requestGroup singleflight.Group

// Set up a cache for the getDependencies function
type getDependenciesOutput struct {
	dependencies []string
	err          error
}

type GetDependenciesCache struct {
	mtx  sync.RWMutex
	data map[string]getDependenciesOutput
}

func newGetDependenciesCache() *GetDependenciesCache {
	return &GetDependenciesCache{data: map[string]getDependenciesOutput{}}
}

func (m *GetDependenciesCache) set(k string, v getDependenciesOutput) {
	m.mtx.Lock()
	defer m.mtx.Unlock()
	m.data[k] = v
}

func (m *GetDependenciesCache) get(k string) (getDependenciesOutput, bool) {
	m.mtx.RLock()
	defer m.mtx.RUnlock()
	v, ok := m.data[k]
	return v, ok
}

var getDependenciesCache = newGetDependenciesCache()

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

func lookupProjectHcl(m map[string][]string, value string) (key string) {
	for k, values := range m {
		for _, val := range values {
			if val == value {
				key = k
				return
			}
		}
	}
	return key
}

// sliceUnion takes two slices of strings and produces a union of them, containing only unique values
func sliceUnion(a, b []string) []string {
	m := make(map[string]bool)

	for _, item := range a {
		m[item] = true
	}

	for _, item := range b {
		if _, ok := m[item]; !ok {
			a = append(a, item)
		}
	}
	return a
}

// Parses the terragrunt config at `path` to find all modules it depends on
func getDependencies(ctx *config.ParsingContext, path string) ([]string, error) {
	res, err, _ := requestGroup.Do(path, func() (interface{}, error) {
		// Check if this path has already been computed
		cachedResult, ok := getDependenciesCache.get(path)
		if ok {
			return cachedResult.dependencies, cachedResult.err
		}

		// parse the module path to find what it includes, as well as its potential to be a parent
		// return nils to indicate we should skip this project
		isParent, includes, err := parseModule(ctx, path)
		if err != nil {
			getDependenciesCache.set(path, getDependenciesOutput{nil, err})
			return nil, err
		}
		if isParent && ignoreParentTerragrunt {
			getDependenciesCache.set(path, getDependenciesOutput{nil, nil})
			return nil, nil
		}

		dependencies := []string{}
		if len(includes) > 0 {
			for _, includeDep := range includes {
				// NOTE: do NOT seed the cache for includeDep.Path here.
				// Seeding it with a nil result (i.e. "skip this project")
				// races with the include target's own project computation
				// (e.g. with --ignore-parent-terragrunt=false): whichever
				// goroutine reaches the path first decided whether the
				// parent project existed at all, producing flaky output.
				dependencies = append(dependencies, includeDep.Path)
			}
		}

		// Parse the HCL file
		parseCtx := newParsingContext(context.Background(), ctx.TerragruntOptions).
			WithDecodeList(
				config.DependencyBlock,
				config.DependenciesBlock,
				config.TerraformBlock,
			)
		parsedConfig, err := config.PartialParseConfigFile(context.Background(), parseCtx, quietTerragruntLogger(), path, nil)
		if err != nil {
			getDependenciesCache.set(path, getDependenciesOutput{nil, err})
			return nil, err
		}

		// Parse out locals
		locals, err := parseLocals(ctx, path, nil)
		if err != nil {
			getDependenciesCache.set(path, getDependenciesOutput{nil, err})
			return nil, err
		}

		// Get deps from locals
		if locals.ExtraAtlantisDependencies != nil {
			dependencies = sliceUnion(dependencies, locals.ExtraAtlantisDependencies)
		}

		// Get deps from `dependencies` and `dependency` blocks
		if parsedConfig.Dependencies != nil {
			depBlockPaths := make([]string, 0, len(parsedConfig.Dependencies.Paths))
			for _, parsedPaths := range parsedConfig.Dependencies.Paths {
				depPath := filepath.Join(parsedPaths, "terragrunt.hcl")
				depBlockPaths = append(depBlockPaths, depPath)
				if !ignoreDependencyBlocks {
					dependencies = append(dependencies, depPath)
				}
			}
			// Ordering (--execution-order-groups / --depends-on) must respect
			// dependency edges even when they are deliberately not watched.
			absEdges := make([]string, 0, len(depBlockPaths))
			for _, depPath := range depBlockPaths {
				if !filepath.IsAbs(depPath) {
					depPath = makePathAbsolute(depPath, path)
				}
				absEdges = append(absEdges, depPath)
			}
			recordDepBlockEdges(path, absEdges)
		}

		// Get deps from the `Source` field of the `Terraform` block
		if parsedConfig.Terraform != nil && parsedConfig.Terraform.Source != nil {
			source := parsedConfig.Terraform.Source

			// Use `go-getter` to normalize the source paths
			parsedSource, err := getter.Detect(*source, filepath.Dir(path), getter.Detectors)
			if err != nil {
				return nil, err
			}

			// Check if the path begins with a drive letter, denoting Windows
			isWindowsPath, err := regexp.MatchString(`^[A-Za-z]:`, parsedSource)
			if err != nil {
				return nil, err
			}

			// If the normalized source begins with `file://`, or matched the Windows drive letter check, it is a local path
			if strings.HasPrefix(parsedSource, "file://") || isWindowsPath {
				// Remove the prefix so we have a valid filesystem path
				parsedSource = strings.TrimPrefix(parsedSource, "file://")

				dependencies = append(dependencies, filepath.Join(parsedSource, "*.tf*"))

				ls, err := parseTerraformLocalModuleSource(parsedSource)
				if err != nil {
					return nil, err
				}
				sort.Strings(ls)

				dependencies = append(dependencies, ls...)
			}
		}

		// Get deps from `extra_arguments` fields of the `Terraform` block
		if parsedConfig.Terraform != nil && parsedConfig.Terraform.ExtraArgs != nil {
			extraArgs := parsedConfig.Terraform.ExtraArgs
			for _, arg := range extraArgs {
				if arg.RequiredVarFiles != nil {
					dependencies = append(dependencies, *arg.RequiredVarFiles...)
				}
				if arg.OptionalVarFiles != nil {
					dependencies = append(dependencies, *arg.OptionalVarFiles...)
				}
				if arg.Arguments != nil {
					for _, cliFlag := range *arg.Arguments {
						if strings.HasPrefix(cliFlag, "-var-file=") {
							dependencies = append(dependencies, strings.TrimPrefix(cliFlag, "-var-file="))
						}
					}
				}
			}
		}

		// Filter out and dependencies that are the empty string
		nonEmptyDeps := []string{}
		for _, dep := range dependencies {
			if dep != "" {
				childDepAbsPath := dep
				if !filepath.IsAbs(childDepAbsPath) {
					childDepAbsPath = makePathAbsolute(dep, path)
				}
				childDepAbsPath = filepath.ToSlash(childDepAbsPath)
				nonEmptyDeps = append(nonEmptyDeps, childDepAbsPath)
			}
		}

		// Recurse to find dependencies of all dependencies
		cascadedDeps := []string{}
		for _, dep := range nonEmptyDeps {
			cascadedDeps = append(cascadedDeps, dep)

			// The "cascading" feature is protected by a flag
			if !cascadeDependencies {
				continue
			}

			depPath := dep
			terrOpts, _ := options.NewTerragruntOptionsWithConfigPath(depPath)
			terrOpts.OriginalTerragruntConfigPath = ctx.TerragruntOptions.OriginalTerragruntConfigPath
			terrOpts.Env = ctx.TerragruntOptions.Env
			terrContext := newParsingContext(context.Background(), terrOpts)
			childDeps, err := getDependencies(terrContext, depPath)
			if err != nil {
				continue
			}

			for _, childDep := range childDeps {
				// If `childDep` is a relative path, it will be relative to `childDep`, as it is from the nested
				// `getDependencies` call on the top level module's dependencies. So here we update any relative
				// path to be from the top level module instead.
				childDepAbsPath := childDep
				if !filepath.IsAbs(childDep) {
					childDepAbsPath, err = filepath.Abs(filepath.Join(depPath, "..", childDep))
					if err != nil {
						getDependenciesCache.set(path, getDependenciesOutput{nil, err})
						return nil, err
					}
				}
				childDepAbsPath = filepath.ToSlash(childDepAbsPath)

				// Ensure we are not adding a duplicate dependency
				alreadyExists := false
				for _, dep := range cascadedDeps {
					if dep == childDepAbsPath {
						alreadyExists = true
						break
					}
				}
				if !alreadyExists {
					cascadedDeps = append(cascadedDeps, childDepAbsPath)
				}
			}
		}

		if filepath.Base(path) == "terragrunt.hcl" {
			dir := filepath.Dir(path)

			ls, err := parseTerraformLocalModuleSource(dir)
			if err != nil {
				return nil, err
			}
			sort.Strings(ls)

			cascadedDeps = append(cascadedDeps, ls...)
		}

		getDependenciesCache.set(path, getDependenciesOutput{cascadedDeps, err})
		return cascadedDeps, nil
	})

	if res != nil {
		return res.([]string), err
	} else {
		return nil, err
	}
}

// Creates an AtlantisProject for a directory
func createProject(ctx context.Context, sourcePath string) (*AtlantisProject, error) {
	options, err := options.NewTerragruntOptionsWithConfigPath(sourcePath)
	if err != nil {
		return nil, err
	}
	options.OriginalTerragruntConfigPath = sourcePath
	options.Env = getEnvs()

	parsingContext := newParsingContext(ctx, options)
	dependencies, err := getDependencies(parsingContext, sourcePath)
	if err != nil {
		return nil, err
	}

	// dependencies being nil is a sign from `getDependencies` that this project should be skipped
	if dependencies == nil {
		return nil, nil
	}

	absoluteSourceDir := filepath.Dir(sourcePath) + string(filepath.Separator)
	locals, err := parseLocals(parsingContext, sourcePath, nil)
	if err != nil {
		return nil, err
	}

	// If `atlantis_skip` is true on the module, then do not produce a project for it
	if locals.Skip != nil && *locals.Skip {
		return nil, nil
	}

	// All dependencies depend on their own .hcl file, and any tf files in their directory
	relativeDependencies := []string{
		"*.hcl",
		"*.tf*",
	}

	// Add other dependencies based on their relative paths. We always want to output with Unix path separators
	for _, dependencyPath := range dependencies {
		absolutePath := dependencyPath
		if !filepath.IsAbs(absolutePath) {
			absolutePath = makePathAbsolute(dependencyPath, sourcePath)
		}
		relativePath, err := filepath.Rel(absoluteSourceDir, absolutePath)
		if err != nil {
			return nil, err
		}

		relativeDependencies = append(relativeDependencies, filepath.ToSlash(relativePath))
	}

	// Clean up the relative path to the format Atlantis expects
	relativeSourceDir := strings.TrimPrefix(absoluteSourceDir, gitRoot)
	relativeSourceDir = strings.TrimSuffix(relativeSourceDir, string(filepath.Separator))
	if relativeSourceDir == "" {
		relativeSourceDir = "."
	}

	workflow := defaultWorkflow
	if locals.AtlantisWorkflow != "" {
		workflow = locals.AtlantisWorkflow
	}

	applyRequirements := &defaultApplyRequirements
	if len(defaultApplyRequirements) == 0 {
		applyRequirements = nil
	}
	if locals.ApplyRequirements != nil {
		applyRequirements = &locals.ApplyRequirements
	}

	resolvedAutoPlan := autoPlan
	if locals.AutoPlan != nil {
		resolvedAutoPlan = *locals.AutoPlan
	}

	terraformVersion := resolveTerraformVersion(locals.TerraformVersion, filepath.Dir(sourcePath))

	terraformDistribution := defaultTerraformDistribution
	if locals.TerraformDistribution != "" {
		terraformDistribution = locals.TerraformDistribution
	}

	project := &AtlantisProject{
		Dir:                   filepath.ToSlash(relativeSourceDir),
		Workflow:              workflow,
		TerraformVersion:      terraformVersion,
		TerraformDistribution: terraformDistribution,
		ApplyRequirements:     applyRequirements,
		Autoplan: AutoplanConfig{
			Enabled:      resolvedAutoPlan,
			WhenModified: uniqueStrings(relativeDependencies),
		},
	}

	// Terraform Cloud limits the workspace names to be less than 90 characters
	// with letters, numbers, -, and _
	// https://www.terraform.io/docs/cloud/workspaces/naming.html
	// It is not clear from documentation whether the normal workspaces have those limitations
	// However a workspace 97 chars long has been working perfectly.
	// We are going to use the same name for both workspace & project name as it is unique.
	projectName := projectNameRegex.ReplaceAllString(project.Dir, "_")

	if createProjectName {
		project.Name = projectName
	}

	if createWorkspace {
		project.Workspace = projectName
	}

	return project, nil
}

func createHclProject(ctx context.Context, sourcePaths []string, workingDir string, projectHcl string) (*AtlantisProject, error) {
	var projectHclDependencies []string
	var childDependencies []string
	workflow := defaultWorkflow
	applyRequirements := &defaultApplyRequirements
	resolvedAutoPlan := autoPlan

	projectHclFile := filepath.Join(workingDir, projectHcl)
	projectHclOptions, err := options.NewTerragruntOptionsWithConfigPath(workingDir)
	if err != nil {
		return nil, err
	}
	projectHclOptions.Env = getEnvs()

	parsingContext := newParsingContext(ctx, projectHclOptions)
	locals, err := parseLocals(parsingContext, projectHclFile, nil)
	if err != nil {
		return nil, err
	}

	// If `atlantis_skip` is true on the module, then do not produce a project for it
	if locals.Skip != nil && *locals.Skip {
		return nil, nil
	}

	// if project markers are enabled, check if locals are set
	markedProject := false
	if locals.markedProject != nil {
		markedProject = *locals.markedProject
	}
	if useProjectMarkers && !markedProject {
		return nil, nil
	}

	if locals.ExtraAtlantisDependencies != nil {
		for _, dep := range locals.ExtraAtlantisDependencies {
			relDep, err := filepath.Rel(workingDir, dep)
			if err != nil {
				return nil, err
			}
			projectHclDependencies = append(projectHclDependencies, filepath.ToSlash(relDep))
		}
	}

	if locals.AtlantisWorkflow != "" {
		workflow = locals.AtlantisWorkflow
	}

	if len(defaultApplyRequirements) == 0 {
		applyRequirements = nil
	}
	if locals.ApplyRequirements != nil {
		applyRequirements = &locals.ApplyRequirements
	}

	if locals.AutoPlan != nil {
		resolvedAutoPlan = *locals.AutoPlan
	}

	terraformVersion := resolveTerraformVersion(locals.TerraformVersion, workingDir)

	terraformDistribution := defaultTerraformDistribution
	if locals.TerraformDistribution != "" {
		terraformDistribution = locals.TerraformDistribution
	}

	// build dependencies for terragrunt childs in directories below project hcl file
	for _, sourcePath := range sourcePaths {
		opt, err := options.NewTerragruntOptionsWithConfigPath(sourcePath)
		if err != nil {
			return nil, err
		}
		opt.Env = getEnvs()
		parsingContext := newParsingContext(ctx, opt)
		dependencies, err := getDependencies(parsingContext, sourcePath)
		if err != nil {
			return nil, err
		}
		// dependencies being nil is a sign from `getDependencies` that this project should be skipped
		if dependencies == nil {
			return nil, nil
		}

		// All dependencies depend on their own .hcl file, and any tf files in their directory
		relativeDependencies := []string{
			"*.hcl",
			"*.tf*",
			"**/*.hcl",
			"**/*.tf*",
		}

		// Add other dependencies based on their relative paths. We always want to output with Unix path separators
		for _, dependencyPath := range dependencies {
			absolutePath := dependencyPath
			if !filepath.IsAbs(absolutePath) {
				absolutePath = makePathAbsolute(dependencyPath, sourcePath)
			}

			relativePath, err := filepath.Rel(workingDir, absolutePath)
			if err != nil {
				return nil, err
			}

			if !strings.Contains(absolutePath, filepath.ToSlash(workingDir)) {
				relativeDependencies = append(relativeDependencies, filepath.ToSlash(relativePath))
			}
		}

		childDependencies = append(childDependencies, relativeDependencies...)
	}
	dir, err := filepath.Rel(gitRoot, workingDir)
	if err != nil {
		return nil, err
	}

	project := &AtlantisProject{
		Dir:                   filepath.ToSlash(dir),
		Workflow:              workflow,
		TerraformVersion:      terraformVersion,
		TerraformDistribution: terraformDistribution,
		ApplyRequirements:     applyRequirements,
		Autoplan: AutoplanConfig{
			Enabled:      resolvedAutoPlan,
			WhenModified: uniqueStrings(append(childDependencies, projectHclDependencies...)),
		},
	}

	// Terraform Cloud limits the workspace names to be less than 90 characters
	// with letters, numbers, -, and _
	// https://www.terraform.io/docs/cloud/workspaces/naming.html
	// It is not clear from documentation whether the normal workspaces have those limitations
	// However a workspace 97 chars long has been working perfectly.
	// We are going to use the same name for both workspace & project name as it is unique.
	projectName := projectNameRegex.ReplaceAllString(project.Dir, "_")

	if createProjectName {
		project.Name = projectName
	}

	if createWorkspace {
		project.Workspace = projectName
	}

	return project, nil
}

// Finds the absolute paths of all terragrunt.hcl files
func getAllTerragruntFiles(path string) ([]string, error) {
	options, err := options.NewTerragruntOptionsWithConfigPath(path)
	if err != nil {
		return nil, err
	}

	// If filterPaths is provided, override workingPath instead of gitRoot
	// We do this here because we want to keep the relative path structure of Terragrunt files
	// to root and just ignore the ConfigFiles
	workingPaths := []string{path}

	// filters are not working (yet) if using project hcl files (which are kind of filters by themselves)
	if len(filterPaths) > 0 && len(projectHclFiles) == 0 {
		workingPaths = []string{}
		for _, filterPath := range filterPaths {
			// get all matching folders
			theseWorkingPaths, err := filepath.Glob(filterPath)
			if err != nil {
				return nil, err
			}
			// A filter matching nothing silently produces an empty
			// atlantis.yaml, which is a very expensive way to learn about a
			// typo. Fail loudly instead.
			if len(theseWorkingPaths) == 0 {
				return nil, fmt.Errorf("--filter %q matched no directories", filterPath)
			}
			workingPaths = append(workingPaths, theseWorkingPaths...)
		}
	}

	uniqueConfigFilePaths := make(map[string]bool)
	orderedConfigFilePaths := []string{}
	for _, workingPath := range workingPaths {
		paths, err := FindConfigFilesInPath(workingPath, options)
		if err != nil {
			return nil, err
		}
		for _, p := range paths {
			// if path not yet seen, insert once
			if !uniqueConfigFilePaths[p] {
				orderedConfigFilePaths = append(orderedConfigFilePaths, p)
				uniqueConfigFilePaths[p] = true
			}
		}
	}

	uniqueConfigFileAbsPaths := []string{}
	for _, uniquePath := range orderedConfigFilePaths {
		uniqueAbsPath, err := filepath.Abs(uniquePath)
		if err != nil {
			return nil, err
		}
		uniqueConfigFileAbsPaths = append(uniqueConfigFileAbsPaths, uniqueAbsPath)
	}

	return uniqueConfigFileAbsPaths, nil
}

// FindConfigFilesInPath returns a list of all Terragrunt config files in the given path or any subfolder of the path. A file is a Terragrunt
// config file if it has a name as returned by the DefaultConfigPath method
func FindConfigFilesInPath(rootPath string, opts *options.TerragruntOptions) ([]string, error) {
	configFiles := []string{}

	walkFunc := filepath.Walk

	err := walkFunc(rootPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			// Unreadable directories (e.g. stale root-owned .terragrunt-cache
			// from container runs) must not abort discovery of everything else
			if info != nil && info.IsDir() {
				log.Warnf("Skipping unreadable directory %s: %v", path, err)
				return filepath.SkipDir
			}
			return err
		}

		// Skip .terragrunt-stack directories (generated by `terragrunt stack generate`)
		// when stack support is enabled. With stacks disabled, behavior is left untouched.
		if enableStacks && info.IsDir() && (filepath.Base(path) == ".terragrunt-stack" || strings.Contains(path, "/.terragrunt-stack/") || strings.Contains(path, "\\.terragrunt-stack\\")) {
			return filepath.SkipDir
		}

		// Never walk into .terragrunt-cache: it holds *copies* of configs
		// (incl. generated root.hcl) that would otherwise spawn duplicate,
		// garbage projects (upstream issue #434). Applies regardless of the
		// stacks toggle, matching the terragrunt library's own behavior.
		if info.IsDir() && (filepath.Base(path) == ".terragrunt-cache" || strings.Contains(path, "/.terragrunt-cache/") || strings.Contains(path, "\\.terragrunt-cache\\")) {
			return filepath.SkipDir
		}

		if !info.IsDir() {
			return nil
		}

		for _, configFile := range []string{"root.hcl"} {
			if !filepath.IsAbs(configFile) {
				configFile = joinPath(path, configFile)
			}

			if !isDir(configFile) && fileExists(configFile) {
				configFiles = append(configFiles, configFile)
				break
			}
		}

		return nil
	})

	nestedConfigFiles, err := config.FindConfigFilesInPath(rootPath, opts)
	if err != nil {
		// The terragrunt library's own walk aborts entirely on unreadable
		// directories (e.g. stale root-owned .terragrunt-cache left behind by
		// container runs). Falling back to a tolerant walk here keeps a
		// single bad directory from silently producing an empty config.
		log.Warnf("terragrunt config discovery failed (%v), falling back to tolerant walk", err)
		_ = walkFunc(rootPath, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				if info != nil && info.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			if info.IsDir() {
				switch info.Name() {
				case ".git", ".terragrunt-stack", ".terragrunt-cache":
					return filepath.SkipDir
				}
				return nil
			}
			if info.Name() == "terragrunt.hcl" {
				configFiles = append(configFiles, path)
			}
			return nil
		})
		return configFiles, nil
	}
	for _, nestedFile := range nestedConfigFiles {
		// Skip files in .terragrunt-stack directories when stack support is enabled
		if enableStacks && (strings.Contains(nestedFile, "/.terragrunt-stack/") || strings.Contains(nestedFile, "\\.terragrunt-stack\\")) {
			continue
		}
		configFiles = append(configFiles, nestedFile)
	}
	return configFiles, nil
}

// Finds the absolute paths of all arbitrary project hcl files
func getAllTerragruntProjectHclFiles() map[string][]string {
	projectHclFiles := projectHclFiles
	orderedHclFilePaths := map[string][]string{}
	uniqueHclFileAbsPaths := map[string][]string{}
	for _, projectHclFile := range projectHclFiles {
		err := filepath.Walk(gitRoot, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}

			if !info.IsDir() && info.Name() == projectHclFile {
				orderedHclFilePaths[projectHclFile] = append(orderedHclFilePaths[projectHclFile], filepath.Dir(path))
			}

			return nil
		})

		if err != nil {
			log.Fatal(err)
		}

		for _, uniquePath := range orderedHclFilePaths[projectHclFile] {
			uniqueAbsPath, err := filepath.Abs(uniquePath)
			if err != nil {
				return nil
			}
			uniqueHclFileAbsPaths[projectHclFile] = append(uniqueHclFileAbsPaths[projectHclFile], uniqueAbsPath)
		}
	}
	return uniqueHclFileAbsPaths
}

func main(cmd *cobra.Command, args []string) error {
	// Fresh run: drop dependency-block edges recorded by a previous generate
	// in the same process (matters for tests and library/embedded use)
	resetDepGraph()

	// Ensure the gitRoot has a trailing slash and is an absolute path
	absoluteGitRoot, err := filepath.Abs(gitRoot)
	if err != nil {
		return err
	}
	gitRoot = absoluteGitRoot + string(filepath.Separator)
	workingDirs := []string{gitRoot}
	projectHclDirMap := map[string][]string{}
	var projectHclDirs []string
	if len(projectHclFiles) > 0 {
		workingDirs = nil
		// map [project-hcl-file] => directories containing project-hcl-file
		projectHclDirMap = getAllTerragruntProjectHclFiles()
		for _, projectHclFile := range projectHclFiles {
			projectHclDirs = append(projectHclDirs, projectHclDirMap[projectHclFile]...)
			workingDirs = append(workingDirs, projectHclDirMap[projectHclFile]...)
		}
		// parse terragrunt child modules outside the scope of projectHclDirs
		if createHclProjectExternalChilds {
			workingDirs = append(workingDirs, gitRoot)
		}
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

	resolvedEngine := resolveEngine(gitRoot)
	var failedProjects atomic.Int64
	if resolvedEngine == engineCLI {
		cliProjects, err := generateProjectsWithCLIEngine(gitRoot)
		if err != nil {
			return err
		}

		if preserveProjects {
			// Same update-in-place semantics as the library engine: projects
			// that already exist are refreshed by dir, new ones appended.
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
	} else {

		lock := sync.Mutex{}
		ctx := context.Background()
		errGroup, _ := errgroup.WithContext(ctx)
		sem := semaphore.NewWeighted(numExecutors)

		// Initialize stack manager early if stacks are enabled (needed for filtering)
		var stackMgr *StackManager
		var discoveredStacks []Stack
		if enableStacks {
			definitionFile := stackDefinitionFile
			if definitionFile != "" && !filepath.IsAbs(definitionFile) {
				definitionFile = filepath.Join(gitRoot, definitionFile)
			}

			stackMgr = NewStackManager(StackManagerConfig{
				GitRoot:                      gitRoot,
				DefinitionFile:               definitionFile,
				StackWorkflow:                stackWorkflow,
				DefaultWorkflow:              defaultWorkflow,
				CreateProjectName:            createProjectName,
				CreateWorkspace:              createWorkspace,
				AutoPlan:                     autoPlan,
				DefaultTerraformVersion:      defaultTerraformVersion,
				DefaultTerraformDistribution: defaultTerraformDistribution,
			})

			// Discover stacks early so we can filter modules
			var err error
			discoveredStacks, err = stackMgr.DiscoverStacks()
			if err != nil {
				log.Warnf("Failed to discover stacks: %v", err)
			} else if len(discoveredStacks) > 0 {
				log.Infof("Discovered %d stack(s)", len(discoveredStacks))

				// Get all terragrunt files to assign modules to stacks
				allTerragruntFiles, err := getAllTerragruntFiles(gitRoot)
				if err == nil {
					// Convert to relative paths
					modulePaths := []string{}
					for _, tfPath := range allTerragruntFiles {
						relPath, err := filepath.Rel(gitRoot, tfPath)
						if err == nil {
							modulePaths = append(modulePaths, filepath.ToSlash(relPath))
						}
					}

					// Assign modules to stacks
					_, err = stackMgr.AssignModulesToStacks(modulePaths)
					if err != nil {
						log.Warnf("Failed to assign modules to stacks: %v", err)
					}
				}
			}
		}

		for _, workingDir := range workingDirs {
			terragruntFiles, err := getAllTerragruntFiles(workingDir)
			if err != nil {
				return err
			}

			if len(projectHclDirs) == 0 || createHclProjectChilds || (createHclProjectExternalChilds && workingDir == gitRoot) {
				// Concurrently looking all dependencies
				for _, terragruntPath := range terragruntFiles {
					terragruntPath := terragruntPath // https://golang.org/doc/faq#closures_and_goroutines

					// don't create atlantis projects already covered by project hcl file projects
					skipProject := false
					if createHclProjectExternalChilds && workingDir == gitRoot && len(projectHclDirs) > 0 {
						for _, projectHclDir := range projectHclDirs {
							if strings.HasPrefix(terragruntPath, projectHclDir) {
								skipProject = true
								break
							}
						}
					}

					// Skip modules that belong to stacks (they will be handled by stack projects)
					if enableStacks && stackMgr != nil {
						relPath, err := filepath.Rel(gitRoot, terragruntPath)
						if err == nil {
							relPath = filepath.ToSlash(relPath)
							stacks := stackMgr.GetStackForModule(relPath)
							if len(stacks) > 0 {
								skipProject = true
								log.Debugf("Skipping regular project for %s (belongs to stack(s): %v)", relPath, stacks)
							} else if stackMgr.IsStackOwnedDir(relPath) {
								skipProject = true
								log.Debugf("Skipping regular project %s (generated content inside stack dir)", relPath)
							} else if stackMgr.IsStackSourceDir(relPath) {
								skipProject = true
								log.Debugf("Skipping regular project for %s (unit source catalog of a stack)", relPath)
							}
						}
					}

					if skipProject {
						continue
					}
					if err := sem.Acquire(ctx, 1); err != nil {
						return err
					}

					errGroup.Go(func() error {
						defer sem.Release(1)
						project, err := createProject(ctx, terragruntPath)
						if err != nil {
							// Our own locals-annotation errors stay fatal;
							// terragrunt-side eval failures degrade to a skipped
							// project + warning, so one bad leaf can't sink a
							// 400-module monorepo.
							if isMarkerError(err) {
								return err
							}
							log.Warnf("Skipping %s: %v", terragruntPath, err)
							failedProjects.Add(1)
							return nil
						}
						// if project and err are nil then skip this project
						if err == nil && project == nil {
							return nil
						}

						// Lock the list as only one goroutine should be writing to config.Projects at a time
						lock.Lock()
						defer lock.Unlock()

						// When preserving existing projects, we should update existing blocks instead of creating a
						// duplicate, when generating something which already has representation
						if preserveProjects {
							updateProject := false

							// TODO: with Go 1.19, we can replace for loop with slices.IndexFunc for increased performance
							for i := range config.Projects {
								if config.Projects[i].Dir == project.Dir {
									updateProject = true
									log.Info("Updated project for ", terragruntPath)
									config.Projects[i] = *project

									// projects should be unique, let's exit for loop for performance
									// once first occurrence is found and replaced
									break
								}
							}

							if !updateProject {
								log.Info("Created project for ", terragruntPath)
								config.Projects = append(config.Projects, *project)
							}
						} else {
							log.Info("Created project for ", terragruntPath)
							config.Projects = append(config.Projects, *project)
						}

						return nil
					})
				}

				if err := errGroup.Wait(); err != nil {
					return err
				}
			}
			if len(projectHclDirs) > 0 && workingDir != gitRoot {
				projectHcl := lookupProjectHcl(projectHclDirMap, workingDir)
				err := sem.Acquire(ctx, 1)
				if err != nil {
					return err
				}

				errGroup.Go(func() error {
					defer sem.Release(1)
					project, err := createHclProject(ctx, terragruntFiles, workingDir, projectHcl)
					if err != nil {
						return err
					}
					// if project and err are nil then skip this project
					if err == nil && project == nil {
						return nil
					}
					// Lock the list as only one goroutine should be writing to config.Projects at a time
					lock.Lock()
					defer lock.Unlock()

					log.Info("Created "+projectHcl+" project for ", workingDir)
					config.Projects = append(config.Projects, *project)

					return nil
				})

				if err := errGroup.Wait(); err != nil {
					return err
				}
			}
		}

		// Generate stack projects if enabled
		if enableStacks && stackMgr != nil && len(discoveredStacks) > 0 {
			// Generate projects for each stack (reuse stacks from earlier discovery)
			for _, stack := range discoveredStacks {
				stackProject, err := stackMgr.GenerateStackProject(stack)
				if err != nil {
					log.Warnf("Failed to generate project for stack %s: %v", stack.Name, err)
					continue
				}

				if stackProject != nil {
					// Check if project already exists (by Dir)
					projectExists := false
					if preserveProjects {
						for i := range config.Projects {
							if config.Projects[i].Dir == stackProject.Dir {
								log.Infof("Updated stack project for %s", stackProject.Dir)
								config.Projects[i] = *stackProject
								projectExists = true
								break
							}
						}
					}

					if !projectExists {
						log.Infof("Created stack project for %s", stackProject.Dir)
						config.Projects = append(config.Projects, *stackProject)
					}
				}
			}
		}

	} // end library engine discovery

	// When everything we discovered failed, generation was meaningless:
	// report it rather than emitting an empty config that looks healthy.
	if failedProjects.Load() > 0 && len(config.Projects) == 0 {
		return fmt.Errorf("generation failed: every discovered module (%d) errored; see warnings above", failedProjects.Load())
	}

	if gitFilter != "" {
		filtered, err := filterProjectsByGitDiff(config.Projects, gitRoot, gitFilter)
		if err != nil {
			return err
		}
		log.Infof("--filter-git: kept %d of %d projects touched by %s...HEAD", len(filtered), len(config.Projects), gitFilter)
		config.Projects = filtered
	}

	// Sort the projects in config by Dir
	sort.Slice(config.Projects, func(i, j int) bool { return config.Projects[i].Dir < config.Projects[j].Dir })

	// The cli engine already assigned ordering from exact dependency edges;
	// the library engine reconstructs below from watch entries.
	if (executionOrderGroups || dependsOn) && resolvedEngine != engineCLI {
		projectsMap := make(map[string]*AtlantisProject, len(config.Projects))
		for i := range config.Projects {
			projectsMap[config.Projects[i].Dir] = &config.Projects[i]
		}

		// Compute order groups in the cycle to avoid incorrect values in cascade dependencies
		hasChanges := true
		for i := 0; hasChanges && i <= len(config.Projects); i++ {
			hasChanges = false
			for _, project := range config.Projects {
				executionOrderGroup := 0
				dependsOnList := []string{}
				// A project can reference the same dependency through several
				// when_modified entries (dependency block + var files living
				// in the dependency's dir, cascades); depends_on must list
				// each project once, keeping first-seen order for stability.
				dependsOnSeen := map[string]bool{}
				// choose order group based on dependencies; orderingInputs()
				// also merges in dependency-block edges that
				// --ignore-dependency-blocks removed from the watch list
				for _, dep := range orderingInputs(project) {
					depPath := filepath.ToSlash(filepath.Dir(filepath.Join(project.Dir, dep)))
					if depPath == project.Dir {
						// skip dependency on oneself
						continue
					}

					depProject, ok := projectsMap[depPath]
					if !ok {
						// skip not project dependencies
						continue
					}
					if depProject.ExecutionOrderGroup != nil {
						if *depProject.ExecutionOrderGroup+1 > executionOrderGroup {
							executionOrderGroup = *depProject.ExecutionOrderGroup + 1
						}
					}
					if !dependsOnSeen[depProject.Name] {
						dependsOnSeen[depProject.Name] = true
						dependsOnList = append(dependsOnList, depProject.Name)
					}
				}
				if projectsMap[project.Dir].ExecutionOrderGroup == nil || *projectsMap[project.Dir].ExecutionOrderGroup != executionOrderGroup {
					if executionOrderGroups {
						projectsMap[project.Dir].ExecutionOrderGroup = &executionOrderGroup
					}
					if dependsOn {
						projectsMap[project.Dir].DependsOn = dependsOnList
					}
					// repeat the main cycle when changed some project
					hasChanges = true
				}
			}
		}

		if hasChanges {
			// The fixed-point loop did not converge: the project graph has a
			// cycle. Name the cycle so the user can fix the dependency layout
			// instead of staring at a generic warning (upstream issue #191).
			if cycle := findProjectCycle(config.Projects); len(cycle) > 0 {
				log.Warnf("Computing execution_order_groups failed: dependency cycle detected between projects: %s", formatProjectCycle(cycle))
			} else {
				log.Warn("Computing execution_order_groups failed. Probably cycle exists")
			}
		}

		// Sort by execution_order_group
		if executionOrderGroups {
			sort.Slice(config.Projects, func(i, j int) bool {
				if *config.Projects[i].ExecutionOrderGroup == *config.Projects[j].ExecutionOrderGroup {
					return config.Projects[i].Dir < config.Projects[j].Dir
				}
				return *config.Projects[i].ExecutionOrderGroup < *config.Projects[j].ExecutionOrderGroup
			})
		}
	}

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
var ignoreParentTerragrunt bool
var createParentProject bool
var ignoreDependencyBlocks bool
var parallel bool
var deleteSourceBranchOnMerge bool
var createWorkspace bool
var createProjectName bool
var defaultTerraformVersion string
var defaultTerraformDistribution string
var defaultWorkflow string
var stackWorkflow string
var stackDefinitionFile string
var enableStacks bool

// projectNameRegex sanitizes directory or stack names into valid Atlantis
// project names and workspace names.
var projectNameRegex = regexp.MustCompile(`[^a-zA-Z0-9_-]+`)
var filterPaths []string
var gitFilter string
var outputPath string
var preserveWorkflows bool
var preserveProjects bool
var cascadeDependencies bool
var defaultApplyRequirements []string
var numExecutors int64
var projectHclFiles []string
var createHclProjectChilds bool
var createHclProjectExternalChilds bool
var useProjectMarkers bool
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
	generateCmd.PersistentFlags().BoolVar(&ignoreParentTerragrunt, "ignore-parent-terragrunt", true, "Ignore parent terragrunt configs (those which don't reference a terraform module). Default is enabled")
	generateCmd.PersistentFlags().BoolVar(&createParentProject, "create-parent-project", false, "Create a project for the parent terragrunt configs (those which don't reference a terraform module). Default is disabled")
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
	generateCmd.PersistentFlags().StringVar(&gitFilter, "filter-git", "", "Only include projects whose autoplan triggers were touched between the given git ref and HEAD (e.g. origin/main). Works with both engines.")
	generateCmd.PersistentFlags().StringVar(&gitRoot, "root", pwd, "Path to the root directory of the git repo you want to build config for. Default is current dir")
	generateCmd.PersistentFlags().StringVar(&defaultTerraformVersion, "terraform-version", "", "Default terraform version to specify for all modules. Can be overriden by locals")
	generateCmd.PersistentFlags().StringVar(&defaultTerraformDistribution, "terraform-distribution", "", "Default terraform distribution to specify for all modules (e.g. 'tofu'). Can be overriden by the atlantis_terraform_distribution locals")
	generateCmd.PersistentFlags().Int64Var(&numExecutors, "num-executors", 15, "Number of executors used for parallel generation of projects. Default is 15")
	generateCmd.PersistentFlags().StringSliceVar(&projectHclFiles, "project-hcl-files", []string{}, "Comma-separated names of arbitrary hcl files in the terragrunt hierarchy to create Atlantis projects for. Disables the --filter flag")
	generateCmd.PersistentFlags().BoolVar(&createHclProjectChilds, "create-hcl-project-childs", false, "Creates Atlantis projects for terragrunt child modules below the directories containing the HCL files defined in --project-hcl-files")
	generateCmd.PersistentFlags().BoolVar(&createHclProjectExternalChilds, "create-hcl-project-external-childs", true, "Creates Atlantis projects for terragrunt child modules outside the directories containing the HCL files defined in --project-hcl-files")
	generateCmd.PersistentFlags().BoolVar(&useProjectMarkers, "use-project-markers", false, "Creates Atlantis projects only for project hcl files with locals: atlantis_project = true")
	generateCmd.PersistentFlags().BoolVar(&executionOrderGroups, "execution-order-groups", false, "Computes execution_order_groups for projects")
	generateCmd.PersistentFlags().BoolVar(&dependsOn, "depends-on", false, "Computes depends_on for projects. Requires --create-project-name.")
	generateCmd.PersistentFlags().BoolVar(&enableStacks, "enable-stacks", false, "Enable Terragrunt stack discovery and generation. Stacks are discovered from terragrunt.stack.hcl files and, optionally, from the file given by --stack-definition-file")
	generateCmd.PersistentFlags().StringVar(&engine, "engine", engineAuto, "Parsing engine: 'cli' discovers via the terragrunt binary (required for terragrunt v1.x), 'library' uses the embedded terragrunt parser, 'auto' picks cli when terragrunt v1+ is installed and library otherwise")
	generateCmd.PersistentFlags().StringVar(&stackWorkflow, "stack-workflow", "", "Default workflow name for stack projects. If not set, uses the value from --workflow flag or stack configuration")
	generateCmd.PersistentFlags().StringVar(&stackDefinitionFile, "stack-definition-file", "", "Path to a YAML/JSON file defining additional stacks (relative to --root when not absolute). Only used together with --enable-stacks")
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
