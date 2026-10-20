package config

import (
	"strings"
	"testing"
)

func TestPlatformProfileRules(t *testing.T) {
	c := Config{NodeEnv: "production", RuntimeProfile: PlatformProfile, ProviderMode: "unavailable", ViewerTokenKey: strings.Repeat("k", 32), DBMaxConns: 20, CORSAllowedOrigins: "https://web.example", TrustedProxyCIDRs: "100.64.0.0/10", DatabaseURL: "postgres://fixture:fixture@localhost/fixture", RedisURL: "redis://localhost", LivepeerAPIURL: "https://livepeer.studio/api", LogLevel: "info", Port: 8080, MaxStreamsPerWallet: 3,
		PrivateListenAddress: "[::]:8443", PrivateTLSCertFile: "/etc/sidiora/pki/livestream/server.pem", PrivateTLSKeyPEM: "-----BEGIN PRIVATE KEY-----", PrivateTLSClientCAFile: "/etc/sidiora/pki/ca/ca.pem", GatewayPeerCertSHA256: strings.Repeat("cd", 32),
		ExpectedSchemaFile: "/etc/sidiora/livestream/expected-schema.json", ExpectedSchemaSHA256: strings.Repeat("ef", 32)}
	if !c.Platform() {
		t.Fatal("platform not selected")
	}
	if err := c.Validate(); err != nil {
		t.Fatalf("unavailable provider refused on the platform runtime: %v", err)
	}
	if err := c.ValidatePlatform(); err != nil {
		t.Fatal(err)
	}
	if proxies, err := c.TrustedProxies(); err != nil || len(proxies) != 1 {
		t.Fatalf("platform must honour TRUSTED_PROXY_CIDRS: %v %v", proxies, err)
	}
	bound := c
	bound.RuntimeProfile = "container"
	if proxies, err := bound.TrustedProxies(); err != nil || proxies != nil {
		t.Fatalf("bound production must ignore TRUSTED_PROXY_CIDRS: %v %v", proxies, err)
	}
	if err := bound.ValidateUnavailableProvider(); err == nil {
		t.Fatal("bound production admitted the unavailable provider without a private fixture")
	}
	for name, mutate := range map[string]func(*Config){
		"https cors only":       func(c *Config) { c.CORSAllowedOrigins = "http://web.example" },
		"pool bound":            func(c *Config) { c.DBMaxConns = 0 },
		"bind":                  func(c *Config) { c.BindAddress = "127.0.0.1" },
		"no private listen":     func(c *Config) { c.PrivateListenAddress = "" },
		"listen host name":      func(c *Config) { c.PrivateListenAddress = "livestream.railway.internal:8443" },
		"listen on port":        func(c *Config) { c.PrivateListenAddress = "[::]:8080" },
		"no server cert":        func(c *Config) { c.PrivateTLSCertFile = "" },
		"no server key":         func(c *Config) { c.PrivateTLSKeyPEM = "" },
		"two server keys":       func(c *Config) { c.PrivateTLSKeyFile = "/etc/sidiora/pki/livestream/server.key" },
		"no client ca":          func(c *Config) { c.PrivateTLSClientCAFile = "" },
		"no gateway pin":        func(c *Config) { c.GatewayPeerCertSHA256 = "" },
		"uppercase gateway pin": func(c *Config) { c.GatewayPeerCertSHA256 = strings.Repeat("CD", 32) },
		"no expected schema":    func(c *Config) { c.ExpectedSchemaFile = "" },
		"short schema digest":   func(c *Config) { c.ExpectedSchemaSHA256 = "ef" },
		"uppercase digest":      func(c *Config) { c.ExpectedSchemaSHA256 = strings.Repeat("EF", 32) },
	} {
		bad := c
		mutate(&bad)
		if bad.ValidatePlatform() == nil {
			t.Fatalf("%s accepted", name)
		}
	}
	for name, tc := range map[string]struct {
		file   string
		inline func(*Config)
		clear  func(*Config)
	}{
		"LIVESTREAM_TLS_CERT_PEM":      {"LIVESTREAM_TLS_CERT_FILE", func(c *Config) { c.PrivateTLSCertPEM = "-----BEGIN CERTIFICATE-----" }, func(c *Config) { c.PrivateTLSCertFile = "" }},
		"LIVESTREAM_TLS_CLIENT_CA_PEM": {"LIVESTREAM_TLS_CLIENT_CA_FILE", func(c *Config) { c.PrivateTLSClientCAPEM = "-----BEGIN CERTIFICATE-----" }, func(c *Config) { c.PrivateTLSClientCAFile = "" }},
	} {
		inline := c
		tc.inline(&inline)
		tc.clear(&inline)
		if err := inline.ValidatePlatform(); err != nil {
			t.Fatalf("%s alone refused: %v", name, err)
		}
		both := c
		tc.inline(&both)
		if err := both.ValidatePlatform(); err == nil || !strings.Contains(err.Error(), tc.file+" and "+name) {
			t.Fatalf("%s with %s accepted or unnamed: %v", name, tc.file, err)
		}
	}
	if got, err := Material("/nonexistent/livestream.pem", "inline"); err != nil || string(got) != "inline" {
		t.Fatalf("inline material: %q %v", got, err)
	}
}

func TestValidateNamesBothDocumentForms(t *testing.T) {
	c := Config{BudgetPolicyFile: "/etc/sidiora/livestream/budget.json", BudgetPolicyDocument: "e30="}
	if err := c.Validate(); err == nil || !strings.Contains(err.Error(), "LIVESTREAM_BUDGET_POLICY_FILE and LIVESTREAM_BUDGET_POLICY_DOCUMENT are both set") {
		t.Fatalf("both forms not named: %v", err)
	}
}
