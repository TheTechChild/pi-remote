// SPDX-License-Identifier: MIT
package coordinator

import "errors"

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

// LoadCredentials reads the two service-token files and returns their
// trimmed contents. Returns a wrapped fs.ErrNotExist when either file is
// missing, and ErrEmptyCredential when a file is empty/whitespace.
//
// Loose file modes (anything more permissive than 0600) are loaded
// successfully but recorded in Credentials.Warnings — callers should log
// them at WARN.
func LoadCredentials(idPath, secretPath string) (*Credentials, error) {
	_ = idPath
	_ = secretPath
	// RED-phase stub.
	return nil, errors.New("not implemented")
}
