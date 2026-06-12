// SPDX-License-Identifier: MIT
package push

import (
	"crypto/rand"
	"fmt"

	"golang.org/x/crypto/nacl/box"
)

// Seal encrypts plaintext to the recipient's X25519 public key using the
// coordinator's secret key (libsodium crypto_box_easy semantics). Wire
// format per SPEC.md § 10.4: nonce (24 bytes) || ciphertext || mac (16
// bytes), with a fresh random nonce per message.
func (k *Keypair) Seal(plaintext []byte, recipientPub *[32]byte) ([]byte, error) {
	var nonce [24]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return nil, fmt.Errorf("nonce: %w", err)
	}
	// box.Seal appends ciphertext||mac to the first arg; prepending the
	// nonce yields the SPEC wire format.
	return box.Seal(nonce[:], plaintext, &nonce, recipientPub, k.Secret), nil
}
