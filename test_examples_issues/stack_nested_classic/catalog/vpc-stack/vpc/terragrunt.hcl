terraform {
  source = "tfr:///terraform-aws-modules/vpc/aws?version=6.6.1"
}

inputs = {
  cidr = "10.0.0.0/16"
}