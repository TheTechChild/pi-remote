# Go dependency management

This document is the operational manual for the Go half of the monorepo
(`pi-remote-daemon/` and `pi-remote-coordinator/`). It is the Go-shaped
analog of [`package-management.md`](package-management.md), which covers
the JavaScript half. The decision rationale for the toolchain split lives
in [SPEC.md § 22](../pi-remote-spec/SPEC.md); this file covers the *how*.

## Overview

The monorepo contains two Go modules:

- `pi-remote-daemon/` — per-machine daemon. Module path
  `github.com/TheTechChild/pi-remote-daemon`.
- `pi-remote-coordinator/` — hosted broker. Module path
  `github.com/TheTechChild/pi-remote-coordinator`.

Each module owns its own `go.mod` and `go.sum`. Both are committed to git.
There is **no Go workspace** (`go.work`) at the repo root: the two
modules are deliberately independent and may diverge in minor-version
lines if the SPEC table permits.

The Go toolchain version is pinned via the `go` directive in each
`go.mod` and via the `go-version` field in the CI workflow
(`.github/workflows/ci.yml`). They must match.

## Source of truth: SPEC § 22.2 / § 22.3

The set of approved direct dependencies for each Go module is enumerated
in [SPEC.md § 22.2](../pi-remote-spec/SPEC.md#222-daemon-pi-remote-daemon-go)
(daemon) and [SPEC.md § 22.3](../pi-remote-spec/SPEC.md#223-coordinator-pi-remote-coordinator-go)
(coordinator). Those tables list, for each library, the **minor-version
line** the project is committed to (e.g., `github.com/coder/websocket
v1.8.x`, `github.com/google/uuid v1.6.x`).

**Adding a direct dep that is not on the SPEC § 22.2/22.3 table requires
a SPEC edit and an ADR.** Do not `go get` a new direct dep and call it
done. The SPEC tables are the merge gate; this document explains how to
honor them mechanically.

Transitive deps are not enumerated in SPEC and are not subject to
per-library approval — they ride along under whatever the direct deps
require, with `go.sum` pinning the resolution.

## Core rules

- **Direct deps are pinned to exact patch versions** in `go.mod` (e.g.,
  `v1.8.13`, not `v1.8.0` or some semver range). Go's module system
  already records exact resolved versions; this rule extends to the
  *intent* — we choose the patch deliberately, not whatever `go get
  package@latest` happens to surface on a given day.
- **The chosen patch is the latest patch within the SPEC-stated minor
  line at time of bump.** Use `go list -m -versions <module>` to
  enumerate available versions before picking one.
- **`go.sum` is committed.** It is part of the dep change, not a
  separate cleanup commit.
- **`go.mod`'s `go` directive matches SPEC § 22.2's "Go toolchain"
  row.** Currently `1.25.x or current stable` — `go 1.25` in `go.mod`.
- **No `replace` directives** without an ADR documenting the reason and
  the condition under which the replace is removed. `replace` is the
  Go-side analog of Yarn's `resolutions`: the heaviest hammer, used only
  when upstream metadata is wrong (not just missing).
- **No `exclude` directives** without the same ADR treatment.
- **No `// indirect` direct entries.** If `go mod tidy` moves a package
  out of `// indirect` because the workspace's code imports it directly,
  that means the package needs to be approved on the SPEC § 22.2/22.3
  table first. If the package is already approved, fine — proceed.
  Otherwise, stop and file the SPEC PR.
- **CI runs `go build`, `go test`, and `golangci-lint`** on every PR
  touching the module. CI does **not** yet enforce `go mod tidy`
  cleanliness; this is a known gap, tracked as a follow-up. In the
  meantime, run `go mod tidy` locally before committing dep changes and
  inspect the diff.

## Adding a new direct dep

1. **Verify approval.** Check SPEC § 22.2 (daemon) or § 22.3
   (coordinator). If the library and its minor-version line are listed,
   proceed. If not, **stop** — write an ADR proposing the addition, get
   the SPEC PR merged first, then resume.
2. **Pick the exact patch.** Run
   `go list -m -versions <module>` and pick the latest patch within the
   approved minor line. For example, if SPEC says `v1.8.x`, pick the
   highest available `v1.8.N`.
3. **Add it.** From inside the module directory:
   ```sh
   cd pi-remote-daemon  # or pi-remote-coordinator
   go get <module>@<exact-version>
   go mod tidy
   ```
4. **Review the diff.** `go.mod` should add one line (the new direct
   dep). `go.sum` should add the new module plus its transitives. Any
   other churn — especially direct-dep version changes you didn't ask
   for — is a signal that something is loose. Investigate.
5. **Commit `go.mod` and `go.sum` together,** in the same commit as the
   first code that imports the new package. The dep arrives with its
   first caller; never empty-add.

## Upgrading a dep

| Change shape          | What it requires                                       |
|-----------------------|--------------------------------------------------------|
| Patch within SPEC minor | Routine. `go get module@<new-patch>`, `go mod tidy`, commit. |
| Minor bump            | SPEC edit (update the table's minor-version line) + this dep PR rebased on the SPEC PR. |
| Major bump            | ADR + SPEC edit + the dep PR. Major bumps frequently bring breaking API changes — the ADR captures the reason for taking on the migration. |

For minor and major bumps, the SPEC PR lands first; the dep upgrade PR
rebases on `main` after.

For patch bumps, expect the `go.sum` diff to be small and bounded to the
upgraded module plus any of its transitives that also moved. Large
unrelated churn means something is loose.

## Toolchain version

Pinned in two places:

- `pi-remote-daemon/go.mod` and `pi-remote-coordinator/go.mod`: `go
  1.25` (matches SPEC § 22.2's "Go toolchain" row, "1.25.x or current
  stable").
- `.github/workflows/ci.yml`: `go-version: "1.25"` in the daemon and
  coordinator job steps.

When the SPEC bumps the toolchain row, all three locations must move in
the same PR.

## Testing & linting tools as deps

`github.com/stretchr/testify` (currently SPEC § 22.2 v1.10.x) is treated
as a regular dep, not a tooling install — it's imported by `_test.go`
files and goes through the same approval and pinning rules. Same for any
future test-helper library.

`golangci-lint` is a *binary* installed by CI, not a `go.mod` dep. Its
version policy lives in
[ADR-0004](../pi-remote-spec/adrs/0004-golangci-lint-version-floats-to-latest.md)
(floats to latest; pinned only if upstream breaks us).

`go-jsonschema` is also a binary, installed on demand by
`scripts/codegen.sh` into a local `.codegen-bin/` directory. It is not a
`go.mod` dep of either module.

## Codegen output

Generated wire-protocol types under `pi-remote-daemon/internal/proto/`
and `pi-remote-coordinator/internal/proto/` are **committed**, per SPEC
§ D25. CI re-runs codegen and diffs to verify the committed output
matches what the current schemas produce; mismatches fail the build.
Codegen produces real Go files importing only `encoding/json` and stdlib
— no third-party deps enter the picture from codegen.

## Current overrides

None at present. When the first `replace` or `exclude` directive is
added, document it here with: which module, what was wrong upstream,
what we pinned it to, and the condition under which we can remove the
override. Match the structure of `package-management.md`'s "Current
overrides" section.

## End-user installs

End users running `pi install git:github.com/TheTechChild/pi-remote` do
not install or run the daemon or coordinator — those ship via OS-native
packaging (launchd plist, systemd unit, Docker image). The Go binaries
are built either in CI (for releases) or by hand from a clone (for
development). `go.sum` governs both. There is no `pi install` analog of
the Yarn-vs-npm transitive-resolution split described in
`package-management.md`.

## See also

- [`go-local-dev.md`](go-local-dev.md) — the pre-push checklist for
  the Go workspaces (build, vet, gofmt, lint, test -race, docker)
- [`package-management.md`](package-management.md) — JavaScript-side
  analog of this doc
- [SPEC.md § 22.2](../pi-remote-spec/SPEC.md) — daemon dep table
- [SPEC.md § 22.3](../pi-remote-spec/SPEC.md) — coordinator dep table
- [SPEC.md § D25](../pi-remote-spec/SPEC.md) — generated code in PRs
- [ADR-0004](../pi-remote-spec/adrs/0004-golangci-lint-version-floats-to-latest.md)
  — golangci-lint version policy
- [AGENTS.md](../AGENTS.md) — top-level pointer at this doc + the JS one
