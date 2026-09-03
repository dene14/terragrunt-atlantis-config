# Uses the values sidecar (terragrunt.values.hcl) both for an atlantis
# override local and for terraform source interpolation.
locals {
  atlantis_workflow = "values-${values.environment}"
}

terraform {
  source = "git::git@github.com:example-corp/infra-modules.git//service?ref=${values.module_version}"
}
