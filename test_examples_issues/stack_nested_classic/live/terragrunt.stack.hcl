stack "vpc" {
  source = "../catalog/vpc-stack"
  path   = "vpc"

  no_dot_terragrunt_stack = true
}