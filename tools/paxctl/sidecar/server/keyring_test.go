package server

import (
	"errors"
	"strings"
	"testing"

	"github.com/sei-protocol/sei-chain/sei-cosmos/crypto/keyring"
)

func TestOpenKeyring_TestBackend(t *testing.T) {
	kr, err := OpenKeyring(BackendTest, t.TempDir(), "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if kr == nil {
		t.Fatal("expected non-nil keyring")
	}
}

func TestOpenKeyring_OSBackend(t *testing.T) {
	// The OS backend on macOS prompts the Security framework on first
	// open; we only assert the factory dispatches without rejecting the
	// backend name. Actual OS-backed key storage is exercised
	// out-of-band by operators.
	kr, err := OpenKeyring(BackendOS, t.TempDir(), "")
	if err != nil {
		t.Skipf("OS backend not available in this environment: %v", err)
	}
	if kr == nil {
		t.Fatal("expected non-nil keyring")
	}
}

func TestOpenKeyring_FileBackend(t *testing.T) {
	kr, err := OpenKeyring(BackendFile, t.TempDir(), "correct-horse-battery-staple")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if kr == nil {
		t.Fatal("expected non-nil keyring")
	}
}

func TestOpenKeyring_FileBackend_EmptyPassphrase(t *testing.T) {
	_, err := OpenKeyring(BackendFile, t.TempDir(), "")
	if err == nil {
		t.Fatal("expected error for empty passphrase")
	}
	if !strings.Contains(err.Error(), "passphrase") {
		t.Fatalf("error should mention passphrase, got: %v", err)
	}
}

func TestOpenKeyring_UnknownBackend(t *testing.T) {
	_, err := OpenKeyring("kms", t.TempDir(), "")
	if err == nil {
		t.Fatal("expected error for unknown backend")
	}
	if !strings.Contains(err.Error(), "unsupported keyring backend") {
		t.Fatalf("error should name the unsupported backend, got: %v", err)
	}
}

func TestRedactPassphrase(t *testing.T) {
	cases := []struct {
		name       string
		in         string
		passphrase string
		want       string
	}{
		{"empty passphrase is a no-op", "no secret here", "", "no secret here"},
		{"verbatim occurrence is replaced", "open failed: pw=hunter2", "hunter2", "open failed: pw=[redacted]"},
		{"absent passphrase is unchanged", "open failed: io error", "hunter2", "open failed: io error"},
		{"multiple occurrences are all replaced", "hunter2 then hunter2", "hunter2", "[redacted] then [redacted]"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := redactPassphrase(tc.in, tc.passphrase)
			if got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestSmokeTestKeyring_HappyPath(t *testing.T) {
	kr, err := OpenKeyring(BackendTest, t.TempDir(), "")
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := SmokeTestKeyring(kr); err != nil {
		t.Fatalf("smoke test failed on healthy keyring: %v", err)
	}
}

func TestSmokeTestKeyring_FailurePath(t *testing.T) {
	// Shrink the retry backoff so the failure path completes in
	// milliseconds instead of seconds.
	prev := smokeTestBackoffTestHook
	smokeTestBackoffTestHook = 0
	t.Cleanup(func() { smokeTestBackoffTestHook = prev })

	err := SmokeTestKeyring(&brokenKeyring{})
	if err == nil {
		t.Fatal("expected smoke test to fail")
	}
	if !strings.Contains(err.Error(), "smoke test failed") {
		t.Fatalf("error should mention smoke test, got: %v", err)
	}
	if !strings.Contains(err.Error(), "keyring backend is sick") {
		t.Fatalf("error should wrap underlying cause, got: %v", err)
	}
}

// brokenKeyring satisfies keyring.Keyring for the smoke-test failure case.
// Only List() is exercised by SmokeTestKeyring; the embedded interface
// gives us nil methods that will panic if anything else touches it,
// which would surface a refactor that broadens the smoke-test surface.
type brokenKeyring struct{ keyring.Keyring }

func (b *brokenKeyring) List() ([]keyring.Info, error) {
	return nil, errors.New("keyring backend is sick")
}
