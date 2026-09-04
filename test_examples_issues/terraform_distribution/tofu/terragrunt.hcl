terraform {
  source = "git::git@github.com:example-corp/infra-modules.git//vpc?ref=v1.0.0"
}

locals {
  atlantis_terraform_distribution = "tofu"
}
