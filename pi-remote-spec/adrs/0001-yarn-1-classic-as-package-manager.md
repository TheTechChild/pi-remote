# ADR-0001: Yarn 1.22 (classic) as the JavaScript package manager

## Status

**Superseded by [ADR-0005](0005-yarn-berry-monorepo.md) (2026-05-12).**

Accepted: 2026-05-07. Superseded: 2026-05-12.

The body below is preserved as the original Phase-0 decision record. ADR-0005
is the authoritative current statement on JS package management; it adopts
Yarn 4 Berry, exact-version pinning, and the monorepo workspace layout.

## Context

SPEC.md § 23.4 (Task 2) lists the acceptance criterion as
`npm install && npm run build && npm test succeeds on a clean clone`. § 22.1
specifies the libraries (vitest, biome, json-schema-to-typescript, etc.) but
does not pin a JavaScript package manager.

During Phase-0 bootstrap, the bootstrapping environment had `yarn` available
but `npm` access constrained, and the operator preferred Yarn. Choosing the
package manager up-front is a one-way decision: lockfile format, CI cache key,
and contributor onboarding all change with the choice.

## Decision

The `pi-remote-ext` repository uses **Yarn 1.22 (classic)** as its package
manager.

- `yarn.lock` is committed.
- `package.json` does not set `packageManager` (`yarn@1` predates that field
  and Corepack-pinning Yarn 1 produces no benefit).
- `package.json` scripts (`build`, `test`, `lint`, `typecheck`, `codegen`) are
  package-manager neutral — they can be run with `npm run <script>` if a
  contributor prefers, but the lockfile is the Yarn one.
- CI installs with `yarn install --frozen-lockfile`.

The acceptance text in SPEC.md § 23.4 should read as
`yarn install && yarn build && yarn test` for that repo. Either invocation
works for someone running it locally; the spec wording is not normatively
literal.

## Consequences

**Positive:**
- Single canonical lockfile; deterministic CI cache keys.
- Faster cold installs than npm in many cases (Yarn 1's parallel resolver).

**Negative:**
- Contributors who prefer npm or pnpm still have to use Yarn for the lockfile
  to remain stable.
- Yarn 1.22 is in maintenance mode upstream; a future ADR may switch to
  Yarn 4 (Berry) or pnpm if a concrete pain point emerges.

## References

- SPEC.md §§ 22.1, 23.4 (Task 2).
- Yarn 1.x docs: https://classic.yarnpkg.com/
