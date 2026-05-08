#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
#
# Codegen wire-protocol TypeScript types from the monorepo's
# pi-remote-spec/protocol/ tree (no clone, no pin — the spec lives in this
# repo).
#
# Output: pi-remote-ext/src/proto/<leg>/<message-type>.ts with the GENERATED
# header per SPEC.md § D1. Generated files are committed (per § D25).

set -euo pipefail

ext_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
repo_root=$(cd -- "${ext_dir}/.." && pwd)
spec_protocol="${repo_root}/pi-remote-spec/protocol"
proto_out="${ext_dir}/src/proto"

if [[ ! -d "${spec_protocol}" ]]; then
  echo "spec protocol dir not found: ${spec_protocol}" >&2
  exit 1
fi

if ! command -v json2ts >/dev/null 2>&1; then
  echo "json2ts not on PATH; install dev deps with 'yarn install' first" >&2
  exit 1
fi

rm -rf "${proto_out}"
mkdir -p "${proto_out}"

while IFS= read -r -d '' schema; do
  rel=${schema#"${spec_protocol}/"}
  out="${proto_out}/${rel%.json}.ts"
  mkdir -p "$(dirname "${out}")"
  json2ts \
    --no-additionalProperties \
    --bannerComment "// SPDX-License-Identifier: MIT
// GENERATED — DO NOT EDIT. Source: pi-remote-spec/protocol/${rel}" \
    --input "${schema}" \
    --output "${out}"
done < <(find "${spec_protocol}" -type f -name '*.json' -print0)

echo "Codegen complete: $(find "${proto_out}" -type f -name '*.ts' | wc -l | tr -d ' ') files written to ${proto_out}"
