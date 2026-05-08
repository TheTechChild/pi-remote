// SPDX-License-Identifier: MIT
package http

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHealthReturns200(t *testing.T) {
	srv := httptest.NewServer(NewMux())
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
	if string(body) != `{"status":"ok"}` {
		t.Errorf("body = %q, want JSON ok", string(body))
	}
}
