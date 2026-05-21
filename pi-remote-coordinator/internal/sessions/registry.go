// SPDX-License-Identifier: MIT
package sessions

import (
	"sync"
	"time"

	"github.com/TheTechChild/pi-remote-coordinator/internal/broker"
	daemon_coordinator "github.com/TheTechChild/pi-remote-coordinator/internal/proto/daemon-coordinator"
)

// Registry is an in-memory map of sessions, safe for concurrent use.
type Registry struct {
	mu  sync.RWMutex
	m   map[string]*Session
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
// LastSeq to 0, Ended to false.
func (r *Registry) Register(sessionID, machineID string, metadata daemon_coordinator.SessionStartedJsonMetadata) *Session {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := r.now()
	s := &Session{
		SessionID:       sessionID,
		MachineID:       machineID,
		Metadata:        metadata,
		State:           "running",
		LastSeq:         0,
		Ended:           false,
		LastTouched:     now,
		Ring:            broker.NewRingBuffer(5 * 1024 * 1024), // 5MB starting MaxBytes
		AttachedClients: make(map[string]ClientConn),
	}
	r.m[sessionID] = s
	return s
}

// Get returns the actual session pointer and ok=true, or (nil, false) if not present.
// The session pointer is safe for concurrent access due to its internal RWMutex.
func (r *Registry) Get(sessionID string) (*Session, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	s, ok := r.m[sessionID]
	if !ok {
		return nil, false
	}
	return s, true
}

// AdvanceSeq sets LastSeq = max(LastSeq, seq). Returns true if LastSeq
// advanced (i.e. seq > previous LastSeq); false if seq was stale or equal.
func (r *Registry) AdvanceSeq(sessionID string, seq int) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.m[sessionID]
	if !ok {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
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
		s.mu.Lock()
		s.State = state
		s.LastTouched = r.now()
		s.mu.Unlock()
	}
}

// MarkEnded sets Ended=true and stamps EndedAt.
func (r *Registry) MarkEnded(sessionID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if s, ok := r.m[sessionID]; ok {
		s.mu.Lock()
		s.Ended = true
		s.EndedAt = r.now()
		s.LastTouched = s.EndedAt
		s.State = "ended"
		s.mu.Unlock()
	}
}

// PauseAllForMachine flips every non-ended session belonging to machineID
// to State="paused". Ended sessions are skipped.
func (r *Registry) PauseAllForMachine(machineID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := r.now()
	for _, s := range r.m {
		s.mu.Lock()
		if s.MachineID != machineID {
			s.mu.Unlock()
			continue
		}
		if s.Ended {
			s.mu.Unlock()
			continue
		}
		s.State = "paused"
		s.LastTouched = now
		s.mu.Unlock()
	}
}

// RestoreLastSeq sets LastSeq = max(LastSeq, n).
func (r *Registry) RestoreLastSeq(sessionID string, n int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if s, ok := r.m[sessionID]; ok {
		s.mu.Lock()
		if n > s.LastSeq {
			s.LastSeq = n
			s.LastTouched = r.now()
		}
		s.mu.Unlock()
	}
}

// LRUItems returns a list of LRU items for all registered sessions.
func (r *Registry) LRUItems() []broker.LRUItem {
	r.mu.RLock()
	defer r.mu.RUnlock()
	items := make([]broker.LRUItem, 0, len(r.m))
	for _, s := range r.m {
		s.mu.RLock()
		items = append(items, broker.LRUItem{
			SessionID:   s.SessionID,
			Ended:       s.Ended,
			LastTouched: s.LastTouched,
			Ring:        s.Ring,
		})
		s.mu.RUnlock()
	}
	return items
}

// AppendToRing appends an entry to a session's RingBuffer and triggers Global LRU balancing.
func (r *Registry) AppendToRing(sessionID string, entry broker.Entry) {
	r.mu.RLock()
	s, ok := r.m[sessionID]
	r.mu.RUnlock()
	if !ok {
		return
	}

	// 1. Append entry to session's ring buffer
	s.Ring.Append(entry)
	s.mu.Lock()
	s.LastTouched = time.Now()
	s.mu.Unlock()

	// 2. Perform Global LRU balancing
	items := r.LRUItems()
	broker.BalanceGlobalLRU(items, sessionID, 50*1024*1024, 1*1024*1024)
}

// List returns a snapshot of all registered sessions.
func (r *Registry) List() []*Session {
	r.mu.RLock()
	defer r.mu.RUnlock()
	res := make([]*Session, 0, len(r.m))
	for _, s := range r.m {
		res = append(res, s)
	}
	return res
}
