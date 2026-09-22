terraform { source = "github.com/example/db" }
dependency "vpc" { config_path = "../vpc" }
