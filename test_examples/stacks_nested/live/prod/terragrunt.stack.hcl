unit "vpc" {
  source = "../../catalog/vpc"
  path   = "vpc"
}

# Nested stack: its definition comes from a shared catalog too
stack "mini" {
  source = "../../catalog/mini-stack"
  path   = "mini"
}
