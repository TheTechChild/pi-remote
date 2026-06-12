// SPDX-License-Identifier: MIT
package push

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"

	"golang.org/x/crypto/nacl/box"

	"github.com/TheTechChild/pi-remote-coordinator/internal/clients"
)

func testKeypair(t *testing.T) *Keypair {
	t.Helper()
	kp, err := LoadOrGenerateKeypair(filepath.Join(t.TempDir(), "kp.box"))
	if err != nil {
		t.Fatal(err)
	}
	return kp
}

// M5 acceptance: output is decryptable with crypto_box_open_easy
// (nonce || ct || mac wire format, 24-byte random nonce).
func TestSealRoundTrip(t *testing.T) {
	kp := testKeypair(t)
	clientPub, clientSec, err := box.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	plaintext := []byte(`{"v":1,"kind":"needs_attention"}`)
	sealed, err := kp.Seal(plaintext, clientPub)
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	if len(sealed) != 24+len(plaintext)+box.Overhead {
		t.Fatalf("sealed len = %d, want nonce(24)+pt(%d)+mac(%d)", len(sealed), len(plaintext), box.Overhead)
	}

	var nonce [24]byte
	copy(nonce[:], sealed[:24])
	opened, ok := box.Open(nil, sealed[24:], &nonce, kp.Public, clientSec)
	if !ok {
		t.Fatal("box.Open failed: wire format not crypto_box_open_easy compatible")
	}
	if string(opened) != string(plaintext) {
		t.Errorf("round trip = %q", opened)
	}

	// Fresh nonce per message: two seals of the same plaintext differ.
	sealed2, _ := kp.Seal(plaintext, clientPub)
	if string(sealed[:24]) == string(sealed2[:24]) {
		t.Error("nonce reused across messages")
	}
}

func TestNtfyPoster(t *testing.T) {
	var gotCT, gotAuth string
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotCT = r.Header.Get("Content-Type")
		gotAuth = r.Header.Get("Authorization")
		gotBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	p := &NtfyPoster{AuthToken: "tok123"}
	if err := p.Post(context.Background(), srv.URL+"/up/topic", []byte{1, 2, 3}); err != nil {
		t.Fatalf("Post: %v", err)
	}
	if gotCT != "application/octet-stream" {
		t.Errorf("Content-Type = %q", gotCT)
	}
	if gotAuth != "Bearer tok123" {
		t.Errorf("Authorization = %q", gotAuth)
	}
	if len(gotBody) != 3 {
		t.Errorf("body = %v", gotBody)
	}

	// Non-2xx is an error.
	srv500 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer srv500.Close()
	if err := p.Post(context.Background(), srv500.URL, nil); err == nil {
		t.Error("expected error for HTTP 502")
	}
}

func TestReasonEnabledDefaults(t *testing.T) {
	for reason, want := range map[string]bool{
		"agent_idle": true, "extension_dialog": true, "tool_failure": true,
		"unresponsive": true, "queue_update": false, "compaction_complete": false,
		"extension_error": false, "session_ended": false,
	} {
		if got := ReasonEnabled(nil, reason); got != want {
			t.Errorf("default %s = %v, want %v", reason, got, want)
		}
	}
	// Overrides win in both directions.
	if ReasonEnabled(map[string]bool{"agent_idle": false}, "agent_idle") {
		t.Error("override to false ignored")
	}
	if !ReasonEnabled(map[string]bool{"queue_update": true}, "queue_update") {
		t.Error("override to true ignored")
	}
	if ValidReason("nope") {
		t.Error("ValidReason accepted unknown reason")
	}
}

func TestFocusTracker(t *testing.T) {
	ft := NewFocusTracker()
	ft.SetFocus("conn-1", "client-a", "sess-1", true)

	if !ft.IsFocused("client-a", "sess-1") {
		t.Error("focus not recorded")
	}
	if ft.IsFocused("client-a", "sess-2") || ft.IsFocused("client-b", "sess-1") {
		t.Error("focus leaked to wrong session/client")
	}

	ft.SetFocus("conn-1", "client-a", "sess-1", false)
	if ft.IsFocused("client-a", "sess-1") {
		t.Error("unfocus ignored")
	}

	// Close clears everything the conn claimed (issue #25).
	ft.SetFocus("conn-1", "client-a", "sess-1", true)
	ft.SetFocus("conn-1", "client-a", "sess-2", true)
	ft.DropConn("conn-1")
	if ft.IsFocused("client-a", "sess-1") || ft.IsFocused("client-a", "sess-2") {
		t.Error("DropConn left stale focus")
	}
}

func TestFocusTrackerConcurrent(t *testing.T) {
	ft := NewFocusTracker()
	var wg sync.WaitGroup
	for w := 0; w < 8; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			conn := fmt.Sprintf("conn-%d", w)
			for i := 0; i < 500; i++ {
				ft.SetFocus(conn, "client-a", fmt.Sprintf("sess-%d", i%4), i%2 == 0)
				ft.IsFocused("client-a", "sess-1")
				if i%100 == 99 {
					ft.DropConn(conn)
				}
			}
			ft.DropConn(conn)
		}(w)
	}
	wg.Wait()
	if ft.IsFocused("client-a", "sess-0") {
		t.Error("stale focus after all conns dropped")
	}
}

