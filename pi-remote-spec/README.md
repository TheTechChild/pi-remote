# pi-remote-spec

Authoritative specification, wire-protocol JSON Schemas, fixtures, scenarios,
and architectural decision records for the **Pi Remote** project.

## Overview

Pi Remote is a session-sharing system for the [Pi](https://pi.dev) coding
agent. It lets coding machines publish their live Pi sessions to a coordinator
running on UnraidOS, and lets Android devices attach to those sessions for
monitoring, push-notification wake-ups, and bidirectional interaction.

This repository is the **source of truth** consumed by every component
implementation — the schemas in [`protocol/`](protocol/) drive code generation
in each language-specific repo.

## Spec

The full technical specification lives in [`SPEC.md`](SPEC.md).

## Component repositories

| Component | Repo |
|-----------|------|
| Pi extension (TypeScript) | [`pi-remote-ext`](https://github.com/TheTechChild/pi-remote-ext) |
| Per-machine daemon (Go) | [`pi-remote-daemon`](https://github.com/TheTechChild/pi-remote-daemon) |
| Coordinator (Go) | [`pi-remote-coordinator`](https://github.com/TheTechChild/pi-remote-coordinator) |
| Android app (Kotlin) | [`pi-remote-android`](https://github.com/TheTechChild/pi-remote-android) |

## Repository layout

```
pi-remote-spec/
├── SPEC.md                # Full technical spec (v1.1)
├── protocol/              # JSON Schema files (Draft 2020-12)
│   ├── extension-daemon/  # Unix-socket leg (§ 10.1)
│   ├── daemon-coordinator/# WebSocket leg (§ 10.2)
│   ├── coordinator-app/   # WebSocket leg (§ 10.3)
│   └── push/              # Push payload schema (§ 10.4)
├── fixtures/              # Canonical message exchanges as .jsonl
├── scenarios/             # End-to-end flow narratives
├── errors/                # Error code catalog
└── adrs/                  # Architecture Decision Records
```

## Setup

Phase-0 tooling is intentionally minimal. CI runs `check-jsonschema` and
`ajv` validation against everything in `protocol/` and `fixtures/`.

## Build

There is nothing to build in this repo — it is documents and schemas.

## Test

CI validates that:

- every file in `protocol/` is a syntactically valid Draft 2020-12 JSON Schema
- every record in each `fixtures/*.jsonl` file validates against the schema
  for its `type` field

## License

MIT — see [LICENSE](LICENSE).
