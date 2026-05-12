# Package management

This document is the operational manual for the JavaScript half of the
monorepo. The decision rationale lives in
[`pi-remote-spec/adrs/0005-yarn-berry-monorepo.md`](../pi-remote-spec/adrs/0005-yarn-berry-monorepo.md);
this file covers the *how*.

## Overview

The monorepo uses **Yarn 4 (Berry)** with the **`node-modules`** linker.
JavaScript code lives in workspace members; currently the only workspace
member is [`pi-remote-ext/`](../pi-remote-ext/). The root `package.json`
exists to declare the pi-package manifest, the workspace list, and
orchestration scripts; it carries no `dependencies` or `devDependencies`
of its own.

The Yarn version is pinned via the `packageManager` field in the root
`package.json` and activated through Corepack (`corepack enable`).

## Layout

```
pi-remote/
├── package.json                # name, workspaces: ["pi-remote-ext"],
│                               #   pi: { extensions: [...] },
│                               #   packageManager: "yarn@4.x.y",
│                               #   no dependencies / devDependencies
├── yarn.lock                   # single lockfile for the entire workspace
├── .yarnrc.yml                 # nodeLinker, defaultSemverRangePrefix, ...
├── AGENTS.md                   # one-screen pointer at this file
├── docs/
│   └── package-management.md   # this file
├── pi-remote-spec/
│   └── adrs/
│       ├── 0001-yarn-1-classic-as-package-manager.md   # superseded
│       └── 0005-yarn-berry-monorepo.md                 # current
└── pi-remote-ext/
    ├── package.json            # actual ext deps, exact-pinned
    ├── biome.json
    ├── tsconfig.json
    ├── vitest.config.ts
    └── src/, test/, scripts/
```

## Core rules

- All deps are **pinned to exact versions** (no `^`, no `~`).
  `defaultSemverRangePrefix: ""` in `.yarnrc.yml` enforces this for
  future `yarn add`.
- The lockfile is committed at the monorepo root.
- In CI and Docker, use `yarn install --immutable` (the Berry equivalent
  of Classic's `--frozen-lockfile`). The build fails if the lockfile
  would change.
- When bumping deps, prefer the latest patch within the current major to
  minimize supply-chain risk. When a major bump is required, pin to the
  current floor (lowest acceptable major's latest patch), not the
  absolute newest.
- After **any** dependency change, run `yarn explain peer-requirements`
  and verify zero `✘` lines before committing. This is a merge gate.

## Layered pinning strategy

The project uses four mechanisms in concert. Each has a specific role;
don't reach for a heavier tool when a lighter one suffices.

1. **`dependencies` / `devDependencies`** in `pi-remote-ext/package.json`
   — exact versions for everything the workspace's code directly imports
   or its toolchain directly uses. The roots of the resolution graph.
2. **`defaultSemverRangePrefix: ""`** in `.yarnrc.yml` — authoring
   guardrail. Future `yarn add foo` writes `"foo": "1.2.3"`, not
   `"foo": "^1.2.3"`.
3. **`packageExtensions`** in `.yarnrc.yml` — patches **missing**
   peer-dep declarations in upstream packages. **Add-only**: cannot
   replace existing entries. Use when an upstream forgot to declare a
   peer that you're already installing. Each entry must pin to the same
   exact version you ship as a direct dep.
4. **`resolutions`** in `pi-remote-ext/package.json` (or the workspace
   where the bad transitive appears) — forcibly substitutes a specific
   version of any package anywhere in the tree, overriding what the dep
   authors requested. The heaviest hammer. Use only when a transitive's
   declared metadata is **wrong** (not just missing) and
   `packageExtensions` can't help. Each entry must have a comment
   explaining why it exists and what would let you remove it (a sibling
   `"//"` key in `package.json`, or a line in this file's "Current
   overrides" section).

Order of operations during install: `packageExtensions` patches
manifests → solver runs → `resolutions` overrides solver picks →
peer-dep validation emits `✘` warnings if anything is still inconsistent.

## Current overrides

None at present. When the first `packageExtensions` or `resolutions`
entry is added, document it here with: which package, what was wrong
upstream, what we pinned it to, and the condition under which we can
remove the override.

## Upgrade procedure

When upgrading any pinned dep:

1. Update the exact version in `pi-remote-ext/package.json` (or
   whichever workspace owns it).
2. Update any matching version in `.yarnrc.yml` `packageExtensions` or
   the workspace's `resolutions` (if they reference the dep being
   bumped).
3. `yarn install` and inspect the diff in `yarn.lock`. Expect mostly
   the bumped package and its transitives; large lockfile churn means
   something else is loose.
4. `yarn explain peer-requirements | grep ✘` must be empty.
5. Run the workspace's full check pipeline:
   ```sh
   yarn typecheck
   yarn lint
   yarn test
   yarn build
   ```
6. Commit `package.json` and `yarn.lock` together. The lockfile is part
   of the dep change, not a separate cleanup commit.

## Pi-runtime peer dependencies

Pi-runtime packages — `@earendil-works/pi-coding-agent`,
`@earendil-works/pi-agent-core`, `@earendil-works/pi-ai`,
`@earendil-works/pi-tui`, `typebox` — are provided by the pi runtime at
load time. Per [pi's packages documentation](https://github.com/earendil-works/pi-mono#packages),
they should be declared as `peerDependencies` with `"*"` and **not**
bundled in the package's `dependencies`.

**Foot-gun:** with `peerDependencies: "*"`, yarn does not install the
package locally. If a source file in `pi-remote-ext/src/` adds an
`import` from one of these peers, local builds and tests will fail to
resolve the import unless the package is also listed in
`devDependencies` (at the same version the pi runtime ships). When
introducing a new import from a pi-runtime peer, add it as both a peer
(`"*"`) and a devDependency (exact version matching whatever the
currently-released pi runtime ships) in the same change. See the
"Upgrade procedure" above for how to keep these in sync when the pi
runtime is bumped.

## End-user install behavior

`pi install git:github.com/TheTechChild/pi-remote` does not use yarn.
Pi clones the monorepo and runs `npm install` at the clone root. npm
understands the `workspaces` field and installs the workspace's deps,
hoisting them to the root `node_modules/` tree. The pi runtime then
resolves `./pi-remote-ext/src/index.ts` and its imports through that
tree.

**Implication for `yarn.lock`:** end users at `pi install` time do not
consult `yarn.lock`. They get whatever npm resolves from the
`package.json` constraints alone. Because direct deps are exact-pinned,
direct dep versions are stable; transitive resolution is npm's, and is
not pinned by our lockfile. This is the same situation as before the
workspace move; the layout did not regress it. The bounded supply-chain
exposure is at authoring time (when we add or bump a direct dep, the
diff is reviewable in `package.json`).

## See also

- [ADR-0005: Yarn 4 (Berry) with workspaces; monorepo layout](../pi-remote-spec/adrs/0005-yarn-berry-monorepo.md)
- [ADR-0001: Yarn 1.22 (classic) (superseded)](../pi-remote-spec/adrs/0001-yarn-1-classic-as-package-manager.md)
- [AGENTS.md](../AGENTS.md)
- Pi packages docs: see `@earendil-works/pi-coding-agent`'s `docs/packages.md`.
