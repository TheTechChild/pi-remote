// SPDX-License-Identifier: MIT
package push

import "sync"

// FocusTracker records per-(client,session) foreground state from
// client_focus frames so the dispatcher can suppress pushes for sessions
// the user is actively looking at (SPEC.md §§ 8.7, 9.7). State is scoped
// to the WebSocket connection that reported it: a connection close drops
// all of its focus claims, so a killed app can never leave a session
// permanently muted (issue #25 "stale focus" acceptance).
type FocusTracker struct {
	mu    sync.RWMutex
	conns map[string]*connFocus // connID → focus claims
}

type connFocus struct {
	clientID string
	sessions map[string]bool // sessionID → focused
}

// NewFocusTracker constructs an empty tracker.
func NewFocusTracker() *FocusTracker {
	return &FocusTracker{conns: make(map[string]*connFocus)}
}

// SetFocus records a client_focus frame from connID.
func (t *FocusTracker) SetFocus(connID, clientID, sessionID string, focused bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	cf, ok := t.conns[connID]
	if !ok {
		cf = &connFocus{clientID: clientID, sessions: make(map[string]bool)}
		t.conns[connID] = cf
	}
	if focused {
		cf.sessions[sessionID] = true
	} else {
		delete(cf.sessions, sessionID)
	}
}

// DropConn removes every focus claim held by connID. Called from the
// client WS handler's close path: a closed connection implicitly flips
// all of that client's sessions to unfocused.
func (t *FocusTracker) DropConn(connID string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.conns, connID)
}

// IsFocused reports whether any live connection of clientID currently
// claims foreground focus on sessionID.
func (t *FocusTracker) IsFocused(clientID, sessionID string) bool {
	t.mu.RLock()
	defer t.mu.RUnlock()
	for _, cf := range t.conns {
		if cf.clientID == clientID && cf.sessions[sessionID] {
			return true
		}
	}
	return false
}
