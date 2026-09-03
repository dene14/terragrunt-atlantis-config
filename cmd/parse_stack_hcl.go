package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/gruntwork-io/go-commons/errors"
	"github.com/gruntwork-io/terragrunt/config"
	"github.com/gruntwork-io/terragrunt/config/hclparse"
	"github.com/gruntwork-io/terragrunt/options"
	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/gohcl"
	log "github.com/sirupsen/logrus"
	"github.com/zclconf/go-cty/cty"
	"github.com/zclconf/go-cty/cty/function"
)

// UnitBlock represents a unit block in terragrunt.stack.hcl
// Per Terragrunt docs, `source` is where the unit configuration comes from
// (often a remote git ref or a local catalog directory) and `path` is the
// directory (relative to the stack file) the unit gets generated into by
// `terragrunt stack generate`.
type UnitBlock struct {
	Name   string   `hcl:"name,label"`
	Source *string  `hcl:"source,attr"`
	Path   *string  `hcl:"path,attr"`
	Remain hcl.Body `hcl:",remain"`
}

// StackBlock represents a (nested) stack block in terragrunt.stack.hcl.
// In real Terragrunt stacks, `stack` blocks define nested stacks with their
// own `source` and `path`; `description` is not an official attribute but is
// tolerated for convenience.
type StackBlock struct {
	Name        string   `hcl:"name,label"`
	Source      *string  `hcl:"source,attr"`
	Path        *string  `hcl:"path,attr"`
	Description *string  `hcl:"description,attr"`
	Remain      hcl.Body `hcl:",remain"`
}

// ParsedStackHcl represents the parsed contents of a terragrunt.stack.hcl file
type ParsedStackHcl struct {
	Units  []UnitBlock  `hcl:"unit,block"`
	Stacks []StackBlock `hcl:"stack,block"`
	Remain hcl.Body     `hcl:",remain"`
}

// StackHclDefinition represents a complete stack definition from HCL
type StackHclDefinition struct {
	FilePath string
	Units    []UnitBlock
	// Stacks are the nested stack blocks declared inside the file.
	Stacks []StackBlock
}

// anchoredUnitWatchPaths collects paths discovered while parsing a unit config
type anchoredUnitWatchPaths struct {
	includes []string // absolute file paths of the include chain
	deps     []string // absolute dirs referenced by dependency blocks
	tfDirs   []string // absolute dirs of local terraform module sources
}

