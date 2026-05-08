#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
#
# Installer skeleton for Linux. Real implementation lands in a follow-up
# milestone alongside the suspend-detection work.

set -euo pipefail

cat <<'EOF'
TODO: implement Linux install:
  1. Build pi-remote-daemon binary (`go build ./cmd/pi-remote-daemon`).
  2. Copy to /usr/local/bin/pi-remote-daemon (chmod 0755).
  3. Copy deploy/systemd/pi-remote-daemon.service to ~/.config/systemd/user/.
  4. Provision /etc/pi-remote/{service_token_id,service_token_secret} (D14).
  5. systemctl --user daemon-reload && systemctl --user enable --now pi-remote-daemon.service.
EOF
