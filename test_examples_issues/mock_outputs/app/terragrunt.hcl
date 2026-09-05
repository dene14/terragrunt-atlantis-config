terraform {
  source = "git::git@github.com:example-corp/infra-modules.git//app?ref=v1.0.0"
}

dependency "db" {
  config_path = "../db"

  mock_outputs = {
    arn      = "mock-arn"
    endpoint = "mock:5432"
  }
}

inputs = {
  db_arn      = dependency.db.outputs.arn
  db_endpoint = dependency.db.outputs.endpoint
}