// parseAnchoredUnitConfig reads sourceFile (a unit's terragrunt.hcl) but
// evaluates it as if it lived at anchorFile — the unit's generated location
// inside the stack — matching Terragrunt's runtime semantics after
// `terragrunt stack generate`. Include blocks are followed recursively with
// the same anchor (Terragrunt merges included config into the child).
//
// Attributes are evaluated one by one: evaluation uses a full Terragrunt
// context, but functions that depend on the config file's location existing
// on disk (get_repo_root, get_path_from_repo_root) are overridden with
// values derived from gitRoot — the anchor does not exist before
// `terragrunt stack generate` runs. Attributes that still fail to evaluate
// fall back to literal strings or are skipped.
func parseAnchoredUnitConfig(sourceFile, anchorFile, gitRoot string, seen map[string]bool, out *anchoredUnitWatchPaths) {
	if seen[sourceFile] {
		return
	}
	seen[sourceFile] = true

	configString, err := os.ReadFile(sourceFile)
	if err != nil {
		log.Debugf("Stack unit config %s not readable: %v", sourceFile, err)
		return
	}

	parser := hclparse.NewParser()
	file, err := parseHclForStack(parser, string(configString), sourceFile)
	if err != nil {
		log.Debugf("Failed to parse stack unit config %s: %v", sourceFile, err)
		return
	}

	anchorDir := filepath.Dir(anchorFile)

	// Build the anchored Terragrunt evaluation context
	var evalContext *hcl.EvalContext
	terragruntOptions, optsErr := options.NewTerragruntOptionsWithConfigPath(anchorFile)
	if optsErr == nil {
		terragruntOptions.OriginalTerragruntConfigPath = anchorFile
		terragruntOptions.Env = getEnvs()
		ctx := config.NewParsingContext(context.Background(), terragruntOptions)
		evalContext, err = createTerragruntEvalContext(ctx, anchorFile)
		if err != nil {
			log.Debugf("Failed to create eval context for %s: %v", sourceFile, err)
			evalContext = nil
		} else {
			overrideRepoRootFunctions(evalContext, gitRoot, anchorDir)
		}
	}

	evalStringAttr := func(body hcl.Body, attrName string) *string {
		attrs, diags := body.JustAttributes()
		if diags != nil && diags.HasErrors() {
			return nil
		}
		attr, ok := attrs[attrName]
		if !ok {
			return nil
		}
		if evalContext != nil {
			if val, diags := attr.Expr.Value(evalContext); !diags.HasErrors() && val.Type() == cty.String {
				s := val.AsString()
				return &s
			}
		}
		// Literal fallback
		if val, diags := attr.Expr.Value(nil); !diags.HasErrors() && val.Type() == cty.String {
			s := val.AsString()
			return &s
		}
		return nil
	}

	schema := &hcl.BodySchema{
		Blocks: []hcl.BlockHeaderSchema{
			{Type: "include", LabelNames: []string{"name"}},
			{Type: "dependency", LabelNames: []string{"name"}},
			{Type: "terraform"},
		},
	}
	content, _, diags := file.Body.PartialContent(schema)
	if diags != nil && diags.HasErrors() {
		log.Debugf("Failed to decode blocks of stack unit config %s: %v", sourceFile, diags)
		return
	}

	resolve := func(p string) string {
		if !filepath.IsAbs(p) {
			p = filepath.Join(anchorDir, p)
		}
		return filepath.Clean(p)
	}

	for _, block := range content.Blocks {
		switch block.Type {
		case "include":
			if path := evalStringAttr(block.Body, "path"); path != nil {
				includeFile := resolve(*path)
				if _, err := os.Stat(includeFile); err == nil {
					out.includes = append(out.includes, includeFile)
					parseAnchoredUnitConfig(includeFile, anchorFile, gitRoot, seen, out)
				}
			}
		case "dependency":
			if configPath := evalStringAttr(block.Body, "config_path"); configPath != nil {
				out.deps = append(out.deps, resolve(*configPath))
			}
		case "terraform":
			if source := evalStringAttr(block.Body, "source"); source != nil {
				sourceDir := resolve(*source)
				if info, err := os.Stat(sourceDir); err == nil && info.IsDir() {
					out.tfDirs = append(out.tfDirs, sourceDir)
				}
			}
		}
	}
}

// staticStringFunc returns an HCL function producing a fixed string.
func staticStringFunc(value string) function.Function {
	return function.New(&function.Spec{
		Params: []function.Parameter{},
		Type:   function.StaticReturnType(cty.String),
		Impl:   func(args []cty.Value, retType cty.Type) (cty.Value, error) { return cty.StringVal(value), nil },
	})
}

// overrideRepoRootFunctions replaces evaluation-context functions that exec
// `git` inside the configuration's directory. The directory of a generated
// unit does not exist until `terragrunt stack generate` runs, and in
// containers git may refuse to operate on a checkout owned by another user
// (safe.directory) — the generator cannot rely on git at all. Repo-relative
// results are known statically because the anchor is always inside gitRoot.
func overrideRepoRootFunctions(evalContext *hcl.EvalContext, gitRoot, anchorDir string) {
	if evalContext == nil || evalContext.Functions == nil {
		return
	}
	cleanGitRoot := filepath.Clean(gitRoot)
	if _, ok := evalContext.Functions["get_repo_root"]; ok {
		evalContext.Functions["get_repo_root"] = staticStringFunc(cleanGitRoot)
	}
	if _, ok := evalContext.Functions["get_path_from_repo_root"]; ok {
		if anchorRel, err := filepath.Rel(cleanGitRoot, anchorDir); err == nil && !strings.HasPrefix(anchorRel, "..") {
			evalContext.Functions["get_path_from_repo_root"] = staticStringFunc(filepath.ToSlash(anchorRel))
		}
	}
}

