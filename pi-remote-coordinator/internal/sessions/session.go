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
	mu          sync.RWMutex
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
