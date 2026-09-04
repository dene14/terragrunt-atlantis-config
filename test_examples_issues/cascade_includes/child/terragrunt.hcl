include {
  path = "../parent/terragrunt.hcl"
}

terraform {
  source = "git::git@github.com:example-corp/infra-modules.git//child?ref=v1.0.0"
}
