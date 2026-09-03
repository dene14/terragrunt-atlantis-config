terraform {
  source = "git::https://github.com/example/terraform-modules.git//vpc?ref=v1.0.0"
}

inputs = {
  name = "shared-vpc"
  cidr = "10.0.0.0/16"
}


