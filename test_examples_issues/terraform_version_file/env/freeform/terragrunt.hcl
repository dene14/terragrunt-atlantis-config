terraform {
  source = "git::git@github.com:example-corp/infra-modules.git//app?ref=v1.0.0"
}

locals {
  atlantis_terraform_version = "1.2.9"
}
