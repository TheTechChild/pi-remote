# ADR-0004: golangci-lint version floats to `latest` in CI

## Status

Accepted

Date: 2026-05-07

## Context

SPEC.md § 22.2 pins `golangci-lint v1.62+`. Both Go services (`pi-remote-daemon`
and `pi-remote-coordinator`) use Go 1.25 toolchains (also per § 22.2:
"Go toolchain — 1.25.x or current stable").

`golangci/golangci-lint-action@v8`'s default release at the time of bootstrap
was a v2.1 binary built against Go 1.24. That binary refuses to lint a Go 1.25
module with the error:

> the Go language version (go1.24) used to build golangci-lint is lower than
> the targeted Go version (1.25)

Pinning to `v1.62` would re-introduce v1-style configuration (which differs
materially from v2 — formatters are split out from linters, the schema has
changed) and would still face the same build-toolchain mismatch.

## Decision

Both Go services' GitHub Actions workflows pass
`version: latest` to `golangci/golangci-lint-action@v8`. Both `.golangci.yml`
files use the v2 schema:

```yaml
version: "2"
linters:
  default: standard
  enable: [govet, ineffassign, misspell, staticcheck, unused]
formatters:
  enable: [gofmt]
```

This means each CI run installs whichever golangci-lint release is current.
The action does its own caching; the only practical cost is occasional
warnings when a new lint or formatter is enabled by default.

## Consequences

**Positive:**
- Always built against a Go toolchain compatible with Go 1.25.
- Picks up new lints automatically.

**Negative:**
- A breaking change in golangci-lint can fail CI on a green PR. Mitigation:
  if this happens, a single PR pins to a known-good v2.x release.

If CI flakiness from upstream becomes a recurring problem, this ADR is
superseded by one pinning to a specific v2.x patch release.

## References

- SPEC.md § 22.2.
- golangci-lint v2 migration: https://golangci-lint.run/product/migration-guide/
