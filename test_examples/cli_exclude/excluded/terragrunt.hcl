include "root" {
  path = find_in_parent_folders("root.hcl")
}

terraform {
  source = "git::git@github.com:transcend-io/terraform-aws-fargate-container?ref=v0.0.4"
}

exclude {
  if      = true
  actions = ["all"]
}