// ParseStackHclFile reads and parses a terragrunt.stack.hcl file.
//
// The file is first decoded with a full Terragrunt evaluation context so that
// functions like find_in_parent_folders() work. Real-world stack files often
// reference files or functions that are not resolvable at generation time
// (e.g. remote sources, missing parent files); in that case we fall back to a
// literal decoding pass that only extracts statically-evaluable attributes,
// instead of failing the whole file.
func ParseStackHclFile(path string, ctx *config.ParsingContext, gitRoot string) (*StackHclDefinition, error) {
	// Check if file exists
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil, fmt.Errorf("stack HCL file not found: %s", path)
	}

	// Read file contents
	configString, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read stack HCL file: %w", err)
	}

	// Parse HCL
	parser := hclparse.NewParser()
	file, err := parseHclForStack(parser, string(configString), path)
	if err != nil {
		return nil, fmt.Errorf("failed to parse stack HCL file: %w", err)
	}

	parsed := ParsedStackHcl{}
	evaluated := false

	// Try decoding with a full Terragrunt evaluation context first
	evalContext, evalCtxErr := createTerragruntEvalContext(ctx, path)
	if evalCtxErr == nil {
		overrideRepoRootFunctions(evalContext, gitRoot, filepath.Dir(path))
		decodeDiagnostics := gohcl.DecodeBody(file.Body, evalContext, &parsed)
		if decodeDiagnostics != nil && decodeDiagnostics.HasErrors() {
			log.Debugf("Failed to evaluate stack HCL file %s with Terragrunt context (%v), falling back to literal parsing", path, decodeDiagnostics)
		} else {
			evaluated = true
		}
	} else {
		log.Debugf("Failed to create eval context for stack HCL file %s (%v), falling back to literal parsing", path, evalCtxErr)
	}

	if !evaluated {
		parsed = ParsedStackHcl{}
		if err := parseStackHclLiteral(file, &parsed); err != nil {
			return nil, fmt.Errorf("failed to decode stack HCL file: %w", err)
		}
	}

	return &StackHclDefinition{
		FilePath: path,
		Units:    parsed.Units,
		Stacks:   parsed.Stacks,
	}, nil
}

// parseStackHclLiteral extracts unit/nested-stack blocks from an already parsed
// HCL file without evaluating any functions or variables. Attributes that do
// not evaluate to a static string are silently skipped (set to nil).
func parseStackHclLiteral(file *hcl.File, parsed *ParsedStackHcl) error {
	schema := &hcl.BodySchema{
		Blocks: []hcl.BlockHeaderSchema{
			{Type: "unit", LabelNames: []string{"name"}},
			{Type: "stack", LabelNames: []string{"name"}},
		},
	}

	content, _, diags := file.Body.PartialContent(schema)
	if diags != nil && diags.HasErrors() {
		return diags
	}

	literalString := func(body hcl.Body, attrName string) *string {
		attrs, diags := body.JustAttributes()
		if diags != nil && diags.HasErrors() {
			return nil
		}
		attr, ok := attrs[attrName]
		if !ok {
			return nil
		}
		val, diags := attr.Expr.Value(nil)
		if diags != nil && diags.HasErrors() {
			return nil
		}
		if val.Type() != cty.String {
			return nil
		}
		s := val.AsString()
		return &s
	}

	for _, block := range content.Blocks {
		if len(block.Labels) == 0 {
			continue
		}
		switch block.Type {
		case "unit":
			parsed.Units = append(parsed.Units, UnitBlock{
				Name:   block.Labels[0],
				Source: literalString(block.Body, "source"),
				Path:   literalString(block.Body, "path"),
			})
		case "stack":
			parsed.Stacks = append(parsed.Stacks, StackBlock{
				Name:        block.Labels[0],
				Source:      literalString(block.Body, "source"),
				Path:        literalString(block.Body, "path"),
				Description: literalString(block.Body, "description"),
			})
		}
	}

	return nil
}

// parseHclForStack is a wrapper around HCL parsing for stack files
func parseHclForStack(parser *hclparse.Parser, hcl string, filename string) (*hcl.File, error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err := errors.WithStackTrace(hclparse.PanicWhileParsingConfigError{RecoveredValue: recovered, ConfigFile: filename})
			log.Errorf("Panic while parsing stack HCL: %v", err)
		}
	}()

	if filepath.Ext(filename) == ".json" {
		file, parseDiagnostics := parser.ParseJSON([]byte(hcl), filename)
		if parseDiagnostics != nil && parseDiagnostics.HasErrors() {
			return nil, parseDiagnostics
		}
		return file, nil
	}

	file, parseDiagnostics := parser.ParseHCL([]byte(hcl), filename)
	if parseDiagnostics != nil && parseDiagnostics.HasErrors() {
		return nil, parseDiagnostics
	}

	return file, nil
}

