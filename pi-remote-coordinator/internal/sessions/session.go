// SPDX-License-Identifier: MIT

// Package sessions holds the coordinator-side registry of live Pi sessions.
// This phase implements only the in-memory bookkeeping needed by the daemon
// and client WebSocket handlers (SPEC.md § 8.4). The per-session ring
// buffer (Session.Ring) and broker fan-out (Session.AttachedClients) are
// added in Batch 3 (M3/M4).
package sessions

import (
	"time"

	daemon_coordinator "github.com/TheTechChild/pi-remote-coordinator/internal/proto/daemon-coordinator"
)

// Session is the coordinator's view of one Pi process running on a daemon.
// See SPEC.md § 8.4. Workstream C scope: no Ring, no AttachedClients — those
// land with the broker in M3/M4.
type Session struct {
	SessionID   string
	MachineID   string
	Metadata    daemon_coordinator.SessionStartedJsonMetadata
	State       string // one of: "running", "idle", "paused", "unresponsive", "ended"
	LastSeq     int
	Ended       bool
	EndedAt     time.Time
	LastTouched time.Time
}
