# A non-declared addition living inside a stack's directory. The stack file
# only declares its own units (vpc with path "main"): user-authored nuggets
# like this one are NOT stack-owned and must stay their own Atlantis project.
terraform {
  source = "git::git@github.com:example-corp/infra-modules.git//addon?ref=v1.0.0"
}
