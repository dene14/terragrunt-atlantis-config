include "root" { path = find_in_parent_folders("repo_root.hcl") }
terraform { source = "github.com/example/state" }
