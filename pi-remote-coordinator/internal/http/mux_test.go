// SPDX-License-Identifier: MIT
package http

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/TheTechChild/pi-remote-coordinator/internal/auth"
	"github.com/TheTechChild/pi-remote-coordinator/internal/clients"
	"github.com/TheTechChild/pi-remote-coordinator/internal/machines"
	"github.com/TheTechChild/pi-remote-coordinator/internal/sessions"
)

// C-60: Existing TestHealthReturns200 updated to new NewMux signature.
func TestHealthReturns200(t *testing.T) {
	deps := Deps{
		Auth:     auth.NewStub(),
		Machines: machines.NewRegistry(),
		Sessions: sessions.NewRegistry(),
		Clients:  clients.NewRegistry(clients.WithStubFixture()),
		Logger:   slog.Default(),
	}
	srv := httptest.NewServer(NewMux(deps))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/v1/health")
	if err != nil {
		t.Fatalf("GET /v1/health: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if string(body) != `{"status":"ok","push":"disabled"}` {
		t.Errorf("body = %q, want ok with push disabled (no keypair in deps)", string(body))
	}
}

// With a keypair present, health must advertise push readiness so a
// /data permission regression is observable from the outside.
func TestHealthReportsPushReady(t *testing.T) {
	mux, _, _ := registerTestMux(t) // constructs Deps with a real keypair
	srv := httptest.NewServer(mux)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/v1/health")
	if err != nil {
		t.Fatalf("GET /v1/health: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	if string(body) != `{"status":"ok","push":"ready"}` {
		t.Errorf("body = %q, want push ready", string(body))
	}
}
