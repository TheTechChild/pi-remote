// SPDX-License-Identifier: MIT
package machines

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/coder/websocket"
)

// fakeConn is a Conn implementation used by registry tests. It records
// Close invocations so tests can assert take-over semantics without needing
// a real websocket.
type fakeConn struct {
	id         string
	closedN    atomic.Int32
	lastCode   atomic.Int32
	lastReason atomic.Value // string
	ctx        context.Context
	cancel     context.CancelFunc
}

func newFakeConn(id string) *fakeConn {
	ctx, cancel := context.WithCancel(context.Background())
	return &fakeConn{id: id, ctx: ctx, cancel: cancel}
}

func (f *fakeConn) Close(code websocket.StatusCode, reason string) error {
	f.closedN.Add(1)
	f.lastCode.Store(int32(code))
	f.lastReason.Store(reason)
	f.cancel()
	return nil
}

func (f *fakeConn) Context() context.Context { return f.ctx }

func (f *fakeConn) Write(ctx context.Context, typ websocket.MessageType, b []byte) error {
	return nil
}

func (f *fakeConn) Closed() bool { return f.closedN.Load() > 0 }

// C-23: Register adds entry; Get returns it.
func TestMachines_RegisterAndGet(t *testing.T) {
	r := NewRegistry()
	conn := newFakeConn("a")
	r.Register("m1", "MacBook", "0.0.1", []string{"spawn", "mirror"}, conn)
	m, ok := r.Get("m1")
	if !ok {
		t.Fatal("Get(m1) ok=false")
	}
	if m.ID != "m1" {
		t.Errorf("ID = %q", m.ID)
	}
	if m.DisplayName != "MacBook" {
		t.Errorf("DisplayName = %q", m.DisplayName)
	}
	if m.DaemonVersion != "0.0.1" {
		t.Errorf("DaemonVersion = %q", m.DaemonVersion)
	}
	if len(m.Capabilities) != 2 {
		t.Errorf("Capabilities = %v", m.Capabilities)
	}
	if m.State != "online" {
		t.Errorf("State = %q, want online", m.State)
	}
	if m.Conn != conn {
		t.Errorf("Conn not the one passed in")
	}
}

// C-24: Take-over: second Register with same machine_id closes previous Conn,
// replaces atomically.
func TestMachines_TakeOverClosesPreviousConn(t *testing.T) {
	r := NewRegistry()
	first := newFakeConn("a")
	second := newFakeConn("b")
	r.Register("m1", "MacBook", "0.0.1", nil, first)
	r.Register("m1", "MacBook", "0.0.1", nil, second)

	if !first.Closed() {
		t.Errorf("first conn was not closed by take-over")
	}
	if second.Closed() {
		t.Errorf("second conn was closed; should remain open")
	}
	m, ok := r.Get("m1")
	if !ok {
		t.Fatal("Get(m1) ok=false")
	}
	if m.Conn != second {
		t.Errorf("Conn = %v, want second", m.Conn)
	}
}

// C-25: SetSuspended flips State to "suspended".
func TestMachines_SetSuspended(t *testing.T) {
	r := NewRegistry()
	conn := newFakeConn("a")
	r.Register("m1", "MacBook", "0.0.1", nil, conn)
	r.SetSuspended("m1")
	m, _ := r.Get("m1")
	if m.State != "suspended" {
		t.Errorf("State = %q, want suspended", m.State)
	}
}

// C-26: UnregisterByConn only removes if Conn matches (guards take-over races).
func TestMachines_UnregisterByConn(t *testing.T) {
	r := NewRegistry()
	first := newFakeConn("a")
	second := newFakeConn("b")
	r.Register("m1", "MacBook", "0.0.1", nil, first)
	r.Register("m1", "MacBook", "0.0.1", nil, second)
	// At this point Conn == second. Unregister by `first` must NOT remove.
	r.UnregisterByConn("m1", first)
	if _, ok := r.Get("m1"); !ok {
		t.Errorf("Unregister with stale conn removed the entry; should be a no-op")
	}
	// Unregister by the live conn does remove.
	r.UnregisterByConn("m1", second)
	if _, ok := r.Get("m1"); ok {
		t.Errorf("Unregister with live conn did not remove the entry")
	}
}

// C-27: Concurrent registers under -race; exactly one current, all others closed.
func TestMachines_ConcurrentRegistersAllButOneClosed(t *testing.T) {
	r := NewRegistry()
	const N = 50
	conns := make([]*fakeConn, N)
	for i := range conns {
		conns[i] = newFakeConn("c")
	}
	var wg sync.WaitGroup
	for i := 0; i < N; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			r.Register("m1", "MacBook", "0.0.1", nil, conns[i])
		}()
	}
	wg.Wait()
	m, ok := r.Get("m1")
	if !ok {
		t.Fatal("no entry after concurrent registers")
	}
	closedCount := 0
	for _, c := range conns {
		if c.Closed() {
			closedCount++
		}
	}
	// One conn remains live; the rest must have been closed by take-over.
	if closedCount != N-1 {
		t.Errorf("closedCount = %d, want %d (one live, rest closed)", closedCount, N-1)
	}
	// And the live entry must be a fake we created.
	found := false
	for _, c := range conns {
		if m.Conn == c {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("registry holds a Conn we did not register")
	}
}