// FindStackHclFiles searches for terragrunt.stack.hcl files in the given root directory.
// VCS metadata and Terragrunt-generated directories (.terragrunt-stack,
// .terragrunt-cache) are excluded from the search.
func FindStackHclFiles(rootDir string) ([]string, error) {
	var stackFiles []string

	err := filepath.Walk(rootDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			// Unreadable directories (e.g. stale root-owned .terragrunt-cache)
			// must not abort stack discovery
			if info != nil && info.IsDir() {
				log.Warnf("Skipping unreadable directory %s: %v", path, err)
				return filepath.SkipDir
			}
			return err
		}

		if info.IsDir() {
			switch info.Name() {
			case ".git", ".terragrunt-stack", ".terragrunt-cache":
				return filepath.SkipDir
			}
			return nil
		}

		// Check for terragrunt.stack.hcl files
		if info.Name() == "terragrunt.stack.hcl" {
			stackFiles = append(stackFiles, path)
		}

		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to search for stack HCL files: %w", err)
	}

	return stackFiles, nil
}

// ConvertStackHclToStacks converts parsed HCL stack definitions to internal Stack structs.
//
// For every unit (and nested stack) we record two kinds of directories,
// relative to gitRoot:
//   - Modules: directories at the unit's `path` that contain a terragrunt.hcl
//     on disk. These are treated as stack members and do not get individual
//     Atlantis projects.
//   - UnitSources: directories referenced through a unit's local `source`
//     (e.g. a catalog directory such as ../../units/vpc). These are added to
//     the stack project's autoplan when_modified patterns. Remote sources
//     cannot be watched and are skipped.
func ConvertStackHclToStacks(definitions []StackHclDefinition, gitRoot string) []Stack {
	stacks := []Stack{}
	cleanGitRoot := filepath.Clean(gitRoot)

	addUnique := func(list *[]string, entry string) {
		for _, e := range *list {
			if e == entry {
				return
			}
		}
		*list = append(*list, entry)
	}

	// relDirInsideRepo converts an absolute path to a slash-separated path
	// relative to gitRoot. Returns false when the path is outside the repo.
	relDirInsideRepo := func(absPath string) (string, bool) {
		rel, err := filepath.Rel(cleanGitRoot, absPath)
		if err != nil {
			return "", false
		}
		if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return "", false
		}
		return filepath.ToSlash(rel), true
	}

	for _, def := range definitions {
		stackDir := filepath.Dir(def.FilePath)

		relStackDir, err := filepath.Rel(cleanGitRoot, stackDir)
		if err != nil {
			relStackDir = stackDir
		}
		relStackDir = filepath.ToSlash(relStackDir)

		relStackFile, err := filepath.Rel(cleanGitRoot, def.FilePath)
		if err != nil {
			relStackFile = def.FilePath
		}

		members := []string{}
		unitSources := []string{}

		// processBlock records member and/or source directories for a unit or
		// nested stack block.
		processBlock := func(source, path *string) {
			if path != nil {
				unitDir := filepath.Join(stackDir, *path)
				if _, err := os.Stat(filepath.Join(unitDir, "terragrunt.hcl")); err == nil {
					if rel, ok := relDirInsideRepo(unitDir); ok {
						addUnique(&members, rel)
					}
				}
			}
			// A source is only watched when it resolves to an existing local
			// directory inside the repo. Remote sources (git refs, registry,
			// etc.) never resolve locally and are skipped.
			if source != nil {
				sourceDir := *source
				if !filepath.IsAbs(sourceDir) {
					sourceDir = filepath.Join(stackDir, sourceDir)
				}
				if info, err := os.Stat(sourceDir); err == nil && info.IsDir() {
					if rel, ok := relDirInsideRepo(sourceDir); ok {
						addUnique(&unitSources, rel)
					}
				}
			}
		}

		for _, unit := range def.Units {
			processBlock(unit.Source, unit.Path)
		}
		for _, nested := range def.Stacks {
			processBlock(nested.Source, nested.Path)
		}

		// Use an optional description from the first stack block that declares one
		description := ""
		for _, nested := range def.Stacks {
			if nested.Description != nil {
				description = *nested.Description
				break
			}
		}

		stacks = append(stacks, Stack{
			Name:         relStackDir,
			Description:  description,
			Modules:      members,
			UnitSources:  unitSources,
			Dependencies: []string{},
			Source:       filepath.ToSlash(relStackFile),
		})
	}

	return stacks
}

