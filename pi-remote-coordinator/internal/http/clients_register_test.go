// SPDX-License-Identifier: MIT
package http

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/TheTechChild/pi-remote-coordinator/internal/auth"
	"github.com/TheTechChild/pi-remote-coordinator/internal/clients"
	"github.com/TheTechChild/pi-remote-coordinator/internal/machines"
	"github.com/TheTechChild/pi-remote-coordinator/internal/push"
	"github.com/TheTechChild/pi-remote-coordinator/internal/sessions"
)

func registerTestMux(t *testing.T) (*http.ServeMux, *clients.Registry, *push.Keypair) {
	t.Helper()
	kp, err := push.LoadOrGenerateKeypair(filepath.Join(t.TempDir(), "kp.box"))
	if err != nil {
		t.Fatalf("keypair: %v", err)
	}
	creg := clients.NewRegistry()
	mux := NewMux(Deps{
		Auth:     auth.NewStub(),
		Machines: machines.NewRegistry(),
		Sessions: sessions.NewRegistry(),
		Clients:  creg,
		Logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
		Keypair:  kp,
	})
	return mux, creg, kp
}

func postRegister(t *testing.T, mux *http.ServeMux, jwt string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	switch b := body.(type) {
	case string:
		buf.WriteString(b)
	default:
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatal(err)
		}
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/clients/register", &buf)
	if jwt != "" {
		req.AddCookie(&http.Cookie{Name: "CF_Authorization", Value: jwt})
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func validBody() map[string]string {
	pub := make([]byte, 32)
	pub[0] = 0x42
	return map[string]string{
		"device_display_name":  "Pixel 8",
		"unifiedpush_endpoint": "https://ntfy.example.com/up/abc123",
		"x25519_pubkey":        base64.StdEncoding.EncodeToString(pub),
	}
}

func TestClientsRegister_HappyPath(t *testing.T) {
	mux, creg, kp := registerTestMux(t)

	rec := postRegister(t, mux, "test-jwt-clayton", validBody())
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body.String())
	}

	var resp struct {
		ClientID                string `json:"client_id"`
		CoordinatorX25519Pubkey string `json:"coordinator_x25519_pubkey"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.ClientID == "" || len(resp.ClientID) != 36 || resp.ClientID[14] != '7' {
		t.Errorf("client_id %q is not a UUIDv7", resp.ClientID)
	}
	gotPub, err := base64.StdEncoding.DecodeString(resp.CoordinatorX25519Pubkey)
	if err != nil || !bytes.Equal(gotPub, kp.Public[:]) {
		t.Errorf("coordinator pubkey mismatch")
	}

	c, ok := creg.Get(resp.ClientID)
	if !ok {
		t.Fatal("client not stored in registry")
	}
	if c.DeviceDisplayName != "Pixel 8" ||
		c.UnifiedPushEndpoint != "https://ntfy.example.com/up/abc123" ||
		c.X25519PubKey[0] != 0x42 {
		t.Errorf("stored client wrong: %+v", c)
	}
	if c.RegisteredAt.IsZero() {
		t.Error("RegisteredAt not stamped")
	}
}

func TestClientsRegister_AuthRequired(t *testing.T) {
	mux, _, _ := registerTestMux(t)
	rec := postRegister(t, mux, "", validBody())
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "ERR_COORD_AUTH_REQUIRED") {
		t.Errorf("body = %s", rec.Body.String())
	}
}

func TestClientsRegister_Validation(t *testing.T) {
	mux, _, _ := registerTestMux(t)

	for name, body := range map[string]any{
		"malformed json": "{not json",
		"missing fields": map[string]string{"device_display_name": "Pixel 8"},
		"bad pubkey b64": map[string]string{"device_display_name": "P", "unifiedpush_endpoint": "u", "x25519_pubkey": "!!!"},
		"short pubkey":   map[string]string{"device_display_name": "P", "unifiedpush_endpoint": "u", "x25519_pubkey": base64.StdEncoding.EncodeToString([]byte("short"))},
	} {
		rec := postRegister(t, mux, "test-jwt-clayton", body)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400", name, rec.Code)
		}
	}
}

func TestClientsRegister_NotRoutedWithoutKeypair(t *testing.T) {
	mux := NewMux(Deps{
		Auth:     auth.NewStub(),
		Machines: machines.NewRegistry(),
		Sessions: sessions.NewRegistry(),
		Clients:  clients.NewRegistry(),
		Logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
		Keypair:  nil,
	})
	rec := postRegister(t, mux, "test-jwt-clayton", validBody())
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 when keypair unavailable", rec.Code)
	}
}
