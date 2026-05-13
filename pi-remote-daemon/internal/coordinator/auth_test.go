// SPDX-License-Identifier: MIT
package coordinator_test

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/TheTechChild/pi-remote-daemon/internal/coordinator"
)

// writeFile writes content to a file inside the test's temp dir with the
// requested mode. Returns the full path. Test failure on any error.
func writeFile(t *testing.T, dir, name, content string, mode os.FileMode) string {
	t.Helper()
	path := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(path, []byte(content), mode))
	// WriteFile honors the umask; force the mode explicitly so file-mode
	// tests behave the same across developer machines.
	require.NoError(t, os.Chmod(path, mode))
	return path
}

// A1: TestLoadCredentials_HappyPath — two readable 0600 files with the
// expected contents load cleanly with no warnings.
func TestLoadCredentials_HappyPath(t *testing.T) {
	dir := t.TempDir()
	idPath := writeFile(t, dir, "id", "test-machine", 0o600)
	secretPath := writeFile(t, dir, "secret", "test-secret", 0o600)

	creds, err := coordinator.LoadCredentials(idPath, secretPath)
	require.NoError(t, err)
	require.NotNil(t, creds)
	require.Equal(t, "test-machine", creds.ID)
	require.Equal(t, "test-secret", creds.Secret)
	require.Empty(t, creds.Warnings)
}

// A2: TestLoadCredentials_TrimsWhitespace — defensive trimming against
// 'echo > file' footgun.
func TestLoadCredentials_TrimsWhitespace(t *testing.T) {
	dir := t.TempDir()
	idPath := writeFile(t, dir, "id", "test-machine\n", 0o600)
	secretPath := writeFile(t, dir, "secret", "  test-secret  ", 0o600)

	creds, err := coordinator.LoadCredentials(idPath, secretPath)
	require.NoError(t, err)
	require.Equal(t, "test-machine", creds.ID)
	require.Equal(t, "test-secret", creds.Secret)
}

// A3: TestLoadCredentials_MissingIDFile — wraps fs.ErrNotExist.
func TestLoadCredentials_MissingIDFile(t *testing.T) {
	dir := t.TempDir()
	idPath := filepath.Join(dir, "does-not-exist")
	secretPath := writeFile(t, dir, "secret", "test-secret", 0o600)

	creds, err := coordinator.LoadCredentials(idPath, secretPath)
	require.Nil(t, creds)
	require.Error(t, err)
	require.ErrorIs(t, err, fs.ErrNotExist, "error must wrap fs.ErrNotExist")
}

// A4: TestLoadCredentials_MissingSecretFile — same as A3 for the secret.
func TestLoadCredentials_MissingSecretFile(t *testing.T) {
	dir := t.TempDir()
	idPath := writeFile(t, dir, "id", "test-machine", 0o600)
	secretPath := filepath.Join(dir, "does-not-exist")

	creds, err := coordinator.LoadCredentials(idPath, secretPath)
	require.Nil(t, creds)
	require.Error(t, err)
	require.ErrorIs(t, err, fs.ErrNotExist)
}

// A5: TestLoadCredentials_EmptyIDFile — typed ErrEmptyCredential error.
func TestLoadCredentials_EmptyIDFile(t *testing.T) {
	dir := t.TempDir()
	idPath := writeFile(t, dir, "id", "   \n", 0o600)
	secretPath := writeFile(t, dir, "secret", "test-secret", 0o600)

	creds, err := coordinator.LoadCredentials(idPath, secretPath)
	require.Nil(t, creds)
	require.Error(t, err)
	require.True(t, errors.Is(err, coordinator.ErrEmptyCredential), "want ErrEmptyCredential, got %v", err)
}

// A6: TestLoadCredentials_EmptySecretFile — same for the secret file.
func TestLoadCredentials_EmptySecretFile(t *testing.T) {
	dir := t.TempDir()
	idPath := writeFile(t, dir, "id", "test-machine", 0o600)
	secretPath := writeFile(t, dir, "secret", "", 0o600)

	creds, err := coordinator.LoadCredentials(idPath, secretPath)
	require.Nil(t, creds)
	require.Error(t, err)
	require.True(t, errors.Is(err, coordinator.ErrEmptyCredential))
}

// A7: TestLoadCredentials_PermissiveMode_LoadsWithWarning — D13 says
// 0600. We don't refuse on loose modes (aspirational hardening), but we
// surface a warning so operators see it in logs.
func TestLoadCredentials_PermissiveMode_LoadsWithWarning(t *testing.T) {
	dir := t.TempDir()
	idPath := writeFile(t, dir, "id", "test-machine", 0o644)
	secretPath := writeFile(t, dir, "secret", "test-secret", 0o644)

	creds, err := coordinator.LoadCredentials(idPath, secretPath)
	require.NoError(t, err)
	require.NotNil(t, creds)
	require.Equal(t, "test-machine", creds.ID)
	require.Equal(t, "test-secret", creds.Secret)
	require.Len(t, creds.Warnings, 2, "expected one warning per file")

	// Each warning should name the file it refers to so operators can
	// fix the right one.
	joined := creds.Warnings[0] + "|" + creds.Warnings[1]
	require.Contains(t, joined, "id")
	require.Contains(t, joined, "secret")
}

// A8: TestLoadCredentials_WorldReadableMode_LoadsWithWarning — same as
// A7 with a more egregious mode.
func TestLoadCredentials_WorldReadableMode_LoadsWithWarning(t *testing.T) {
	dir := t.TempDir()
	idPath := writeFile(t, dir, "id", "test-machine", 0o666)
	secretPath := writeFile(t, dir, "secret", "test-secret", 0o666)

	creds, err := coordinator.LoadCredentials(idPath, secretPath)
	require.NoError(t, err)
	require.NotNil(t, creds)
	require.Len(t, creds.Warnings, 2)
}

// A9: TestLoadCredentials_StrictMode_NoWarning — clean 0600 case.
func TestLoadCredentials_StrictMode_NoWarning(t *testing.T) {
	dir := t.TempDir()
	idPath := writeFile(t, dir, "id", "test-machine", 0o600)
	secretPath := writeFile(t, dir, "secret", "test-secret", 0o600)

	creds, err := coordinator.LoadCredentials(idPath, secretPath)
	require.NoError(t, err)
	require.Empty(t, creds.Warnings)
}
