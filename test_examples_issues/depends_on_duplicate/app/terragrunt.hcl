terraform {
  source = "git::git@github.com:example-corp/infra-modules.git//app?ref=v1.0.0"

  extra_arguments "shared_vars" {
    commands = get_terraform_commands_that_need_vars()

    optional_var_files = [
      "../dep/shared.tfvars"
    ]
  }
}

dependency "dep" {
  config_path = "../dep"
}
