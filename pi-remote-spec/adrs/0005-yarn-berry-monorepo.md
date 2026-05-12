# ADR-0005: Yarn 4 (Berry) with workspaces; monorepo layout

## Status

Accepted

Date: 2026-05-12

Supersedes [ADR-0001](0001-yarn-1-classic-as-package-manager.md).

## Scope

All JavaScript/TypeScript packages in this monorepo. Currently:
`pi-remote-ext/` (Yarn workspace), `package.json` at the monorepo root
(the pi-package entry point + workspace orchestrator).

## Context

ADR-0001 selected Yarn 1.22 Classic on the rationale that "the operator
preferred Yarn" during Phase-0 bootstrap, without distinguishing Classic
from Berry. The operator's actual preference, documented in another
project (`noyesFamilyMedia/docs/decisions/0001-package-management.md`),
is Yarn 4 Berry — specifically for the dependency-management tools it
makes available that Classic does not.

Concurrently, ADR-0001 also did not specify a monorepo layout. The
bootstrap landed all extension dependencies in the root `package.json`,
which conflated the pi-package entry point with the extension's
dependency-bearing manifest. Phase 1 work made it clear that the
extension's deps should live next to the extension's code.

These two threads are coupled. Yarn Berry's workspace model is the
mechanism through which the extension's deps can live in
`pi-remote-ext/package.json` while pi's git-install path still finds a
manifest at the monorepo root. They have to be decided together.

## Decisions

### Yarn 4 (Berry) over Yarn 1 Classic, npm, pnpm, and bun

Berry's `packageExtensions` and first-class `resolutions` give us the
tools to address upstream packaging defects deliberately. Classic and npm
lack the former; pnpm's symlink layout creates friction with some build
tooling; bun's lockfile churn is unsuitable for a deterministic-CI
posture. Yarn Berry's contribution-graph is healthy and its release
cadence is predictable.

### `nodeLinker: node-modules`, not PnP

PnP imposes resolution rules that not all of our toolchain understands
(Vitest, Biome, json-schema-to-typescript, future tooling). The
`node-modules` linker keeps us compatible with the broader ecosystem at
the cost of some install size. If a concrete pain point ever motivates
PnP, that is a follow-up ADR.

### Pin every dependency to an exact version

Supply-chain attacks routinely propagate through caret ranges. Exact
pinning means every version change is a reviewable diff.
`defaultSemverRangePrefix: ""` in `.yarnrc.yml` enforces this for future
`yarn add`. The lockfile remains the canonical record, but exact pins in
`package.json` mean a misbehaving lockfile-regen does not silently bump
direct deps.

### Pin to the current floor when bumping majors

When a major-version bump is required, pick the latest patch of the
lowest acceptable major rather than the absolute newest minor/patch.
Smaller blast radius, fewer surprises.

### Layered overrides: `packageExtensions` for missing peers, `resolutions` for wrong transitives

`packageExtensions` is add-only and harmless — use it freely to declare
peers that upstreams forgot. `resolutions` overrides what dependency
authors asked for — use it sparingly and only when an upstream's
published metadata is actually wrong. Every `resolutions` entry must be
documented in a sibling `"//"` comment in `package.json` or in
`docs/package-management.md`.

### Zero peer-dep warnings as a merge gate

`yarn explain peer-requirements | grep ✘` must be empty before commit.
Tolerated warnings accumulate and hide real problems.

### Monorepo workspace layout

The root `package.json` carries no `dependencies` or `devDependencies`.
It exists to declare:

- The `pi` manifest (`pi.extensions: ["./pi-remote-ext/src/index.ts"]`),
  which makes the monorepo installable as a Pi package via
  `pi install git:github.com/TheTechChild/pi-remote`.
- The `workspaces` field (`["pi-remote-ext"]`), which Yarn (and npm,
  during `pi install`) use to find and install the workspace's deps.
- Top-level orchestration scripts (`yarn build`, `yarn test`, …) that
  delegate to the workspace.

