package cmd

import (
	"context"

	"github.com/gruntwork-io/terragrunt/pkg/config"
	"github.com/gruntwork-io/terragrunt/pkg/log"
	"github.com/gruntwork-io/terragrunt/pkg/log/format"
	"github.com/gruntwork-io/terragrunt/pkg/options"
)

// quietTerragruntLogger returns a Terragrunt logger that only surfaces
// errors. The generator writes atlantis.yaml to stdout, so library chatter
// at info/warn level must stay out of the way. A bare formatter is still
// required: terragrunt's parser options dereference Logger.Formatter().
func quietTerragruntLogger() log.Logger {
	return log.New(
		log.WithLevel(log.ErrorLevel),
		log.WithFormatter(format.NewFormatter(format.NewBareFormatPlaceholders())),
	)
}

// newParsingContext wraps config.NewParsingContext with the quiet logger so
// call sites stay one-liners, mirroring the pre-0.98 API shape.
func newParsingContext(ctx context.Context, opts *options.TerragruntOptions) *config.ParsingContext {
	_, pctx := config.NewParsingContext(ctx, quietTerragruntLogger(), opts)
	return pctx
}
