// SPDX-License-Identifier: MIT
package sessions

import (
	"sync"
	"time"

	daemon_coordinator "github.com/TheTechChild/pi-remote-coordinator/internal/proto/daemon-coordinator"
)

// Registry is an in-memory map of sessions, safe for concurrent use. The
// coordinator holds a single Registry for its lifetime; entries are never
// reaped in Workstream C (the broker GC adds that later — SPEC.md § 18.3).
type Registry struct {
	mu sync.RWMutex
	m  map[string]*Session

	now func() time.Time // injectable for tests; defaults to time.Now
}

// NewRegistry constructs an empty Registry.
func NewRegistry() *Registry {
	return &Registry{
		m:   make(map[string]*Session),
		now: time.Now,
	}
}

// Register creates or replaces a session entry. State defaults to "running",
// LastSeq to 0, Ended to false. Caller may follow up with RestoreLastSeq for
// the resume path.
func (r *Registry) Register(sessionID, machineID string, metadata daemon_coordinator.SessionStartedJsonMetadata) *Session {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := r.now()
	s := &Session{
		SessionID:   sessionID,
		MachineID:   machineID,
		Metadata:    metadata,
		State:       "running",
		LastSeq:     0,
		Ended:       false,
		LastTouched: now,
	}
	r.m[sessionID] = s
	return s
}

// Get returns a snapshot of the session and ok=true, or (nil, false) if
// not present. The returned pointer is a copy: mutating it does not affect
// the registry, and reading it is safe without holding any lock. This
// matters under -race because the HTTP read loop writes Session fields
// from a different goroutine than the test that asserts on them.
func (r *Registry) Get(sessionID string) (*Session, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	s, ok := r.m[sessionID]
	if !ok {
		return nil, false
	}
	snap := *s
	return &snap, true
}

// AdvanceSeq sets LastSeq = max(LastSeq, seq). Returns true if LastSeq
// advanced (i.e. seq > previous LastSeq); false if seq was stale or equal.
// The handler logs stale frames at debug level — we don't surface them as
// errors per the plan.
func (r *Registry) AdvanceSeq(sessionID string, seq int) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.m[sessionID]
	if !ok {
		return false
	}
	if seq > s.LastSeq {
		s.LastSeq = seq
		s.LastTouched = r.now()
		return true
	}
	return false
}

// SetState updates State on a known session. No-op if unknown.
func (r *Registry) SetState(sessionID, state string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if s, ok := r.m[sessionID]; ok {
		s.State = state
		s.LastTouched = r.now()
	}
}

// MarkEnded sets Ended=true and stamps EndedAt. The session is retained in
// the registry (no reaper in Workstream C).
func (r *Registry) MarkEnded(sessionID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if s, ok := r.m[sessionID]; ok {
		s.Ended = true
		s.EndedAt = r.now()
		s.LastTouched = s.EndedAt
		s.State = "ended"
	}
}

// PauseAllForMachine flips every non-ended session belonging to machineID
// to State="paused". Ended sessions are skipped (they retain their state).
// Called on machine_suspending and on daemon socket close.
func (r *Registry) PauseAllForMachine(machineID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := r.now()
	for _, s := range r.m {
		if s.MachineID != machineID {
			continue
		}
		if s.Ended {
			continue
		}
		s.State = "paused"
		s.LastTouched = now
	}
}

// RestoreLastSeq sets LastSeq = max(LastSeq, n). Used by the session_resume
// path so we never regress past a seq we've already observed.
func (r *Registry) RestoreLastSeq(sessionID string, n int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if s, ok := r.m[sessionID]; ok {
		if n > s.LastSeq {
			s.LastSeq = n
			s.LastTouched = r.now()
		}
	}
}
