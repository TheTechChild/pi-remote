#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
#
# Installer skeleton for macOS. Real implementation lands in a follow-up
# milestone alongside the suspend-detection work.

set -euo pipefail

cat <<'EOF'
TODO: implement macOS install:
  1. Build pi-remote-daemon binary (`go build ./cmd/pi-remote-daemon`).
  2. Copy to /usr/local/bin/pi-remote-daemon (chmod 0755).
  3. Materialize deploy/launchd/dev.pi-remote.daemon.plist with $HOME substituted.
  4. Copy plist to ~/Library/LaunchAgents/dev.pi-remote.daemon.plist.
  5. Provision /etc/pi-remote/{service_token_id,service_token_secret} (D13).
  6. launchctl load -w ~/Library/LaunchAgents/dev.pi-remote.daemon.plist.
EOF
