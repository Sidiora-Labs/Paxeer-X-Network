package main

import (
	"os"
	"strings"
	"testing"
)

// TestBuildExecutionConfig_UnsetReturnsZero verifies the Phase-1 default:
// no SEI_KEYRING_BACKEND means no keyring, no error, sidecar boots normally.
func TestBuildExecutionConfig_UnsetReturnsZero(t *testing.T) {
	withEnv(t, map[string]string{
		"SEI_KEYRING_BACKEND":    "",
		"SEI_KEYRING_DIR":        "",
		"SEI_KEYRING_PASSPHRASE": "",
	})

	cfg, err := buildExecutionConfig(t.TempDir())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Keyring != nil {
		t.Fatal("expected nil keyring when SEI_KEYRING_BACKEND is unset")
	}
}

func TestBuildExecutionConfig_UnknownBackendFailsStartup(t *testing.T) {
	withEnv(t, map[string]string{
		"SEI_KEYRING_BACKEND":    "kms",
		"SEI_KEYRING_PASSPHRASE": "",
	})

	_, err := buildExecutionConfig(t.TempDir())
	if err == nil {
		t.Fatal("expected error for unknown backend")
	}
	if !strings.Contains(err.Error(), "unsupported") {
		t.Fatalf("error should mention unsupported, got: %v", err)
	}
}

func TestBuildExecutionConfig_FileBackend_MissingPassphraseFailsStartup(t *testing.T) {
	withEnv(t, map[string]string{
		"SEI_KEYRING_BACKEND":    "file",
		"SEI_KEYRING_PASSPHRASE": "",
	})

	_, err := buildExecutionConfig(t.TempDir())
	if err == nil {
		t.Fatal("expected error for missing passphrase")
	}
	if !strings.Contains(err.Error(), "SEI_KEYRING_PASSPHRASE") {
		t.Fatalf("error should name the missing env, got: %v", err)
	}
}

func TestBuildExecutionConfig_FileBackend_WipesPassphrase(t *testing.T) {
	const passphrase = "do-not-leak-this"
	withEnv(t, map[string]string{
		"SEI_KEYRING_BACKEND":    "file",
		"SEI_KEYRING_DIR":        t.TempDir(),
		"SEI_KEYRING_PASSPHRASE": passphrase,
	})

	cfg, err := buildExecutionConfig(t.TempDir())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Keyring == nil {
		t.Fatal("expected non-nil keyring")
	}
	if got := os.Getenv("SEI_KEYRING_PASSPHRASE"); got != "" {
		t.Fatalf("passphrase env should be wiped, found %q", got)
	}
}

// TestBuildExecutionConfig_NoPassphraseInError asserts that for every
// path that returns a non-nil error from buildExecutionConfig, the
// passphrase is absent from the error message. Each case is a distinct
// failure mode and the test serves as a regression guard against future
// error-construction changes that might interpolate the env value.
func TestBuildExecutionConfig_NoPassphraseInError(t *testing.T) {
	const passphrase = "do-not-leak-this-passphrase"
	cases := []struct {
		name    string
		backend string
	}{
		{"unknown backend", "kms"},
		{"empty passphrase on file backend", "file"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := map[string]string{
				"SEI_KEYRING_BACKEND": tc.backend,
			}
			if tc.name == "unknown backend" {
				env["SEI_KEYRING_PASSPHRASE"] = passphrase
			}
			withEnv(t, env)

			_, err := buildExecutionConfig(t.TempDir())
			if err == nil {
				t.Fatal("expected error")
			}
			if strings.Contains(err.Error(), passphrase) {
				t.Fatalf("passphrase leaked into error: %v", err)
			}
		})
	}
}

func TestBuildExecutionConfig_TestBackend(t *testing.T) {
	withEnv(t, map[string]string{
		"SEI_KEYRING_BACKEND":    "test",
		"SEI_KEYRING_DIR":        t.TempDir(),
		"SEI_KEYRING_PASSPHRASE": "",
	})

	cfg, err := buildExecutionConfig(t.TempDir())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Keyring == nil {
		t.Fatal("expected non-nil keyring for test backend")
	}
}

// withEnv sets the supplied env vars for the duration of the test and
// restores prior values via t.Cleanup. Empty values are honored as
// "unset" so callers can express the Phase-1 default explicitly.
func withEnv(t *testing.T, kv map[string]string) {
	t.Helper()
	for k, v := range kv {
		prev, had := os.LookupEnv(k)
		if v == "" {
			_ = os.Unsetenv(k)
		} else {
			_ = os.Setenv(k, v)
		}
		t.Cleanup(func() {
			if had {
				_ = os.Setenv(k, prev)
			} else {
				_ = os.Unsetenv(k)
			}
		})
	}
}
