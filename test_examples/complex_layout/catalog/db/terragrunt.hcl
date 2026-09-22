terraform { source = "../modules/db" }
dependency "vpc" { config_path = "../vpc" }
