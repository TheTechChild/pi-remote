#!/usr/bin/env python3
# SPDX-License-Identifier: MIT
"""Validate every record in fixtures/ against its corresponding protocol schema.

Each line of every `.jsonl` file is validated against the protocol schemas
(under `protocol/**/*.json`) whose `properties.type.const` equals the record's
`type` field. Some message types are defined on more than one leg
(e.g., `session_started` exists on both daemon->coordinator and
coordinator->app); the record is considered valid if **at least one** schema
with that type accepts it.

The standalone `push-payload-plaintext.json` file is validated against
`protocol/push/push_payload.json` directly.

Failures print a useful pointer (file:line, JSON path, message) and exit 1.
"""

from __future__ import annotations

import json
import sys
from pathlib import Path
from typing import Any

from jsonschema import Draft202012Validator


REPO_ROOT = Path(__file__).resolve().parent.parent
PROTOCOL_DIR = REPO_ROOT / "protocol"
FIXTURES_DIR = REPO_ROOT / "fixtures"


def load_schemas_by_type() -> tuple[dict[str, list[dict[str, Any]]], int]:
    """Return mapping of message `type` -> list of schemas accepting that type.

    Some message types appear on more than one leg (daemon->coordinator and
    coordinator->app); a record validates if any schema with that type
    accepts it.
    """
    by_type: dict[str, list[dict[str, Any]]] = {}
    schema_count = 0
    for path in sorted(PROTOCOL_DIR.rglob("*.json")):
        with path.open("r", encoding="utf-8") as fh:
            schema = json.load(fh)
        schema_count += 1
        type_value = (
            schema.get("properties", {}).get("type", {}).get("const")
        )
        if type_value is None:
            continue
        by_type.setdefault(type_value, []).append(schema)
    return by_type, schema_count


def validate_record(
    record: dict[str, Any], schemas: list[dict[str, Any]]
) -> list[str]:
    """Return [] if any schema accepts the record, else the error list from
    whichever schema produced the fewest errors (most informative diagnosis)."""
    best_errors: list[str] | None = None
    for schema in schemas:
        validator = Draft202012Validator(schema)
        errors = sorted(validator.iter_errors(record), key=lambda e: e.path)
        if not errors:
            return []
        formatted = [
            (
                f"schema={schema.get('$id', '<no-id>')} "
                f"path={'/'.join(str(p) for p in err.absolute_path) or '<root>'}: "
                f"{err.message}"
            )
            for err in errors
        ]
        if best_errors is None or len(formatted) < len(best_errors):
            best_errors = formatted
    return best_errors or []


def validate_jsonl(
    path: Path, schemas: dict[str, list[dict[str, Any]]]
) -> int:
    failures = 0
    with path.open("r", encoding="utf-8") as fh:
        for lineno, raw in enumerate(fh, start=1):
            raw = raw.strip()
            if not raw:
                continue
            try:
                record = json.loads(raw)
            except json.JSONDecodeError as exc:
                print(f"{path}:{lineno}: invalid JSON: {exc}", file=sys.stderr)
                failures += 1
                continue
            type_value = record.get("type")
            if type_value is None:
                print(
                    f"{path}:{lineno}: record has no `type` field",
                    file=sys.stderr,
                )
                failures += 1
                continue
            candidates = schemas.get(type_value)
            if not candidates:
                print(
                    f"{path}:{lineno}: no protocol schema for "
                    f"type={type_value!r}",
                    file=sys.stderr,
                )
                failures += 1
                continue
            errors = validate_record(record, candidates)
            for err in errors:
                print(f"{path}:{lineno}: {err}", file=sys.stderr)
            if errors:
                failures += 1
    return failures


def validate_push_payload() -> int:
    schema_path = PROTOCOL_DIR / "push" / "push_payload.json"
    fixture_path = FIXTURES_DIR / "push-payload-plaintext.json"
    if not fixture_path.exists():
        return 0
    with schema_path.open("r", encoding="utf-8") as fh:
        schema = json.load(fh)
    with fixture_path.open("r", encoding="utf-8") as fh:
        record = json.load(fh)
    validator = Draft202012Validator(schema)
    errors = sorted(validator.iter_errors(record), key=lambda e: e.path)
    failures = 0
    for err in errors:
        location = "/".join(str(p) for p in err.absolute_path) or "<root>"
        print(
            f"{fixture_path}: path={location}: {err.message}",
            file=sys.stderr,
        )
        failures += 1
    return failures


def main() -> int:
    schemas, schema_count = load_schemas_by_type()
    failures = 0
    for path in sorted(FIXTURES_DIR.glob("*.jsonl")):
        failures += validate_jsonl(path, schemas)
    failures += validate_push_payload()
    if failures:
        print(f"\n{failures} validation failure(s)", file=sys.stderr)
        return 1
    print(
        f"OK: {schema_count} schemas loaded; all fixtures validate"
    )
    return 0


if __name__ == "__main__":
    sys.exit(main())
