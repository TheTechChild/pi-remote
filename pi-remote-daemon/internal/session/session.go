// SPDX-License-Identifier: MIT
package session

import "time"

// SessionState mirrors SPEC.md § 12.1. M1 only ever sets `running` (initial)
// and `ended` (on connection close without disconnect). Other states land in
// later milestones (M3 unresponsive via heartbeat timeout, M7 paused via
// suspend detection, agent_start/agent_end driving running<->idle).
type SessionState string

const (
	StateRunning      SessionState = "running"
	StateIdle         SessionState = "idle"
	StatePaused       SessionState = "paused"
	StateUnresponsive SessionState = "unresponsive"
	StateEnded        SessionState = "ended"
)

// Session is the daemon-side join of one tmux pane, one Pi process, and one
// extension instance. Field set is per SPEC.md § 7.5 with two M3+M4
// additions:
//
//   - Seq: the per-session monotonic counter feeding upstream frames.
//     Zero value is usable (see seq.go).
//   - EndedAt: timestamp captured when MarkEnded / RemoveWithReason flips
//     the session out of the live set. Unblocks the reaper (#43) without
//     forcing it to scan for end transitions on a tick. Strictly
//     informational in this batch.
type Session struct {
	SessionID       string
	SpawnToken      string
	TmuxTarget      string
	PID             int
	CWD             string
	ProjectName     string
	Hostname        string
	Model           string
	StartedAt       time.Time
	LastHeartbeat   time.Time
	EndedAt         time.Time
	Seq             SeqAllocator
	LastSeq         uint64
	State           SessionState
	AttachedClients map[string]bool
}
