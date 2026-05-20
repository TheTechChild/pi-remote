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

const ReasonTmuxServerLost = "tmux_server_lost"

// Session is the daemon-side join of one tmux pane, one Pi process, and one
// extension instance. Field set is per SPEC.md § 7.5 with one M3+M4
// addition:
//
//   - EndedAt: timestamp captured when MarkEnded / RemoveWithReason flips
//     the session out of the live set. Unblocks the reaper (#43) without
//     forcing it to scan for end transitions on a tick. Strictly
//     informational in this batch.
//
// The per-session SeqAllocator lives in the multiplex (the only consumer
// of allocated seqs), not on the Session struct. Embedding a
// sync/atomic.Uint64 here would make Session no-copy and force every
// reader (Get, Snapshot, frame helpers, tests) onto pointer plumbing;
// the multiplex's parallel map[sessionID]*SeqAllocator is the cleaner
// boundary.
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
	LastSeq         uint64
	State           SessionState
	AttachedClients map[string]bool
}
