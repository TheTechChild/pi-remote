#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
#
# Codegen wire-protocol Kotlin types from the monorepo's pi-remote-spec/protocol/
# tree (no clone, no pin) using the quicktype CLI (per SPEC.md § 22.4).
#
# Output: pi-remote-android/app/src/main/kotlin/dev/pi_remote/android/proto/
# <leg>/<MessageType>.kt with the GENERATED + SPDX header per § D1.

set -euo pipefail

android_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
repo_root=$(cd -- "${android_dir}/.." && pwd)
spec_protocol="${repo_root}/pi-remote-spec/protocol"
proto_out="${android_dir}/app/src/main/kotlin/dev/pi_remote/android/proto"
package="dev.pi_remote.android.proto"

if [[ ! -d "${spec_protocol}" ]]; then
  echo "spec protocol dir not found: ${spec_protocol}" >&2
  exit 1
fi

if ! command -v quicktype >/dev/null 2>&1; then
  echo "quicktype not on PATH; install with 'npm install -g quicktype'" >&2
  exit 1
fi

rm -rf "${proto_out}"
mkdir -p "${proto_out}"

while IFS= read -r -d '' schema; do
  rel=${schema#"${spec_protocol}/"}
  leg=$(dirname "${rel}")
  base=$(basename "${rel}" .json)
  out_dir="${proto_out}/${leg//-/_}"
  out="${out_dir}/$(echo "${base}" | sed 's/_\([a-z]\)/\U\1/g; s/^./\U&/').kt"
  mkdir -p "${out_dir}"
  quicktype \
    --src-lang schema \
    --lang kotlin \
    --framework kotlinx \
    --package "${package}.${leg//-/_}" \
    --out "${out}" \
    "${schema}"
  tmp=$(mktemp)
  {
    printf '// SPDX-License-Identifier: MIT\n'
    printf '// GENERATED — DO NOT EDIT. Source: pi-remote-spec/protocol/%s\n\n' "${rel}"
    cat "${out}"
  } >"${tmp}"
  mv "${tmp}" "${out}"
done < <(find "${spec_protocol}" -type f -name '*.json' -print0)

echo "Codegen complete: $(find "${proto_out}" -type f -name '*.kt' | wc -l | tr -d ' ') files written to ${proto_out}"
