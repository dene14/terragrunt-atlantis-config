terraform {
  source = "git::https://github.com/example/terraform-modules.git//app?ref=v1.0.0"
}

inputs = {
  name = "app-b"
}


