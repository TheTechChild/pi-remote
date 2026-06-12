// SPDX-License-Identifier: MIT
package push

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"time"
)

// NtfyPoster POSTs sealed push payloads to a client's UnifiedPush
// endpoint (the per-device ntfy topic URL, SPEC.md §§ 19.2-19.3).
type NtfyPoster struct {
	// HTTPClient defaults to a 10s-timeout client when nil.
	HTTPClient *http.Client
	// AuthToken is the optional ntfy bearer token (config [ntfy] auth_token).
	AuthToken string
}

func (p *NtfyPoster) client() *http.Client {
	if p.HTTPClient != nil {
		return p.HTTPClient
	}
	return &http.Client{Timeout: 10 * time.Second}
}

// Post sends the raw sealed bytes (nonce||ct||mac) as
// application/octet-stream per SPEC.md §§ 10.4, 19.3.
func (p *NtfyPoster) Post(ctx context.Context, endpoint string, sealed []byte) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(sealed))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	if p.AuthToken != "" {
		req.Header.Set("Authorization", "Bearer "+p.AuthToken)
	}

	resp, err := p.client().Do(req)
	if err != nil {
		return fmt.Errorf("post %s: %w", endpoint, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("post %s: HTTP %d", endpoint, resp.StatusCode)
	}
	return nil
}
