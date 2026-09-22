# Security Policy

## Reporting

Please report vulnerabilities discreetly. Open a GitHub issue with "Security:"
in the title, or contact the maintainers directly. We treat reports seriously
and will respond promptly.

## Current known-false-positive alerts (reviewed September 2026)

Dependabot periodically opens alerts for `go-git/v6`/`go-billy/v6` (and the
occasional `terragrunt` advisories) that arrive only as transitive dependencies
of the embedded terragrunt v0.99.5 library:

```
terragrunt v0.99.5  -> internal/cas  -> internal/git  -> go-git/v6 -> go-billy/v6
```

These are **not reachable in this tool**:

- We use terragrunt exclusively for *partial HCL parsing*
  (`PartialParseConfigFile` / `DecodeBaseBlocks`) and `getter.Detect` for URL
  type detection. No git clone, CAS module fetch, `run_cmd`, or file mutation
  is ever triggered by terragrunt-atlantis-config.
- Consequently the git/cas code paths implicated by those advisories are
  linked into the binary but never executed.

Why the alerts are dismissed rather than "fixed":

- `go-git/v6` has **no stable release** — the CVE fixes exist only in
  `v6.0.0-alpha.*` pre-releases, which break the terragrunt 0.99.5 API
  (e.g. removal of `storage/filesystem.Options.KeepDescriptors`).
- Upgrading terragrunt itself is impossible as a library: terragrunt **v1.x
  closed its Go API** behind `internal/`.

Mitigations in place:

- `dependabot.yml` ignores routine version bumps for `go-git/v6`,
  `go-billy/v6`, and semver-major terragrunt bumps.
- The 9 applicable alerts are dismissed with reason "vulnerable code is not
  actually used".

## Planned resolution

The embedded terragrunt library engine is kept for backward compatibility, but
the direction is a full switch to the terragrunt 1.0 **CLI** (already shipped
behind `--engine=cli`, the default on terragrunt v1.x installs). The library
engine was removed in **v1.27.0** and with it `go-git`/`go-billy` drop out of
the build entirely, and the remaining `ignore` entries here can be removed.

If a future terragrunt release re-opens its Go API or ships a stable, patched
go-git, revisit the ignore list before any upgrade.