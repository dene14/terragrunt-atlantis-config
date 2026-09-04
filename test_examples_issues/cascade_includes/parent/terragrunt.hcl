terraform {
  source = "git::git@github.com:example-corp/infra-modules.git//parent?ref=v1.0.0"
}

dependency "shared" {
  config_path = "../shared"
}
