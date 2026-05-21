// SPDX-License-Identifier: MIT
package tmux_test

import (
	"bytes"
	"testing"

	"github.com/TheTechChild/pi-remote-daemon/internal/tmux"
)

func TestDeescapeTmux(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    []byte
		wantErr bool
	}{
		{
			name:  "plain text",
			input: "hello",
			want:  []byte("hello"),
		},
		{
			name:  "escaped backslash",
			input: "hello\\\\world",
			want:  []byte("hello\\world"),
		},
		{
			name:  "octal escapes",
			input: "\\141\\142\\143",
			want:  []byte("abc"),
		},
		{
			name:  "mixed plain and octal",
			input: "A\\112B\\113C",
			want:  []byte("AJBKC"),
		},
		{
			name:    "trailing backslash",
			input:   "hello\\",
			wantErr: true,
		},
		{
			name:    "invalid octal sequence",
			input:   "hello\\199",
			wantErr: true,
		},
		{
			name:    "too short octal",
			input:   "hello\\12",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tmux.DeescapeTmux(tt.input)
			if (err != nil) != tt.wantErr {
				t.Fatalf("DeescapeTmux() error = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr && !bytes.Equal(got, tt.want) {
				t.Errorf("DeescapeTmux() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestClient_WritePty_NotStarted(t *testing.T) {
	c := tmux.NewClient("tmux", "pi-test-", nil, nil, nil)
	err := c.WritePty("session-1", []byte("hello"))
	if err == nil {
		t.Fatal("expected error since client is not started")
	}
}

func TestClient_ResizePty_NotStarted(t *testing.T) {
	c := tmux.NewClient("tmux", "pi-test-", nil, nil, nil)
	err := c.ResizePty("session-1", 80, 24)
	if err == nil {
		t.Fatal("expected error since client is not started")
	}
}
