// SPDX-License-Identifier: MIT
package push

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadOrGenerateKeypair_FirstRunPersists(t *testing.T) {
	p := filepath.Join(t.TempDir(), "sub", "coordinator-keypair.box")
	kp, err := LoadOrGenerateKeypair(p)
	if err != nil {
		t.Fatalf("first run: %v", err)
	}
	if kp.Public == nil || kp.Secret == nil {
		t.Fatal("nil key halves")
	}
	if bytes.Equal(kp.Public[:], make([]byte, 32)) || bytes.Equal(kp.Secret[:], make([]byte, 32)) {
		t.Fatal("zero key material")
	}

	info, err := os.Stat(p)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("file mode = %o, want 600", info.Mode().Perm())
	}
	if info.Size() != keypairFileSize {
		t.Errorf("file size = %d, want %d", info.Size(), keypairFileSize)
	}
}

func TestLoadOrGenerateKeypair_SubsequentRunLoadsSame(t *testing.T) {
	p := filepath.Join(t.TempDir(), "kp.box")
	first, err := LoadOrGenerateKeypair(p)
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	second, err := LoadOrGenerateKeypair(p)
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	if !bytes.Equal(first.Public[:], second.Public[:]) || !bytes.Equal(first.Secret[:], second.Secret[:]) {
		t.Error("keypair not stable across runs")
	}
}

func TestLoadOrGenerateKeypair_CorruptSizeErrors(t *testing.T) {
	p := filepath.Join(t.TempDir(), "kp.box")
	if err := os.WriteFile(p, []byte("too short"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadOrGenerateKeypair(p); err == nil {
		t.Fatal("expected error for corrupt keypair file")
	}
}
