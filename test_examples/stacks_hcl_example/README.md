# stacks_hcl_example

Example of the native Terragrunt stacks feature (`terragrunt.stack.hcl`).

Layout:

- `units/` — a catalog of reusable unit configurations
- `live/prod/terragrunt.stack.hcl` — a stack instantiating units from the catalog

With `--enable-stacks`, a single Atlantis project is generated for
`live/prod` whose autoplan `when_modified` patterns cover the catalog
directories. Without the flag, the stack file is ignored and the catalog
directories get regular projects (unchanged behavior).
