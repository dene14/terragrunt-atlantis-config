unit "vpc" {
  source                  = "../../units/vpc"
  path                    = "main"
  no_dot_terragrunt_stack = true
}

unit "peering" {
  source                  = "../../units/vpc"
  path                    = "peering"
  no_dot_terragrunt_stack = true
}
