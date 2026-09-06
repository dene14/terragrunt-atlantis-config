# This file simulates what `terragrunt stack generate` writes when a stack
# pinpoints no_dot_terragrunt_stack=true: content materialized directly under
# the stack dir. Never its own Atlantis project.
terraform {
  source = "git::git@github.com:example-corp/infra-modules.git//vpc?ref=v1.0.0"
}
