#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
#
# Codegen wire-protocol Go types from the monorepo's pi-remote-spec/protocol/
# tree (no clone, no pin).
#
# Output: pi-remote-daemon/internal/proto/<leg>/<message-type>.go with the
# GENERATED + SPDX headers per SPEC.md §§ D1, D22. Generated files are
# committed (per § D25).

set -euo pipefail

daemon_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
repo_root=$(cd -- "${daemon_dir}/.." && pwd)
spec_protocol="${repo_root}/pi-remote-spec/protocol"
proto_out="${daemon_dir}/internal/proto"
bin_dir="${repo_root}/.codegen-bin"

if [[ ! -d "${spec_protocol}" ]]; then
  echo "spec protocol dir not found: ${spec_protocol}" >&2
  exit 1
fi

if ! command -v go-jsonschema >/dev/null 2>&1; then
  echo "installing github.com/atombender/go-jsonschema..." >&2
  GOBIN="${bin_dir}" go install github.com/atombender/go-jsonschema@latest
  export PATH="${bin_dir}:${PATH}"
fi

rm -rf "${proto_out}"
mkdir -p "${proto_out}"

while IFS= read -r -d '' schema; do
  rel=${schema#"${spec_protocol}/"}
  leg=$(dirname "${rel}")
  base=$(basename "${rel}" .json)
  out_dir="${proto_out}/${leg}"
  out="${out_dir}/${base}.go"
  pkg=$(echo "${leg}" | tr '-' '_')
  mkdir -p "${out_dir}"
  go-jsonschema \
    --package "${pkg}" \
    --tags json \
    --output "${out}" \
    "${schema}"
  tmp=$(mktemp)
  {
    printf '// SPDX-License-Identifier: MIT\n'
    printf '// GENERATED — DO NOT EDIT. Source: pi-remote-spec/protocol/%s\n\n' "${rel}"
    cat "${out}"
  } >"${tmp}"
  mv "${tmp}" "${out}"
done < <(find "${spec_protocol}" -type f -name '*.json' -print0)

echo "Codegen complete: $(find "${proto_out}" -type f -name '*.go' | wc -l | tr -d ' ') files written to ${proto_out}"
