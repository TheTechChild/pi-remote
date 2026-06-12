// SPDX-License-Identifier: MIT

// Package push implements the coordinator's push-notification pipeline:
// the X25519 identity keypair (SPEC.md §§ 8.3, 19.2), NaCl crypto_box
// encryption, and ntfy dispatch (SPEC.md §§ 8.8, 10.4, 19.3).
package push

import (
	"crypto/rand"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/crypto/nacl/box"
)

// keypairFileSize is the on-disk format: public key (32 bytes) followed
// by secret key (32 bytes) — the two halves of a libsodium
// crypto_box_keypair, concatenated.
const keypairFileSize = 64

// Keypair is the coordinator's X25519 crypto_box identity. The public
// half is shared with phones at registration (SPEC.md § 11.3); the
// secret half encrypts push payloads (SPEC.md § 10.4).
type Keypair struct {
	Public *[32]byte
	Secret *[32]byte
}

// LoadOrGenerateKeypair returns the keypair persisted at path, generating
// and persisting a fresh one (file mode 0600) on first run. SPEC.md § 19.2:
// "generated on first run", loaded on every subsequent start.
func LoadOrGenerateKeypair(path string) (*Keypair, error) {
	b, err := os.ReadFile(path)
	switch {
	case err == nil:
		if len(b) != keypairFileSize {
			return nil, fmt.Errorf("keypair %s: %d bytes, want %d (corrupt?)", path, len(b), keypairFileSize)
		}
		kp := &Keypair{Public: new([32]byte), Secret: new([32]byte)}
		copy(kp.Public[:], b[:32])
		copy(kp.Secret[:], b[32:])
		return kp, nil
	case !os.IsNotExist(err):
		return nil, fmt.Errorf("read keypair %s: %w", path, err)
	}

	pub, sec, err := box.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate keypair: %w", err)
	}
	kp := &Keypair{Public: pub, Secret: sec}

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create keypair dir: %w", err)
	}
	buf := make([]byte, 0, keypairFileSize)
	buf = append(buf, pub[:]...)
	buf = append(buf, sec[:]...)
	// 0600: the secret half is a credential (SPEC.md § 8.3 keeps it on
	// the /data volume, never in config).
	if err := os.WriteFile(path, buf, 0o600); err != nil {
		return nil, fmt.Errorf("persist keypair %s: %w", path, err)
	}
	return kp, nil
}
