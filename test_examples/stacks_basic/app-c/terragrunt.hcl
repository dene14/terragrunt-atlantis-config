terraform {
  source = "git::https://github.com/example/terraform-modules.git//app?ref=v1.0.0"
}

dependency "app_a" {
  config_path = "../app-a"
}

inputs = {
  name = "app-c"
  depends_on_app_a = dependency.app_a.outputs.id
}