The extension's actual deps live in `pi-remote-ext/package.json`. Adding
a future JS workspace (e.g., a v2 admin UI) is just adding an entry to
the root `workspaces` array.

### `pi install` via the monorepo URL

The canonical install command is:

```sh
pi install git:github.com/TheTechChild/pi-remote
```

Pi clones the repo, runs `npm install` at the root (pi explicitly uses
npm regardless of which JS package manager the project uses), and reads
the root `pi.extensions` manifest. npm understands the `workspaces`
field and installs the workspace's deps. The pi runtime resolves
`./pi-remote-ext/src/index.ts` and its imports through the resulting
`node_modules/` tree.

This means our `yarn.lock` is consulted by developers and CI but **not**
by end users at install time. The implication is documented in
`docs/package-management.md`: direct deps are exact-pinned (so end users
get exactly those versions for direct deps), but transitive resolution
at `pi install` time is npm's, not yarn's, and is not pinned by
`yarn.lock`. This is the same situation as before this ADR; the layout
change does not regress it.

## Consequences

### Positive

- Single canonical lockfile at the monorepo root.
- Strict dependency hygiene: exact pinning, layered overrides, zero
  peer-warnings gate.
- The extension's deps live with the extension's code; the root
  manifest's role is small and clear.
- Future JS packages slot into the workspace pattern without
  relitigating the package-manager choice.

### Negative

- Contributors who prefer npm or pnpm still have to use Yarn for the
  lockfile to remain stable. CI installs with `yarn install --immutable`
  and rejects PRs with lockfile drift.
- Yarn 4's CLI ergonomics differ from Classic's; contributors familiar
  with Classic need to learn the small set of changed commands
  (`--immutable` over `--frozen-lockfile`, `yarn workspaces foreach`,
  `yarn explain peer-requirements`). The README and
  `docs/package-management.md` document these.
- `pi install` runs npm, not yarn — end-user transitive resolution is
  not pinned by our lockfile. This is a property of how pi installs git
  sources and is outside this ADR's authority. Direct-dep exact pinning
  bounds the supply-chain exposure to authoring time.

### Neutral

- `package.json` `packageManager` field pins Yarn 4 via Corepack
  (`packageManager: "yarn@4.x.y"`), which contributors with Corepack
  enabled pick up automatically.

## Errata against SPEC.md

This ADR is the source of truth for SPEC.md drift in the following
places, all corrected in the commit that landed this ADR:

- **§ 5** previously said "Pi Remote does not need to be a monorepo —
  one repo per component." The repo as built is a monorepo. § 5 has
  been updated to reflect the as-built layout.
- **§ 22.1** previously listed the Pi extension API package as
  `@mariozechner/pi-coding-agent`. The package has been renamed
  upstream to `@earendil-works/pi-coding-agent`; § 22.1 has been
  updated, and the actual dep rename is the subject of a separate
  commit per its own review concerns.
- **§ 23.4 Task 2** previously listed the install URL as
  `pi install git:github.com/TheTechChild/pi-remote-ext`. The
  canonical URL is the monorepo URL above.
- **§ D21** previously described codegen as "clones or updates the
  pi-remote-spec repo at a pinned commit (recorded in
  `scripts/spec-version.txt`)." Because the spec lives in this
  monorepo, the codegen scripts read from `pi-remote-spec/protocol/`
  directly with no clone and no pin. § D21 has been updated;
  `scripts/spec-version.txt` references throughout § 23.4 have been
  dropped.

## References

- ADR-0001 (superseded) — initial Yarn 1.22 Classic decision.
- noyesFamilyMedia ADR-0001 — precedent for the Berry + exact-pinning +
  layered-overrides philosophy mirrored here.
- Pi packages docs:
  `/Users/clayton.noyes/.nvm/versions/node/v22.18.0/lib/node_modules/@earendil-works/pi-coding-agent/docs/packages.md`
  — confirms pi runs `npm install` at the clone root for git sources.
- `docs/package-management.md` — the operational manual: rules,
  layered-override mechanics, upgrade procedure, current overrides.
- SPEC.md §§ 5, 22.1, 23.4, D21, D25.
