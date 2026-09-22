unit "vpc" {
  source = "${get_repo_root()}/catalog/vpc"
  path   = "vpc"
  no_dot_terragrunt_stack = true
}
unit "db" {
  source = "${get_repo_root()}/catalog/db"
  path   = "db"
  no_dot_terragrunt_stack = true
}
stack "observability" {
  source = "nonexistent-on-purpose"
  path   = "observability"
  no_dot_terragrunt_stack = true
}
