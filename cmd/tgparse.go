package cmd

import (
	"context"
	"io"

	"github.com/gruntwork-io/terragrunt/pkg/config"
	"github.com/gruntwork-io/terragrunt/pkg/log"
	"github.com/gruntwork-io/terragrunt/pkg/log/format"
	"github.com/gruntwork-io/terragrunt/pkg/options"
)

// quietTerragruntLogger is the logger handed to terragrunt's parser. Its
// diagnostics writer surfaces every HCL eval hiccup at its default ERROR
// level — in real repositories, catalog/template files evaluated outside
// their runtime context (get_path_from_repo_root() arithmetic on short
// paths, read_terragrunt_config into non-existent parents, sops locks)
// produce an unreadable wall of "Error: Invalid index" spam. Nothing
// user-actionable travels this channel: failures reach users as returned Go
// errors with positions (we log those ourselves). A bare formatter is still
// required: terragrunt's parser options dereference Logger.Formatter().
func quietTerragruntLogger() log.Logger {
	return log.New(
		log.WithOutput(io.Discard),
		log.WithFormatter(format.NewFormatter(format.NewBareFormatPlaceholders())),
	)
}

// newParsingContext wraps config.NewParsingContext with the quiet logger so
// call sites stay one-liners, mirroring the pre-0.98 API shape. Parsing
// contexts also pick up a sibling terragrunt.values.hcl when present.
func newParsingContext(ctx context.Context, opts *options.TerragruntOptions) *config.ParsingContext {
	_, pctx := config.NewParsingContext(ctx, quietTerragruntLogger(), opts)
	return attachSidecarValues(ctx, pctx)
}
