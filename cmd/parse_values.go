package cmd

import (
	"context"
	"path/filepath"

	"github.com/gruntwork-io/terragrunt/pkg/config"

	log "github.com/sirupsen/logrus"
)

// TerragruntValuesFile is the conventional sidecar file name terragrunt
// loads next to a config to provide the `values` variable, e.g.:
//
//	environment = "prod"
//	team        = "payments"
//
// terragrunt.hcl can then interpolate ${values.environment}.
const TerragruntValuesFile = "terragrunt.values.hcl"

// attachSidecarValues loads a terragrunt.values.hcl sitting next to the
// config being parsed (if any) and exposes it as the `values` variable of the
// parsing context. Terragrunt v1.x ships this natively for units; here the
// library engine is taught the same behavior so both engines see identical
// semantics.
//
// Loading is best-effort by design: a malformed or unreadable sidecar must
// not abort generation for the whole repository.
func attachSidecarValues(ctx context.Context, pctx *config.ParsingContext) *config.ParsingContext {
	configPath := pctx.TerragruntOptions.TerragruntConfigPath
	if configPath == "" {
		return pctx
	}

	dir := filepath.Dir(configPath)
	if !fileExists(filepath.Join(dir, TerragruntValuesFile)) {
		return pctx
	}

	values, err := config.ReadValues(ctx, quietTerragruntLogger(), pctx.TerragruntOptions, dir)
	if err != nil || values == nil {
		log.Debugf("could not read values sidecar for %s: %v", configPath, err)
		return pctx
	}

	return pctx.WithValues(values)
}