// capturePoster records posts; Seal output is decrypted to verify payload.
type capturePoster struct {
	mu    sync.Mutex
	posts map[string][][]byte // endpoint → sealed bodies
}

func (p *capturePoster) Post(_ context.Context, endpoint string, sealed []byte) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.posts == nil {
		p.posts = make(map[string][][]byte)
	}
	cp := make([]byte, len(sealed))
	copy(cp, sealed)
	p.posts[endpoint] = append(p.posts[endpoint], cp)
	return nil
}

func TestDispatcherFanOutAndSkips(t *testing.T) {
	kp := testKeypair(t)
	ft := NewFocusTracker()
	creg := clients.NewRegistry()
	poster := &capturePoster{}

	newClient := func(id, endpoint string) *[32]byte {
		pub, _, err := box.GenerateKey(rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		c := &clients.Client{ID: id, DeviceDisplayName: id, UnifiedPushEndpoint: endpoint}
		copy(c.X25519PubKey[:], pub[:])
		creg.Register(c)
		return pub
	}

	_ = newClient("c-eligible", "https://ntfy/up/eligible")
	_ = newClient("c-focused", "https://ntfy/up/focused")
	_ = newClient("c-filtered", "https://ntfy/up/filtered")
	creg.Register(&clients.Client{ID: "c-nopush", DeviceDisplayName: "no push"}) // no endpoint/key

	ft.SetFocus("conn-9", "c-focused", "sess-1", true)
	creg.SetPreferences("c-filtered", map[string]bool{"extension_dialog": false})

	d := &Dispatcher{Clients: creg, Focus: ft, Keypair: kp, Poster: poster,
		Log: slog.New(slog.NewTextHandler(io.Discard, nil))}

	d.Dispatch(Notification{
		Reason:             "extension_dialog",
		SessionID:          "sess-1",
		MachineID:          "mach-1",
		MachineDisplayName: "MacBook Pro",
		ProjectName:        "pi-remote",
		Summary:            "Permission required: rm -rf",
	})

	poster.mu.Lock()
	defer poster.mu.Unlock()
	if len(poster.posts["https://ntfy/up/eligible"]) != 1 {
		t.Errorf("eligible client: %d posts, want 1", len(poster.posts["https://ntfy/up/eligible"]))
	}
	for _, skipped := range []string{"https://ntfy/up/focused", "https://ntfy/up/filtered"} {
		if len(poster.posts[skipped]) != 0 {
			t.Errorf("%s: pushed despite skip condition", skipped)
		}
	}
	if len(poster.posts) != 1 {
		t.Errorf("posts to %d endpoints, want 1", len(poster.posts))
	}
}

func TestDispatcherPayloadDecryptsAndValidates(t *testing.T) {
	kp := testKeypair(t)
	creg := clients.NewRegistry()
	poster := &capturePoster{}

	clientPub, clientSec, err := box.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	c := &clients.Client{ID: "c1", DeviceDisplayName: "Pixel", UnifiedPushEndpoint: "https://ntfy/up/t"}
	copy(c.X25519PubKey[:], clientPub[:])
	creg.Register(c)

	d := &Dispatcher{Clients: creg, Focus: NewFocusTracker(), Keypair: kp, Poster: poster,
		Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	d.Dispatch(Notification{
		Reason:             "tool_failure",
		SessionID:          "sess-42",
		MachineID:          "mach-1",
		MachineDisplayName: "MacBook Pro",
		ProjectName:        "pi-remote",
		Summary:            "Tool failed: bash",
		Ts:                 1730000000123,
	})

	poster.mu.Lock()
	sealed := poster.posts["https://ntfy/up/t"][0]
	poster.mu.Unlock()

	var nonce [24]byte
	copy(nonce[:], sealed[:24])
	plaintext, ok := box.Open(nil, sealed[24:], &nonce, kp.Public, clientSec)
	if !ok {
		t.Fatal("client could not decrypt the dispatched payload")
	}

	var payload map[string]any
	if err := json.Unmarshal(plaintext, &payload); err != nil {
		t.Fatalf("payload not JSON: %v", err)
	}
	for k, want := range map[string]any{
		"v": float64(1), "kind": "needs_attention", "reason": "tool_failure",
		"session_id": "sess-42", "machine_id": "mach-1",
		"machine_display_name": "MacBook Pro", "project_name": "pi-remote",
		"summary": "Tool failed: bash", "ts": float64(1730000000123),
		"deep_link": "pi-remote://session/sess-42",
	} {
		if payload[k] != want {
			t.Errorf("payload[%s] = %v, want %v", k, payload[k], want)
		}
	}
}
