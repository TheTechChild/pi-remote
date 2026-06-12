#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
#
# Per-user macOS installer for pi-remote-daemon (SPEC.md §§ 7.3, 7.4, D11).
#
# Usage:
#   ./install-macos.sh                       # interactive prompts
#   PI_REMOTE_COORDINATOR_URL=wss://... \
#   PI_REMOTE_SERVICE_TOKEN_ID=... \
#   PI_REMOTE_SERVICE_TOKEN_SECRET=... \
#   ./install-macos.sh                       # non-interactive
#
# Lays down:
#   /usr/local/bin/pi-remote-daemon                      (binary, 0755)
#   ~/.config/pi-remote/daemon.toml                      (config; kept if present)
#   ~/.config/pi-remote/service_token_id                 (0600)
#   ~/.config/pi-remote/service_token_secret             (0600)
#   ~/Library/LaunchAgents/dev.pi-remote.daemon.plist    (launchd agent)
#
# machine_id is NOT written here: the daemon generates a UUIDv7 on first
# run and persists it to ~/.local/state/pi-remote/machine_id (SPEC § 7.3).

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"
CONFIG_DIR="${HOME}/.config/pi-remote"
PLIST_SRC="${SCRIPT_DIR}/launchd/dev.pi-remote.daemon.plist"
PLIST_DST="${HOME}/Library/LaunchAgents/dev.pi-remote.daemon.plist"
BIN_DST="/usr/local/bin/pi-remote-daemon"

say() { printf '==> %s\n' "$*"; }

# --- 1. Build ---------------------------------------------------------------
say "Building pi-remote-daemon"
( cd "${REPO_DIR}" && go build -o /tmp/pi-remote-daemon ./cmd/pi-remote-daemon )

# --- 2. Install binary ------------------------------------------------------
say "Installing ${BIN_DST}"
if [ -w "$(dirname "${BIN_DST}")" ]; then
  install -m 0755 /tmp/pi-remote-daemon "${BIN_DST}"
else
  sudo install -m 0755 /tmp/pi-remote-daemon "${BIN_DST}"
fi
rm -f /tmp/pi-remote-daemon

# --- 3. Gather operator inputs ----------------------------------------------
COORDINATOR_URL="${PI_REMOTE_COORDINATOR_URL:-}"
TOKEN_ID="${PI_REMOTE_SERVICE_TOKEN_ID:-}"
TOKEN_SECRET="${PI_REMOTE_SERVICE_TOKEN_SECRET:-}"

if [ -z "${COORDINATOR_URL}" ]; then
  read -r -p "Coordinator WebSocket URL (e.g. wss://pi-remote.example.com/v1/daemon): " COORDINATOR_URL
fi
if [ -z "${TOKEN_ID}" ]; then
  read -r -p "CF service-token ID: " TOKEN_ID
fi
if [ -z "${TOKEN_SECRET}" ]; then
  read -r -s -p "CF service-token secret (input hidden): " TOKEN_SECRET; echo
fi

# --- 4. Credentials (0600, outside the config per D13) -----------------------
say "Provisioning credentials in ${CONFIG_DIR}"
mkdir -p "${CONFIG_DIR}"
umask 077
printf '%s\n' "${TOKEN_ID}"     > "${CONFIG_DIR}/service_token_id"
printf '%s\n' "${TOKEN_SECRET}" > "${CONFIG_DIR}/service_token_secret"
umask 022

# --- 5. Config (kept if already present) -------------------------------------
if [ -f "${CONFIG_DIR}/daemon.toml" ]; then
  say "Keeping existing ${CONFIG_DIR}/daemon.toml (delete it to regenerate)"
else
  say "Writing ${CONFIG_DIR}/daemon.toml"
  cat > "${CONFIG_DIR}/daemon.toml" <<EOF
# pi-remote daemon configuration (SPEC.md § 7.3).
# machine_id is generated on first run; set it here only to override.
machine_display_name = "$(scutil --get ComputerName 2>/dev/null || hostname -s)"

[coordinator]
url = "${COORDINATOR_URL}"
service_token_id_file = "${CONFIG_DIR}/service_token_id"
service_token_secret_file = "${CONFIG_DIR}/service_token_secret"

[socket]
path = "~/.pi-remote/daemon.sock"

[tmux]
binary = "tmux"
session_prefix = "pi-remote-"

[logging]
level = "info"
file = "~/.pi-remote/daemon.log"
EOF
fi

# --- 6. launchd agent ---------------------------------------------------------
say "Installing launchd agent ${PLIST_DST}"
mkdir -p "${HOME}/Library/LaunchAgents" "${HOME}/.pi-remote"
sed "s|{{HOME}}|${HOME}|g" "${PLIST_SRC}" > "${PLIST_DST}"
launchctl unload "${PLIST_DST}" 2>/dev/null || true
launchctl load -w "${PLIST_DST}"

say "Done. Logs: ${HOME}/.pi-remote/daemon.err.log"
say "Status: launchctl list | grep dev.pi-remote.daemon"
