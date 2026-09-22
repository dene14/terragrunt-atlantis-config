<p align="center">
  <img alt="Terragrunt Atlantis Config by Transcend" src="https://user-images.githubusercontent.com/7354176/78756035-f9863480-792e-11ea-96d3-d4ffe50e0269.png"/>
</p>
<h1 align="center">Terragrunt Atlantis Config</h1>
<p align="center">
  <strong>Generate Atlantis Config for Terragrunt projects.</strong>
</p>
<br />

> **This fork is actively maintained.** The original repository
> (`transcend-io/terragrunt-atlantis-config`) no longer publishes updates.
> Highlights of this continuation:
>
> - **Terragrunt v1.x support** via a dedicated `--engine=cli` mode that
>   delegates parsing to the terragrunt binary (works with v1.1.4 and newer)
>   unit-catalog watches — see [`README_STACKS.md`](README_STACKS.md)
> - `terragrunt.values.hcl` sidecar support
> - OpenTofu-specific syntax (e.g. indexed providers) handled correctly
> - Preserved workflows are kept byte-identical between runs (no key
>   reordering, comments survive)

## What is this?

[Atlantis](https://runatlantis.io) is an awesome tool for Terraform pull request automation. Each repo can have a YAML config file that defines Terraform module dependencies, so that PRs that affect dependent modules will automatically generate `terraform plan`s for those modules.

[Terragrunt](https://terragrunt.gruntwork.io) is a Terraform wrapper, which has the concept of dependencies built into its configuration.

This tool creates Atlantis YAML configurations for Terragrunt projects by:

- Finding all `terragrunt.hcl` in a repo
- Evaluating their `dependency`, `terraform`, `locals`, and other source blocks to find their dependencies
- Creating a Directed Acyclic Graph of all dependencies
- Constructing and logging YAML in Atlantis' config spec that reflects the graph

This is especially useful for organizations that use monorepos for their Terragrunt config (as we do at Transcend), and have thousands of lines of config.

## Parsing engine

Since v1.27.0, terragrunt-atlantis-config uses only the `cli` engine:
generation asks the `terragrunt` binary itself for discovery data
(`terragrunt find --json --dependencies --reading`), so semantics always match
the deployed terragrunt version exactly — stacks, `exclude` blocks,
autoinclude, dependency expansions and all.

> **Removed in v1.27.0** — the embedded library engine (`--engine=library`)
> and the `--engine=auto` semantic fallback. Terragrunt closed its Go API at
> 1.0; v0.99.x was the last release consumable as a library. The adjacent
> `--terraform-version` files, `.terraform-version` discovery and `atlantis_*`
> locals-based overrides (`atlantis_workflow`, `atlantis_skip`, ...) were only
> evaluateiable via the library and are likewise gone; use `--exclude`, the
> terragrunt-native `exclude` block, and explicit server-side workflows
> instead.

| Engine  | How it works                                                                                          | Use |
| ------- | ------------------------------------------------------------------------------------------------------ | --- |
| `cli`   | Runs `terragrunt find --json --dependencies --reading` on the terragrunt v1.x binary available on `$PATH`. Semantics always match the terragrunt you run plans with. | everywhere |
| `auto`  | Accepted as an alias of `cli` (kept so existing `--engine=auto` invocations keep working).             | — |

Notes:

- The `cli` engine discovers stacks natively; stack projects additionally
  watch the local unit sources they reference, so editing a shared unit
  catalog re-triggers dependent stacks.
  library engine. `--execution-order-groups` and `--depends-on` are native in
  the cli engine, computed from the exact dependency graph `terragrunt find`
  reports.

## Integrate into your Atlantis Server

The recommended way to use this tool is to install it onto your Atlantis server, and then use a [Pre-Workflow hook](https://www.runatlantis.io/docs/pre-workflow-hooks.html#pre-workflow-hooks) to run it after every clone. This way, Atlantis can automatically determine what modules should be planned/applied for any change to your repository.

To get started, add a `pre_workflow_hooks` field to your `repos` section of your [server-side repo config](https://www.runatlantis.io/docs/server-side-repo-config.html#do-i-need-a-server-side-repo-config-file):

```json
{
  "repos": [
    {
      "id": "<your_github_repo>",
      "workflow": "default",
      "pre_workflow_hooks": [
        {
          "run": "terragrunt-atlantis-config generate --output atlantis.yaml --autoplan --parallel --create-workspace"
        }
      ]
    }
  ]
}
```

Then, make sure `terragrunt-atlantis-config` is present on your Atlantis server. There are many different ways to configure a server, but this example in [Packer](https://www.packer.io/) should show the bash commands you'll need just about anywhere:

```hcl
variable "terragrunt_atlantis_config_version" {
  default = "1.24.0"
}

build {
  // ...
  provisioner "shell" {
    inline = [
      "wget https://github.com/dbccompany/terragrunt-atlantis-config/releases/download/v${var.terragrunt_atlantis_config_version}/terragrunt-atlantis-config_${var.terragrunt_atlantis_config_version}_linux_amd64",
      "wget https://github.com/dbccompany/terragrunt-atlantis-config/releases/download/v${var.terragrunt_atlantis_config_version}/SHA256SUMS",
      "grep 'linux_amd64$' SHA256SUMS | sha256sum -c -",
      "mv terragrunt-atlantis-config_${var.terragrunt_atlantis_config_version}_linux_amd64 terragrunt-atlantis-config",
      "sudo install terragrunt-atlantis-config /usr/local/bin",
    ]
    inline_shebang = "/bin/bash -e"
  }
  // ...
}
```

and just like that, your developers should never have to worry about an `atlantis.yaml` file, or even need to know what it is.

## Extra dependencies & locals-based overrides (removed in v1.27)

`extra_atlantis_dependencies`, the `atlantis_*` locals (`atlantis_workflow`,
`atlantis_skip`, `atlantis_autoplan`, `atlantis_terraform_version`,
`atlantis_terraform_distribution`, `atlantis_project`, ...), the
library-engine features and are removed. On the cli engine:

- watch files come straight from terragrunt's `--reading` output, then
  `--filter` / `--exclude` / `--filter-git`
- per-module skipping is terragrunt's own `exclude` block or `--exclude`
- pinning use `.terraform-version` or `--terraform-version`
- stack boundaries are declared in `terragrunt.stack.hcl`

## Project generation

These flags offer additional options to generate Atlantis projects based on HCL configuration files in the terragrunt hierarchy. This, for example, enables Atlantis to use `terragrunt run-all` workflows on staging environment or product levels in a terragrunt hierarchy. Mostly useful in large terragrunt projects containing lots of interdependent child modules. Atlantis `locals` can be used in the defined project marker files.

| Flag Name                    | Description                                                                                                                                                                     | Default Value     | Type |
| ---------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ----------------- |----- |

## Separate workspace for parallel plan and apply

Atlantis added support for running plan and apply parallel in [v0.13.0](https://github.com/runatlantis/atlantis/releases/tag/v0.13.0).

To use this feature, projects have to be separated in different workspaces, and the `create-workspace` flag enables this by concatenating the project path as the
name of the workspace.

As an example, project `${git_root}/stage/app/terragrunt.hcl` will have the name `stage_app` as workspace name. This flag should be used along with `parallel` to enable parallel plan and apply:

```bash
terragrunt-atlantis-config generate --output atlantis.yaml --parallel --create-workspace
```

Enabling this feature may consume more resources like cpu, memory, network, and disk, as each workspace will now be cloned separately by atlantis.

As when defining the workspace this info is also needed when running `atlantis plan/apply -d ${git_root}/stage/app -w stage_app` to run the command on specific directory,
you can also use the `atlantis plan/apply -p stage_app` in case you have enabled the `create-project-name` cli argument (it is `false` by default).

## Rules for merging config

Each terragrunt module can have locals, but can also have zero to many `include` blocks that can specify parent terragrunt files that can also have locals.

In most cases (for string/boolean locals), the primary terragrunt module has the highest precedence, followed by the locals in the lowest appearing `include` block, etc. all the way until the lowest precedence at the locals in the first `include` block to appear.

However, there is one exception where the values are merged, which is the `atlantis_extra_dependencies` local. For this local, all values are appended to one another. This way, you can have `include` files declare their own dependencies.

## Local Installation and Usage

You can install this tool locally to checkout what kinds of config it will generate for your repo, though in production it is recommended to [install this tool directly onto your Atlantis server](##integrate-into-your-atlantis-server)

Recommended: install via `go install`:

```bash
go install github.com/dbccompany/terragrunt-atlantis-config@latest
```

…or fetch a release binary and verify it against the published checksums:

```bash
VERSION="1.24.0"
curl -sSfLO "https://github.com/dbccompany/terragrunt-atlantis-config/releases/download/v${VERSION}/terragrunt-atlantis-config_${VERSION}_linux_amd64"
curl -sSfLO "https://github.com/dbccompany/terragrunt-atlantis-config/releases/download/v${VERSION}/SHA256SUMS"
grep "linux_amd64$" SHA256SUMS | sha256sum -c -
mv "terragrunt-atlantis-config_${VERSION}_linux_amd64" terragrunt-atlantis-config
chmod 755 terragrunt-atlantis-config
```

This module officially supports golang v1.27, tested on Github with each build. 
This module also officially supports both Windows and Nix-based file formats, tested on Github with each build. CLI-engine tests additionally run against the real `terragrunt` binary (currently v1.1.4, sha256-pinned) on Linux and Windows runners.

Usage Examples (see below sections for all options):

```bash
# From the root of your repo
terragrunt-atlantis-config generate

# or from anywhere
terragrunt-atlantis-config generate --root /some/path/to/your/repo/root

# output to a file
terragrunt-atlantis-config generate --autoplan --output ./atlantis.yaml
```

Finally, check the log output (or your output file) for the YAML.

## Contributing

To test any changes you've made, run `make gotestsum` (or `make test` for standard golang testing).

For maintainers: cut a release by pushing a `v*` tag — the release workflow
builds all binaries plus SHA256/SHA512 checksums and publishes them.

## Contributors

<img src="./CONTRIBUTORS.svg">

## Stargazers over time

[![Stargazers over time](https://starchart.cc/dbccompany/terragrunt-atlantis-config.svg)](https://starchart.cc/dbccompany/terragrunt-atlantis-config)

## License
### Locals (previous versions)

In v1.27 the old `atlantis_*` locals are gone with the library engine. If you
had them, refactor per the table below:

| Former local                          | Replacement |
| ------------------------------------- | ----------- |
| `atlantis_skip`                       | terragrunt `exclude` block or `--exclude` flag |
| `atlantis_workflow`                   | per-dir workflows (`--workflow` at scope) |
| `atlantis_apply_requirements`         | `--apply-requirements` |
| `atlantis_terraform_version`          | `.terraform-version` file |
| `atlantis_terraform_distribution`     | `--terraform-distribution` |
| `atlantis_autoplan`                   | `--autoplan` |
| `extra_atlantis_dependencies`         | `--filter` / `--filter-git` / terragrunt `--reading`-driven watch set |
| `atlantis_project`                    | terragrunt stacks |