// safeGetDependencies wraps getDependencies so a broken dependency (parse
// failure or library panic, e.g. in environments where git cannot operate on
// the checkout) degrades to an error instead of aborting generation.
func safeGetDependencies(ctx *config.ParsingContext, path string) (deps []string, err error) {
	defer func() {
		if r := recover(); r != nil {
			log.Debugf("Recovered panic while resolving dependencies of %s: %v", path, r)
			deps, err = nil, fmt.Errorf("panic while resolving dependencies of %s: %v", path, r)
		}
	}()
	return getDependencies(ctx, path)
}

// EnrichStackWithUnitDetails inspects the units of a stack and fills in
// ExtraWatchPaths so the stack project tracks exactly what the units would
// track if they were standalone projects:
//
//   - the include chain of every unit config (evaluated from the unit's
//     generated location, matching `terragrunt stack run` semantics)
//   - dependency blocks: dependencies on sibling units of the same stack are
//     skipped (terragrunt orders them within the stack run); dependencies
//     pointing elsewhere are recorded, and — with cascade enabled — their
//     own dependencies are recorded too
//   - local terraform module sources referenced by unit configs
func EnrichStackWithUnitDetails(stack *Stack, def StackHclDefinition, gitRoot string) {
	stackDir := filepath.Dir(def.FilePath)

	// Generated (or existing) directories of the stack's own units
	unitDirs := map[string]bool{}
	for _, unit := range def.Units {
		if unit.Path != nil {
			unitDirs[filepath.Clean(filepath.Join(stackDir, *unit.Path))] = true
		}
	}

	extra := []string{}
	addUnique := func(entry string) {
		entry = filepath.ToSlash(entry)
		for _, e := range extra {
			if e == entry {
				return
			}
		}
		extra = append(extra, entry)
	}

	for _, unit := range def.Units {
		if unit.Path == nil {
			continue
		}
		unitDir := filepath.Clean(filepath.Join(stackDir, *unit.Path))
		anchorFile := filepath.Join(unitDir, "terragrunt.hcl")

		// The config file to read: the live unit dir if it exists,
		// otherwise the unit's source directory (catalog).
		configFile := anchorFile
		if _, err := os.Stat(configFile); err != nil {
			if unit.Source == nil {
				continue
			}
			sourceDir := *unit.Source
			if !filepath.IsAbs(sourceDir) {
				sourceDir = filepath.Join(stackDir, sourceDir)
			}
			configFile = filepath.Join(sourceDir, "terragrunt.hcl")
			if _, err := os.Stat(configFile); err != nil {
				continue
			}
		}

		paths := anchoredUnitWatchPaths{}
		parseAnchoredUnitConfig(filepath.Clean(configFile), anchorFile, gitRoot, map[string]bool{}, &paths)

		for _, includeFile := range paths.includes {
			addUnique(includeFile)
		}
		for _, tfDir := range paths.tfDirs {
			addUnique(filepath.Join(tfDir, "*.tf*"))
		}

		for _, depDir := range paths.deps {
			depDir = filepath.Clean(depDir)
			if unitDirs[depDir] {
				// internal to the stack; ordered by terragrunt stack run
				continue
			}
			depFile := filepath.Join(depDir, "terragrunt.hcl")
			addUnique(depFile)

			// Cascade through the dependency's own dependencies so
			// transitive changes propagate, mirroring regular projects
			if !cascadeDependencies {
				continue
			}
			if _, err := os.Stat(depFile); err != nil {
				continue
			}
			depOpts, err := options.NewTerragruntOptionsWithConfigPath(depFile)
			if err != nil {
				continue
			}
			depOpts.Env = getEnvs()
			depCtx := config.NewParsingContext(context.Background(), depOpts)
			cascaded, err := safeGetDependencies(depCtx, depFile)
			if err != nil {
				log.Debugf("Failed to cascade stack dependency %s: %v", depFile, err)
				continue
			}
			for _, cascadedDep := range cascaded {
				if filepath.IsAbs(cascadedDep) {
					addUnique(cascadedDep)
				}
			}
		}
	}

	stack.ExtraWatchPaths = extra
}
