unit "vpc" {
  source = "${find_in_parent_folders("units/vpc")}"
  path   = "vpc"
}

unit "database" {
  source = "${find_in_parent_folders("units/database")}"
  path   = "database"
}

unit "app" {
  source = "${find_in_parent_folders("units/app")}"
  path   = "app"
}

stack "production" {
  description = "Production environment stack"
}

