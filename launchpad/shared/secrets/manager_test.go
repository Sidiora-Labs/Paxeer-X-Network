package secrets

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"
)

func TestNewLogsWarnings(t *testing.T) {
	lg := slog.New(slog.NewTextHandler(io.Discard, nil))
	for _, pt := range []ProviderType{ProviderAWS, ProviderVault, "weird"} {
		if _, ok := New(Options{Provider: pt, Logger: lg}).(EnvProvider); !ok {
			t.Errorf("provider %q with logger did not fall back to EnvProvider", pt)
		}
	}
}

// errProvider is a real Provider implementation whose backend always fails,
// used to exercise the cache's error-propagation path.
type errProvider struct{}

func (errProvider) Get(context.Context, string) (string, bool, error) {
	return "", false, errors.New("backend down")
}
func (errProvider) GetRequired(context.Context, string) (string, error) {
	return "", errors.New("backend down")
}

func TestCachedPropagatesDelegateError(t *testing.T) {
	c := newCached(errProvider{}, time.Minute, nil)
	if _, _, err := c.Get(context.Background(), "k"); err == nil {
		t.Fatal("Get: want delegate error")
	}
	if _, err := c.GetRequired(context.Background(), "k"); err == nil {
		t.Fatal("GetRequired: want delegate error")
	}
}

func TestEnvProvider(t *testing.T) {
	t.Setenv("MY_SECRET", "value123")
	p := EnvProvider{}
	v, ok, err := p.Get(context.Background(), "MY_SECRET")
	if err != nil || !ok || v != "value123" {
		t.Fatalf("Get = %q,%v,%v", v, ok, err)
	}
	if _, ok, _ := p.Get(context.Background(), "MISSING_SECRET_XYZ"); ok {
		t.Error("missing key reported as found")
	}
	if _, err := p.GetRequired(context.Background(), "MISSING_SECRET_XYZ"); err == nil {
		t.Error("GetRequired on missing key should error")
	}
	if got, err := p.GetRequired(context.Background(), "MY_SECRET"); err != nil || got != "value123" {
		t.Errorf("GetRequired = %q,%v", got, err)
	}
}

func TestNewDefaultsToEnv(t *testing.T) {
	t.Setenv("SECRETS_PROVIDER", "")
	if _, ok := New(Options{}).(EnvProvider); !ok {
		t.Error("default provider is not EnvProvider")
	}
}

func TestNewUnknownAndCloudFallBackToEnv(t *testing.T) {
	for _, pt := range []ProviderType{ProviderAWS, ProviderVault, "weird"} {
		if _, ok := New(Options{Provider: pt}).(EnvProvider); !ok {
			t.Errorf("provider %q did not fall back to EnvProvider", pt)
		}
	}
}

func TestCachedProviderCachesAndExpires(t *testing.T) {
	t.Setenv("CACHED_KEY", "v1")
	now := time.Unix(1_000_000, 0)
	clock := func() time.Time { return now }
	p := New(Options{CacheTTL: time.Minute, now: clock})

	v, ok, _ := p.Get(context.Background(), "CACHED_KEY")
	if !ok || v != "v1" {
		t.Fatalf("first Get = %q,%v", v, ok)
	}
	// Change the env; cache should still serve the old value within TTL.
	t.Setenv("CACHED_KEY", "v2")
	if v, _, _ := p.Get(context.Background(), "CACHED_KEY"); v != "v1" {
		t.Errorf("cached Get = %q, want v1 (still within TTL)", v)
	}
	// Advance beyond TTL -> refetch.
	now = now.Add(2 * time.Minute)
	if v, _, _ := p.Get(context.Background(), "CACHED_KEY"); v != "v2" {
		t.Errorf("post-expiry Get = %q, want v2", v)
	}
}

func TestCachedGetRequired(t *testing.T) {
	t.Setenv("REQ_KEY", "present")
	p := New(Options{CacheTTL: time.Minute})
	if got, err := p.GetRequired(context.Background(), "REQ_KEY"); err != nil || got != "present" {
		t.Errorf("GetRequired = %q,%v", got, err)
	}
	if _, err := p.GetRequired(context.Background(), "ABSENT_KEY_QQ"); err == nil {
		t.Error("GetRequired on absent key should error")
	}
}

func TestNewCheckedUnsupportedProviderWithLoggerFailsClosed(t *testing.T) {
	lg := slog.New(slog.NewTextHandler(io.Discard, nil))
	for _, pt := range []ProviderType{ProviderAWS, ProviderVault, "weird"} {
		if p, err := NewChecked(Options{Provider: pt, Logger: lg}); err == nil || p != nil {
			t.Errorf("provider %q with logger must fail closed", pt)
		}
	}
}

func TestNewCheckedUnknownAndCloudFailClosed(t *testing.T) {
	t.Setenv("CONFIGURED_PROVIDER_SECRET", "must-not-be-used")
	for _, pt := range []ProviderType{ProviderAWS, ProviderVault, "weird"} {
		if p, err := NewChecked(Options{Provider: pt}); err == nil || p != nil {
			t.Errorf("provider %q must fail closed", pt)
		}
		t.Setenv("SECRETS_PROVIDER", string(pt))
		if p, err := NewChecked(Options{}); err == nil || p != nil {
			t.Errorf("configured provider %q must fail closed", pt)
		}
	}
}

func TestNewCheckedProductionCacheBounds(t *testing.T) {
	for _, ttl := range []time.Duration{-time.Nanosecond, time.Minute + time.Nanosecond} {
		if p, err := NewChecked(Options{Provider: ProviderEnv, CacheTTL: ttl}); err == nil || p != nil {
			t.Errorf("cache TTL %v must fail startup", ttl)
		}
	}
	t.Setenv("CHECKED_ROTATING_SECRET", "first")
	now := time.Unix(1_000_000, 0)
	p, err := NewChecked(Options{Provider: ProviderEnv, CacheTTL: time.Minute, now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	if got, err := p.GetRequired(context.Background(), "CHECKED_ROTATING_SECRET"); err != nil || got != "first" {
		t.Fatalf("initial secret = %q, %v", got, err)
	}
	t.Setenv("CHECKED_ROTATING_SECRET", "rotated")
	now = now.Add(time.Minute)
	if got, err := p.GetRequired(context.Background(), "CHECKED_ROTATING_SECRET"); err != nil || got != "rotated" {
		t.Fatalf("rotation at maximum cache age = %q, %v", got, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := p.Get(ctx, "CHECKED_ROTATING_SECRET"); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled cached lookup = %v", err)
	}
}

func TestNewEnvironmentSelectionHonorsConfiguredAuthority(t *testing.T) {
	t.Setenv("CONFIGURED_PROVIDER_SECRET", "development-secret")
	for _, provider := range []string{"aws", "vault", "unavailable-provider"} {
		t.Setenv("SECRETS_PROVIDER", provider)
		if _, _, err := New(Options{}).Get(context.Background(), "CONFIGURED_PROVIDER_SECRET"); err == nil {
			t.Fatalf("environment-selected provider %q used environment secret", provider)
		}
		p := New(Options{Provider: ProviderEnv})
		if got, err := p.GetRequired(context.Background(), "CONFIGURED_PROVIDER_SECRET"); err != nil || got != "development-secret" {
			t.Fatalf("explicit development env override = %q, %v", got, err)
		}
	}
}
