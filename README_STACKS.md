# Terragrunt Stacks Support

This fork adds support for [Terragrunt stacks](https://terragrunt.gruntwork.io/docs/features/stacks/)
to `terragrunt-atlantis-config`. All stack functionality is **opt-in** via `--enable-stacks`;
without the flag the output is byte-identical to upstream behavior.

## Quick start

```bash
terragrunt-atlantis-config generate \
  --root . \
  --enable-stacks \
  --stack-workflow terragrunt-stack \
  --output atlantis.yaml
```

Flags:

| Flag                      | Description                                                                                |
| ------------------------- | ------------------------------------------------------------------------------------------ |
| `--enable-stacks`         | Enable stack discovery and stack project generation                                        |
| `--stack-workflow`        | Workflow for stack projects (falls back to `--workflow`)                                   |
| `--stack-definition-file` | Additional YAML/JSON file declaring stacks, relative to `--root` unless absolute           |

Global flags `--autoplan`, `--terraform-version`, `--create-workspace`, `--create-project-name`
and `--workflow` apply to stack projects the same way they apply to regular projects.

## Source 1: `terragrunt.stack.hcl` (native Terragrunt stacks)

Every `terragrunt.stack.hcl` below `--root` becomes one Atlantis project whose `dir` is the
directory of the stack file. A workflow for that project is expected to run the stack, e.g.
`terragrunt stack run plan` (which regenerates `.terragrunt-stack` before running).

Example layout:

```
units/                      # unit catalog (shared templates)
  vpc/terragrunt.hcl
  app/terragrunt.hcl
live/
  prod/terragrunt.stack.hcl # the stack
```

```hcl
# live/prod/terragrunt.stack.hcl
unit "vpc" {
  source = "../../units/vpc"
  path   = "vpc"
}

unit "app" {
  source = "../../units/app"
  path   = "app"
}
```

Generated project:

```yaml
projects:
  - dir: live/prod
    workflow: terragrunt-stack
    autoplan:
      enabled: false
      when_modified:
        - "*.hcl"
        - "*.tf*"
        - "**/*.hcl"
        - "**/*.tf*"
        - ../../units/vpc/**/*.hcl
        - ../../units/vpc/**/*.tf*
        - ../../units/app/**/*.hcl
        - ../../units/app/**/*.tf*
```

Semantics:

- **Name**: with `--create-project-name`, the project is named after the stack directory
  (e.g. `live_prod`).
- **Members vs. sources**: if a unit's `path` resolves to a directory that already contains a
  `terragrunt.hcl` in the repo, that directory is a stack *member* and gets no individual project.
  Directories referenced through a unit's local `source` (catalogs like `units/vpc`) are treated
  as templates: they are watched via `when_modified` but get no Atlantis project of their own
  (planning a template directory in-place would be meaningless).
- **Dependency tracking**: each unit's config is evaluated anchored at the unit's *generated*
  location (as if `terragrunt stack generate` had already run). From it:
  - `include` chains (e.g. `find_in_parent_folders` of shared root env/account files) are
    watched;
  - `dependency` blocks pointing at sibling units inside the same stack are ignored (terragrunt
    orders them during `stack run`); dependencies outside the stack are watched, and — with
    `--cascade-dependencies` (default) — their own dependencies are followed transitively, the
    same way regular projects behave. This also feeds correct `execution_order_group` values
    when combined with `--execution-order-groups`;
  - local `terraform { source = ... }` directories of unit configs are watched.
- **Remote sources** (git refs, registry addresses) cannot be watched and are skipped.
- **Nested `stack` blocks** contribute their local `source` directories to `when_modified`.
  Every `terragrunt.stack.hcl` found in the repo gets its own project regardless of nesting
  (a nested stack definition file is itself discovered and projected).
- **Parsing is tolerant**: the file is first decoded with a full Terragrunt evaluation context
  (so functions like `find_in_parent_folders()` work); if evaluation fails, a fallback pass
  extracts the statically-evaluable `source`/`path` literals instead of dropping the file.
- `.terragrunt-stack` and `.terragrunt-cache` directories are excluded from stack discovery,
  and (only with `--enable-stacks`) from regular project discovery as well.

See `test_examples/stacks_hcl_example/` and `test_examples/stacks_local_units/`.

## Source 2: stack definition file (`--stack-definition-file`)

Stacks can additionally be declared in a YAML/JSON file:

```yaml
version: 1
stacks:
  - name: production-environment
    description: All production infrastructure
    include:                      # glob patterns, relative to the repo root
      - "environments/production/**"
    exclude:
      - "environments/production/experimental/**"
    depends_on:                   # other stack names; requires --create-project-name
      - shared-infrastructure
    atlantis:
      workflow: production
      autoplan: false
      apply_requirements: [approved, mergeable]
      execution_order_group: 100

  - name: shared-infrastructure
    modules:                      # explicit directories instead of patterns
      - shared/vpc
    atlantis:
      workflow: shared
      autoplan: true
      execution_order_group: 1
```

Rules:

- Stacks must define `include`/`modules`; both are relative to the repo root.
- Member modules do not get individual projects.
- The project `dir` is the common parent directory of all matched modules.
- `depends_on` entries reference other stack names and are only emitted when
  `--create-project-name` is set (Atlantis `depends_on` requires project names).
- `atlantis.autoplan`/`workflow`/`terraform_version` override the corresponding global flags
  for that stack.

See `test_examples/stacks_basic/` and `test_examples/stacks_with_patterns/`.

## Atlantis workflow for stacks

Stack projects need a workflow that runs stack commands. Define it in the `workflows` section of
your `atlantis.yaml` (preserved across regeneration with `--preserve-workflows`) or server-side.
A battle-tested variant that also keeps Atlantis policy checks working (tested with
runatlantis/atlantis v0.47.1):

```yaml
workflows:
  terragrunt-stack:
    plan:
      steps:
        - run: |
            # -out is relative; each unit's planfile lands in its own
            # .terragrunt-cache working dir for the policy_check step.
            terragrunt stack run -- plan -input=false -out=tac-stack-plan.tfplan
            # Atlantis's policy_check delegate unconditionally reads the
            # workflow-conventional planfile (<dir>/<workspace>.tfplan)
            # before running any policy step. Create it (local backends
            # never read its content).
            touch "$PLANFILE"
    policy_check:
      steps:
        - run: |
            set -e
            tmpdir=$(mktemp -d)
            find . -type f -name tac-stack-plan.tfplan -path '*/.terragrunt-cache/*' | sort |
            (i=0
             while IFS= read -r plan; do
               i=$((i + 1)); dir=$(dirname "$plan")
               (cd "$dir" && tofu"${ATLANTIS_TERRAFORM_VERSION}" show -json tac-stack-plan.tfplan) > "$tmpdir/$i.json"
             done)
            [ -e "$tmpdir/1.json" ] || { echo '{"resource_changes": []}' > "$SHOWFILE"; rm -rf "$tmpdir"; exit 0; }
            jq -s '{format_version: (.[0].format_version // "1.2"),
                    resource_changes: [ .[] | (.resource_changes // [])[] ]}' "$tmpdir"/*.json > "$SHOWFILE"
        - policy_check
```

The `policy_check` step above renders each unit's plan to JSON and merges the `resource_changes`
arrays into one synthetic plan at `$SHOWFILE`, so OPA/conftest gates a stack exactly like a
regular project ("Phase 2" of policy enforcement applies uniformly).

### Unapplied dependencies (first plan of a new stack)

Units reading `dependency.X.outputs.*` fail on the very first plan if `X` has never been applied
(`tofu output` has nothing to return). Terragrunt's idiomatic remedy is `mock_outputs`:

```hcl
dependency "vpc" {
  config_path = "../main"
  mock_outputs = { vpc_id = "vpc-mock0000000000000" }
}
```

Once the dependency is applied, real state takes over (default `no_merge`), so the mock only ever
matters for the bootstrapping window.

## Current limitations

- One Atlantis project per stack; per-unit granularity via generated `.terragrunt-stack`
  directories is deliberately not produced (those directories do not exist on a fresh clone).
- Explicit `depends_on` between stack projects requires the definition file.
- `locals { extra_atlantis_dependencies = ... }` inside unit configs are not yet collected
  (upstream feature parity todo).
- Atlantis policy_check needs a workflow-conventional planfile to exist (the step above takes care
  of it); without it the built-in phase fails with `open .../default.tfplan: no such file`.

## Tests

- Unit tests: `cmd/stack_test.go`, `cmd/stack_hcl_test.go` (incl. nested stack parsing,
  external-dependency cascading, catalog exclusion and unreadable-directory tolerance)
- Golden-file integration tests: `TestStacks*` in `cmd/generate_test.go` with expected outputs in
  `cmd/golden/stacks_*.yaml`, including flag-off regression tests pinning the pre-existing
  behavior on the same example repositories.
