include "root" {
  path = find_in_parent_folders("root.hcl")
}

terraform {
  source = "git::git@github.com:transcend-io/terraform-aws-fargate-container?ref=v0.0.4"
}

# Excluded from apply only, so it must still be planned.
exclude {
  if      = true
  actions = ["apply"]
}
