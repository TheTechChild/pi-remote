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
// extension instance. Field set is per SPEC.md § 7.5; some fields
// (LastSeq, AttachedClients) are not yet read in M1 but are present so the
// type stays stable across milestones.
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
	LastSeq         uint64
	State           SessionState
	AttachedClients map[string]bool
}
