package config

import (
	"strings"
	"testing"
)

func TestWebhookSecretIsRequiredForReadiness(t *testing.T) {
	c := Config{DatabaseURL: "postgres://fixture:fixture@localhost/fixture", RedisURL: "redis://localhost", LivepeerAPIURL: "https://livepeer.studio/api", LivepeerAPIKey: "fixture", LogLevel: "info", Port: 3000, MaxStreamsPerWallet: 3}
	if c.Validate() == nil {
		t.Fatal("unsigned webhook readiness accepted")
	}
	c.LivepeerWebhookSecret = strings.Repeat("k", 32)
	if e := c.Validate(); e != nil {
		t.Fatal(e)
	}
}

func TestProviderURLAndErrorsDoNotLeakCredentials(t *testing.T) {
	c := Config{DatabaseURL: "not-a-url-private-password", RedisURL: "redis://localhost", LivepeerAPIURL: "http://provider.invalid", LivepeerAPIKey: "fixture", LivepeerWebhookSecret: strings.Repeat("k", 32), LogLevel: "info", Port: 3000, MaxStreamsPerWallet: 3}
	e := c.Validate()
	if e == nil || strings.Contains(e.Error(), "private-password") {
		t.Fatal(e)
	}
}

func TestConnectedProviderRejectsSSRFEndpoints(t *testing.T) {
	for _, endpoint := range []string{"https://127.0.0.1/api", "https://169.254.169.254/api", "https://livepeer.studio.attacker.invalid/api", "https://livepeer.studio:8443/api", "https://livepeer.studio/api/../private", "https://livepeer.studio/api?target=private"} {
		c := Config{DatabaseURL: "postgres://fixture:fixture@localhost/fixture", RedisURL: "redis://localhost", LivepeerAPIURL: endpoint, LivepeerAPIKey: "fixture", LivepeerWebhookSecret: strings.Repeat("k", 32), LogLevel: "info", Port: 3000, MaxStreamsPerWallet: 3}
		if err := c.ValidateConnected(); err == nil || !strings.Contains(err.Error(), "provider_endpoint_invalid") {
			t.Fatal(endpoint, err)
		}
	}
}

func TestTrustedProxiesParsedOutsideProductionOnly(t *testing.T) {
	c := Config{NodeEnv: "development", TrustedProxyCIDRs: " 100.64.0.0/10, fd00::/8 ,"}
	got, err := c.TrustedProxies()
	if err != nil || len(got) != 2 || got[0].String() != "100.64.0.0/10" || got[1].String() != "fd00::/8" {
		t.Fatal(got, err)
	}
	c.TrustedProxyCIDRs = "100.64.0.1"
	if _, err = c.TrustedProxies(); err == nil {
		t.Fatal("bare address accepted as CIDR")
	}
	c.NodeEnv = "production"
	if got, err = c.TrustedProxies(); err != nil || got != nil {
		t.Fatal("production must keep its reviewed client-IP behaviour", got, err)
	}
}

func TestUnavailableProviderWaivesLivepeerSecretsOnlyOutsideProduction(t *testing.T) {
	c := Config{NodeEnv: "development", ProviderMode: "unavailable", DatabaseURL: "postgres://fixture:fixture@localhost/fixture", RedisURL: "redis://localhost", LivepeerAPIURL: "https://livepeer.studio/api", LogLevel: "info", Port: 3000, MaxStreamsPerWallet: 3}
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	c.NodeEnv = "production"
	c.RuntimeProfile = "container"
	if err := c.Validate(); err == nil {
		t.Fatal("production container accepted the unavailable provider without the owned_local fixture")
	}
}
