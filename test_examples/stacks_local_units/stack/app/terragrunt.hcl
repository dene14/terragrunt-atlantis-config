terraform {
  source = "."
}

dependency "vpc" {
  config_path = "../vpc"
}
