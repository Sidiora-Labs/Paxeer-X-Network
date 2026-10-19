package server

import (
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"

	"github.com/sei-protocol/sei-chain/sei-cosmos/crypto/keyring"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
)

// Values accepted by SEI_KEYRING_BACKEND. Aliased so the env-contract
// surface lives in one place.
const (
	BackendTest = keyring.BackendTest
	BackendFile = keyring.BackendFile
	BackendOS   = keyring.BackendOS
)

// AllowedBackends is the narrow set supported today; KMS / Vault are deferred.
var AllowedBackends = []string{BackendTest, BackendFile, BackendOS}

// Smoke-test retry window absorbs the kubelet Secret-mount race;
// beyond this the keyring is genuinely broken and the pod fails fast.
const (
	smokeTestAttempts = 3
	smokeTestBackoff  = 2 * time.Second
)

// Override hook for tests so failure paths don't wait the full 6 seconds.
var smokeTestBackoffTestHook = smokeTestBackoff

// OpenKeyring constructs a Cosmos SDK keyring for the given backend.
// For file backend, the passphrase is fed twice because the underlying
// 99designs/keyring asks for it twice on key-creation paths.
// Caller is responsible for unsetting SEI_KEYRING_PASSPHRASE post-return.
func OpenKeyring(backend, dir, passphrase string) (keyring.Keyring, error) {
	var input io.Reader
	rootDir := dir
	switch backend {
	case BackendTest, BackendOS:
		// rootDir honored as-is; no passphrase prompt.
	case BackendFile:
		if passphrase == "" {
			return nil, fmt.Errorf("keyring backend %q requires a passphrase", backend)
		}
		input = strings.NewReader(passphrase + "\n" + passphrase + "\n")
		// SDK appends "keyring-file" internally; strip a trailing match
		// so callers passing either /sei or /sei/keyring-file converge.
		if filepath.Base(dir) == "keyring-file" {
			rootDir = filepath.Dir(dir)
		}
	default:
		return nil, fmt.Errorf("unsupported keyring backend %q (allowed: %s)",
			backend, strings.Join(AllowedBackends, "|"))
	}

	kr, err := keyring.New(sdk.KeyringServiceName(), backend, rootDir, input)
	if err != nil {
		// errors.New severs the chain so a typed field embedding the
		// passphrase cannot resurface via a caller's %w or %v of a wrap.
		return nil, errors.New("open keyring: " + redactPassphrase(err.Error(), passphrase))
	}
	return kr, nil
}

// redactPassphrase strips verbatim occurrences of the passphrase.
// Defensive: the SDK isn't known to leak, but the guard is cheap.
func redactPassphrase(s, passphrase string) string {
	if passphrase == "" {
		return s
	}
	return strings.ReplaceAll(s, passphrase, "[redacted]")
}

// SmokeTestKeyring verifies the keyring is structurally usable.
// An empty keyring is permitted; first sign-tx surfaces missing keys.
// Panic recovery exists so the retry loop runs even if the underlying
// lib panics on a malformed config.
func SmokeTestKeyring(kr keyring.Keyring) error {
	var lastErr error
	for attempt := 1; attempt <= smokeTestAttempts; attempt++ {
		err := smokeTestAttempt(kr)
		if err == nil {
			return nil
		}
		lastErr = err
		if attempt < smokeTestAttempts {
			time.Sleep(smokeTestBackoffTestHook)
		}
	}
	return fmt.Errorf("keyring smoke test failed after %d attempts: %w",
		smokeTestAttempts, lastErr)
}

func smokeTestAttempt(kr keyring.Keyring) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("keyring backend panicked during smoke test: %v", r)
		}
	}()
	// List decrypts only the index — strongest non-destructive check.
	_, err = kr.List()
	return err
}
