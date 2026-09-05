terraform {
  source = "git::git@github.com:example-corp/infra-modules.git//app?ref=v1.0.0"
}

# Two dependency blocks sharing the label; terragrunt merges them (warns)
# and the last config_path wins for inputs references. We must still keep
# the entire project list healthy and watch both paths observed either way.
dependency "shared" {
  config_path = "../first"
}

dependency "shared" {
  config_path = "../second"
}

inputs = {
  out = dependency.shared.outputs.value
}
