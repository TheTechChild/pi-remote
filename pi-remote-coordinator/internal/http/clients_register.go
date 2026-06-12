// SPDX-License-Identifier: MIT
package http

import (
	"encoding/base64"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/TheTechChild/pi-remote-coordinator/internal/auth"
	"github.com/TheTechChild/pi-remote-coordinator/internal/clients"
	"github.com/TheTechChild/pi-remote-coordinator/internal/push"
)

// clientsRegister is the POST /v1/clients/register handler (SPEC.md
// §§ 11.3, 19.2): CF Access JWT auth, store the device's push endpoint
// and X25519 pubkey, assign a client_id, return the coordinator's
// public key so the phone can decrypt pushes.
type clientsRegister struct {
	auth    auth.Middleware
	clients *clients.Registry
	keypair *push.Keypair
	log     *slog.Logger
}

type clientsRegisterBody struct {
	DeviceDisplayName   string `json:"device_display_name"`
	UnifiedpushEndpoint string `json:"unifiedpush_endpoint"`
	X25519Pubkey        string `json:"x25519_pubkey"`
}

type clientsRegisterResponse struct {
	ClientID                string `json:"client_id"`
	CoordinatorX25519Pubkey string `json:"coordinator_x25519_pubkey"`
}

func (h *clientsRegister) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	identity, err := h.auth.AccessJWT(r)
	if err != nil {
		h.log.Info("clients/register auth rejected", "err", err.Error(), "remote", r.RemoteAddr)
		writeAuthRequired(w)
		return
	}

	var body clientsRegisterBody
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16*1024)).Decode(&body); err != nil {
		writeJSONError(w, http.StatusBadRequest, "ERR_BAD_REQUEST", "malformed JSON body")
		return
	}
	if body.DeviceDisplayName == "" || body.UnifiedpushEndpoint == "" || body.X25519Pubkey == "" {
		writeJSONError(w, http.StatusBadRequest, "ERR_BAD_REQUEST",
			"device_display_name, unifiedpush_endpoint, and x25519_pubkey are required")
		return
	}
	pub, err := base64.StdEncoding.DecodeString(body.X25519Pubkey)
	if err != nil || len(pub) != 32 {
		writeJSONError(w, http.StatusBadRequest, "ERR_BAD_REQUEST",
			"x25519_pubkey must be base64 of exactly 32 bytes")
		return
	}

	id, err := uuid.NewV7() // D17: coordinator-minted IDs are UUIDv7
	if err != nil {
		h.log.Error("clients/register uuid", "err", err)
		writeJSONError(w, http.StatusInternalServerError, "ERR_INTERNAL", "id generation failed")
		return
	}

	c := &clients.Client{
		ID:                  id.String(),
		DeviceDisplayName:   body.DeviceDisplayName,
		UnifiedPushEndpoint: body.UnifiedpushEndpoint,
		RegisteredAt:        time.Now(),
	}
	copy(c.X25519PubKey[:], pub)
	h.clients.Register(c)

	h.log.Info("client registered",
		"client_id", c.ID, "device", c.DeviceDisplayName, "auth_email", identity.Email)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(clientsRegisterResponse{
		ClientID:                c.ID,
		CoordinatorX25519Pubkey: base64.StdEncoding.EncodeToString(h.keypair.Public[:]),
	})
}

func writeJSONError(w http.ResponseWriter, status int, code, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"code": code, "error": msg})
}
