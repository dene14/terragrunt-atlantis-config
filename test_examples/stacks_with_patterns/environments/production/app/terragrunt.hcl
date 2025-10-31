terraform {
  source = "git::https://github.com/example/terraform-modules.git//app?ref=v1.0.0"
}

dependency "vpc" {
  config_path = "../../../shared/vpc"
}

inputs = {
  name   = "production-app"
  vpc_id = dependency.vpc.outputs.vpc_id
}


