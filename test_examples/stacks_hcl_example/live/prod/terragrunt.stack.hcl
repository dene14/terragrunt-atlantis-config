unit "vpc" {
  source = "../../units/vpc"
  path   = "vpc"
}

unit "app" {
  source = "../../units/app"
  path   = "app"
  values = {
    vpc_id = "dependency.vpc.outputs.vpc_id"
  }
}
