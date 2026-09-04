# root.hcl copied into the terragrunt cache by stack generation. If discovery
# walks into the cache, this bogus module becomes a project.
terraform {
  source = "git::git@github.com:example-corp/infra-modules.git//cache-copy?ref=v1.0.0"
}
