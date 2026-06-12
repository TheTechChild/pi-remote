// SPDX-License-Identifier: MIT

// Package sessions holds the coordinator-side registry of live Pi sessions.
package sessions

import (
	"sync"
	"time"

	"github.com/TheTechChild/pi-remote-coordinator/internal/broker"
	daemon_coordinator "github.com/TheTechChild/pi-remote-coordinator/internal/proto/daemon-coordinator"
)

// ClientConn is an interface for fanning out session frames to attached clients.
type ClientConn interface {
	ID() string
	Send(msg []byte)
}

// Session is the coordinator's view of one Pi process running on a daemon.
type Session struct {
	mu sync.RWMutex

	// publishMu serializes Publish/Broadcast (append + live fan-out)
	// against AttachWithReplay (replay + attach). Without it a frame
	// appended between a client's replay and its attach would be neither
	// replayed nor delivered live — a silent, permanent gap.
	publishMu sync.Mutex

	SessionID   string
	MachineID   string
	Metadata    daemon_coordinator.SessionStartedJsonMetadata
	State       string // one of: "running", "idle", "paused", "unresponsive", "ended"
	LastSeq     int
	Ended       bool
	EndedAt     time.Time
	LastTouched time.Time

	Ring            *broker.RingBuffer
	AttachedClients map[string]ClientConn
}

// Attach registers a client connection to receive live frames.
func (s *Session) Attach(conn ClientConn) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.AttachedClients == nil {
		s.AttachedClients = make(map[string]ClientConn)
	}
	s.AttachedClients[conn.ID()] = conn
	s.LastTouched = time.Now()
}

// Detach unregisters a client connection from receiving live frames.
func (s *Session) Detach(connID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.AttachedClients != nil {
		delete(s.AttachedClients, connID)
	}
	s.LastTouched = time.Now()
}

// GetAttachedClients returns a slice of all currently fanned-out clients.
func (s *Session) GetAttachedClients() []ClientConn {
	s.mu.RLock()
	defer s.mu.RUnlock()
	res := make([]ClientConn, 0, len(s.AttachedClients))
	for _, client := range s.AttachedClients {
		res = append(res, client)
	}
	return res
}

// GetState returns the session state in a thread-safe manner.
func (s *Session) GetState() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.State
}

// GetMetadata returns the session metadata in a thread-safe manner.
func (s *Session) GetMetadata() daemon_coordinator.SessionStartedJsonMetadata {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.Metadata
}

// Publish appends entry to the ring and fans its payload out to every
// attached client. Publish and AttachWithReplay serialize on the same
// per-session lock, so a frame can never fall between a client's replay
// and its first live frame, and is never delivered twice.
func (s *Session) Publish(entry broker.Entry) {
	s.publishMu.Lock()
	defer s.publishMu.Unlock()
	s.Ring.Append(entry)
	for _, c := range s.GetAttachedClients() {
		c.Send(entry.Payload)
	}
}

// Broadcast sends a raw frame to every attached client without touching
// the ring. Used for coordinator→app control frames (session_state_change,
// session_ended) that carry no seq and are not replayable. See SPEC.md
// §§ 8.7, 10.3.
func (s *Session) Broadcast(raw []byte) {
	s.publishMu.Lock()
	defer s.publishMu.Unlock()
	for _, c := range s.GetAttachedClients() {
		c.Send(raw)
	}
}

// AttachWithReplay atomically replays ring history into conn and attaches
// it for live frames. If lastSeq is no longer available in the ring, the
// frame built by unavailableFrame is sent instead of a backfill and the
// client transitions straight to live (SPEC.md § 18.4).
func (s *Session) AttachWithReplay(
	conn ClientConn,
	lastSeq uint64,
	unavailableFrame func(earliestSeq, latestSeq uint64) []byte,
) (replayed int, ok bool) {
	s.publishMu.Lock()
	defer s.publishMu.Unlock()
	entries, ok, earliest, latest := s.Ring.Replay(lastSeq)
	if !ok {
		conn.Send(unavailableFrame(earliest, latest))
	} else {
		for _, e := range entries {
			conn.Send(e.Payload)
		}
	}
	s.Attach(conn)
	return len(entries), ok
}
