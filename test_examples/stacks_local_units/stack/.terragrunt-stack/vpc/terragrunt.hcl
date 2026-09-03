# Simulates the output of `terragrunt stack generate`: generated units live in
# .terragrunt-stack and must not become Atlantis projects when --enable-stacks
# is used (with the flag off, they are discovered like any other directory).
terraform {
  source = "."
}
