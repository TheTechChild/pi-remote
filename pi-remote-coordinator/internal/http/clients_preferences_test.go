// SPDX-License-Identifier: MIT
package http

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func postPrefs(t *testing.T, mux *http.ServeMux, jwt, clientID, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/v1/clients/"+clientID+"/preferences", strings.NewReader(body))
	if jwt != "" {
		req.AddCookie(&http.Cookie{Name: "CF_Authorization", Value: jwt})
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func TestClientsPreferences_HappyPath(t *testing.T) {
	mux, creg, _ := registerTestMux(t)
	rec := postRegister(t, mux, "test-jwt-clayton", validBody())
	var reg struct {
		ClientID string `json:"client_id"`
	}
	mustDecode(t, rec.Body.Bytes(), &reg)

	rec = postPrefs(t, mux, "test-jwt-clayton", reg.ClientID,
		`{"queue_update": true, "agent_idle": false}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body.String())
	}

	c, ok := creg.Get(reg.ClientID)
	if !ok {
		t.Fatal("client gone")
	}
	if !c.Preferences["queue_update"] || c.Preferences["agent_idle"] {
		t.Errorf("preferences not persisted: %v", c.Preferences)
	}
}

func TestClientsPreferences_Validation(t *testing.T) {
	mux, _, _ := registerTestMux(t)
	rec := postRegister(t, mux, "test-jwt-clayton", validBody())
	var reg struct {
		ClientID string `json:"client_id"`
	}
	mustDecode(t, rec.Body.Bytes(), &reg)

	// Unknown reason rejected.
	rec = postPrefs(t, mux, "test-jwt-clayton", reg.ClientID, `{"made_up_reason": true}`)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "made_up_reason") {
		t.Errorf("unknown reason: status %d body %s", rec.Code, rec.Body.String())
	}

	// Non-bool value rejected (decode error).
	rec = postPrefs(t, mux, "test-jwt-clayton", reg.ClientID, `{"agent_idle": "yes"}`)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("non-bool value: status %d", rec.Code)
	}

	// Unknown client → 404.
	rec = postPrefs(t, mux, "test-jwt-clayton", "no-such-client", `{"agent_idle": false}`)
	if rec.Code != http.StatusNotFound {
		t.Errorf("unknown client: status %d", rec.Code)
	}

	// No JWT → 403.
	rec = postPrefs(t, mux, "", reg.ClientID, `{"agent_idle": false}`)
	if rec.Code != http.StatusForbidden {
		t.Errorf("unauthenticated: status %d", rec.Code)
	}
}

func mustDecode(t *testing.T, b []byte, v any) {
	t.Helper()
	if err := json.Unmarshal(b, v); err != nil {
		t.Fatalf("decode: %v\n%s", err, b)
	}
}
