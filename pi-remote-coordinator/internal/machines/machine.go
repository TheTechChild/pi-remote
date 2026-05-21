// SPDX-License-Identifier: MIT

// Package machines holds the coordinator-side registry of connected
// daemons and the ingestor that dispatches daemon-coordinator frames into
// the sessions/machines registries (SPEC.md §§ 8.4, 10.2).
package machines

import (
	"context"
	"time"

	"github.com/coder/websocket"
)

// Conn is the tiny surface the registry needs from a daemon WebSocket
// connection. *websocket.Conn satisfies this in production; tests use a
// fake (see registry_test.go) so they can run without a real socket.
type Conn interface {
	Close(code websocket.StatusCode, reason string) error
	Context() context.Context
	Write(ctx context.Context, typ websocket.MessageType, b []byte) error
}

// Machine is the coordinator's view of one connected daemon. See
// SPEC.md § 8.4.
type Machine struct {
	ID            string
	DisplayName   string
	DaemonVersion string
	Capabilities  []string
	State         string // "online", "suspended"
	Conn          Conn
	LastSeen      time.Time
}
