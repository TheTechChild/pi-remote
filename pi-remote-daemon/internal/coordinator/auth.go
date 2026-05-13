// SPDX-License-Identifier: MIT
package coordinator

import (
	"errors"
	"fmt"
	"os"
	"strings"
)

// ErrEmptyCredential is returned by LoadCredentials when a credential file
// exists but contains only whitespace (after trimming). The reconnect loop
// uses this typed error to log a clear "credential file present but empty"
// message before backing off.
var ErrEmptyCredential = errors.New("coordinator: credential file is empty")

// Credentials carries the values read from the D13/D14 service-token
// files plus any non-fatal warnings observed during the read (e.g., a
// file with permissions looser than 0600). The dial loop reads
// Credentials once per connect attempt; if the files change on disk the
// next reconnect picks up the new values.
//
// Per SPEC §§ D13, D14 and the Batch 2 plan: file mode != 0600 is a
// warning, not a refusal. The strict-mode hardening is aspirational and
// lands when the deployment work picks it up.
type Credentials struct {
	ID       string
	Secret   string
	Warnings []string
}

// strictPerm is the spec-mandated file mode for service-token credential
// files (D13/D14). Modes strictly more permissive than this produce a
// warning in the returned Credentials, but the load itself succeeds.
const strictPerm os.FileMode = 0o600

// LoadCredentials reads the two service-token files and returns their
// trimmed contents. Returns a wrapped fs.ErrNotExist when either file is
// missing, and ErrEmptyCredential when a file is empty/whitespace.
//
// Loose file modes (anything more permissive than 0600) are loaded
// successfully but recorded in Credentials.Warnings — callers should log
// them at WARN.
func LoadCredentials(idPath, secretPath string) (*Credentials, error) {
	id, idWarn, err := readCredFile(idPath, "id")
	if err != nil {
		return nil, err
	}
	secret, secWarn, err := readCredFile(secretPath, "secret")
	if err != nil {
		return nil, err
	}

	var warnings []string
	if idWarn != "" {
		warnings = append(warnings, idWarn)
	}
	if secWarn != "" {
		warnings = append(warnings, secWarn)
	}

	return &Credentials{
		ID:       id,
		Secret:   secret,
		Warnings: warnings,
	}, nil
}

// readCredFile reads one credential file, trims whitespace, validates
// non-emptiness, and returns a permission warning if the file's mode is
// more permissive than strictPerm. The label is used to make the
// warning message identify the file.
func readCredFile(path, label string) (value string, warning string, err error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", "", err
	}

	b, err := os.ReadFile(path)
	if err != nil {
		return "", "", err
	}
	trimmed := strings.TrimSpace(string(b))
	if trimmed == "" {
		return "", "", fmt.Errorf("%w: %s (%s)", ErrEmptyCredential, path, label)
	}

	mode := info.Mode().Perm()
	if mode&^strictPerm != 0 {
		warning = fmt.Sprintf("coordinator: %s credential file %s has permissive mode %o (want %o)", label, path, mode, strictPerm)
	}
	return trimmed, warning, nil
}
