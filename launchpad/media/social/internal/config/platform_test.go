package config

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"strings"
	"testing"
)

// platformSigningKey is a well-formed ed25519 assertion key (seed||public) in
// the standard base64 form IDENTITY_ASSERTION_SIGNING_KEY carries.
func platformSigningKey(seed byte) string {
	return base64.StdEncoding.EncodeToString(ed25519.NewKeyFromSeed([]byte(strings.Repeat(string(rune(seed)), ed25519.SeedSize))))
}

func platformConfig() Config {
	return Config{NodeEnv: "production", RuntimeProfile: PlatformProfile, Port: 8080, IdentityURL: "https://gateway.railway.internal:8443",
		IdentityMTLSCAFile: "/etc/sidiora/pki/ca.pem", IdentityMTLSClientCertFile: "/etc/sidiora/pki/social-client.pem", IdentityMTLSClientKeyPEM: "-----BEGIN PRIVATE KEY-----", IdentityMTLSPeerCertSHA256: strings.Repeat("ab", 32), IdentitySigningKey: platformSigningKey('s'),
		PrivateListenAddress: "[::]:8443", PrivateTLSCertFile: "/etc/sidiora/pki/social-server.pem", PrivateTLSKeyPEM: "-----BEGIN PRIVATE KEY-----", PrivateTLSClientCAFile: "/etc/sidiora/pki/ca.pem", GatewayPeerCertSHA256: strings.Repeat("cd", 32), AuthorityPeers: "ranking.railway.internal=" + strings.Repeat("ef", 32),
		SocialCursorKey: strings.Repeat("A", 43), CORSAllowedOrigins: "https://web.example", DBMaxConns: 20, SecretsProvider: "env", SecretsRefreshSeconds: 30,
		ExpectedSchemaFile: "/etc/sidiora/social/expected-schema.json", ExpectedSchemaSHA256: strings.Repeat("0f", 32)}
}

func TestPlatformProfileSelection(t *testing.T) {
	c := platformConfig()
	if !c.Platform() {
		t.Fatal("production + platform profile not selected")
	}
	for _, other := range []Config{{NodeEnv: "development", RuntimeProfile: PlatformProfile}, {NodeEnv: "production", RuntimeProfile: "container"}, {NodeEnv: "production"}} {
		if other.Platform() {
			t.Fatalf("selected for %+v", other)
		}
	}
	if err := c.ValidatePlatform(); err != nil {
		t.Fatal(err)
	}
}

func TestPlatformIdentityEndpointTransportRule(t *testing.T) {
	for raw, want := range map[string]string{"https://identity.example": "https://identity.example", "https://gateway.railway.internal:8443": "https://gateway.railway.internal:8443", "https://gateway.example/": "https://gateway.example"} {
		c := Config{IdentityURL: raw}
		got, err := c.PlatformIdentityEndpoint()
		if err != nil || got != want {
			t.Fatalf("%s: %q %v", raw, got, err)
		}
	}
	// The gateway serves /internal/v2/identity/* only on its mTLS listener:
	// every plain-HTTP origin is refused, the private network included.
	for _, raw := range []string{"", "http://gateway.railway.internal:8080", "http://127.0.0.1:9000", "http://localhost:1", "http://gateway.example:8080", "https://u:p@identity.example", "https://identity.example/v2", "https://identity.example?x=1", "ftp://identity.example", "gateway.railway.internal:8443"} {
		c := Config{IdentityURL: raw}
		if _, err := c.PlatformIdentityEndpoint(); err == nil {
			t.Fatalf("%q accepted", raw)
		}
	}
}

func TestPlatformIdentityClientAndAuthorityPeers(t *testing.T) {
	c := platformConfig()
	client, err := c.PlatformIdentityClient()
	if err != nil || client.Issuer != "social" || client.Endpoint != "https://gateway.railway.internal:8443" || client.PeerCertSHA256 != c.IdentityMTLSPeerCertSHA256 || client.ClientKeyPEM != c.IdentityMTLSClientKeyPEM || client.SigningKey != c.IdentitySigningKey {
		t.Fatalf("identity client %+v %v", client, err)
	}
	c.AuthorityPeers = " Ranking.Railway.Internal=" + strings.Repeat("ef", 32) + ", metadata.railway.internal=" + strings.Repeat("01", 32) + ","
	peers, err := c.PlatformAuthorityPeers()
	if err != nil || len(peers) != 2 || peers["ranking.railway.internal"] != strings.Repeat("ef", 32) || peers["metadata.railway.internal"] != strings.Repeat("01", 32) {
		t.Fatalf("authority peers %v %v", peers, err)
	}
	for _, raw := range []string{"ranking.railway.internal", "ranking.railway.internal=" + strings.Repeat("EF", 32), "=" + strings.Repeat("ef", 32), "ranking.railway.internal=" + strings.Repeat("ef", 31), "a=" + strings.Repeat("ef", 32) + ",a=" + strings.Repeat("01", 32)} {
		c.AuthorityPeers = raw
		if _, err := c.PlatformAuthorityPeers(); err == nil {
			t.Fatalf("%q accepted", raw)
		}
	}
}

func TestValidatePlatformRejects(t *testing.T) {
	cases := map[string]func(*Config){
		"plain identity":     func(c *Config) { c.IdentityURL = "http://gateway.railway.internal:8080" },
		"no identity ca":     func(c *Config) { c.IdentityMTLSCAFile = "" },
		"no client cert":     func(c *Config) { c.IdentityMTLSClientCertFile = "" },
		"no client key":      func(c *Config) { c.IdentityMTLSClientKeyPEM = "" },
		"two client keys":    func(c *Config) { c.IdentityMTLSClientKeyFile = "/etc/sidiora/pki/social-client.key" },
		"gateway pin":        func(c *Config) { c.IdentityMTLSPeerCertSHA256 = strings.Repeat("AB", 32) },
		"signing key":        func(c *Config) { c.IdentitySigningKey = base64.StdEncoding.EncodeToString(make([]byte, 64)) },
		"no signing key":     func(c *Config) { c.IdentitySigningKey = "" },
		"listen on port":     func(c *Config) { c.PrivateListenAddress = "0.0.0.0:8080" },
		"listen host name":   func(c *Config) { c.PrivateListenAddress = "social.railway.internal:8443" },
		"no listen":          func(c *Config) { c.PrivateListenAddress = "" },
		"no server cert":     func(c *Config) { c.PrivateTLSCertFile = "" },
		"no server key":      func(c *Config) { c.PrivateTLSKeyPEM = "" },
		"no client ca":       func(c *Config) { c.PrivateTLSClientCAFile = "" },
		"no gateway pin":     func(c *Config) { c.GatewayPeerCertSHA256 = "" },
		"authority peers":    func(c *Config) { c.AuthorityPeers = "ranking.railway.internal" },
		"padded cursor":      func(c *Config) { c.SocialCursorKey = strings.Repeat("A", 43) + "=" },
		"short cursor":       func(c *Config) { c.SocialCursorKey = strings.Repeat("A", 40) },
		"no cors":            func(c *Config) { c.CORSAllowedOrigins = "" },
		"wildcard cors":      func(c *Config) { c.CORSAllowedOrigins = "*" },
		"http cors":          func(c *Config) { c.CORSAllowedOrigins = "http://web.example" },
		"short admin key":    func(c *Config) { c.ChatAdminAPIKey = "admin" },
		"pool bound":         func(c *Config) { c.DBMaxConns = 0 },
		"loopback bind":      func(c *Config) { c.BindAddress = "127.0.0.1" },
		"public identity":    func(c *Config) { c.IdentityURL = "http://gateway.example" },
		"missing identity":   func(c *Config) { c.IdentityURL = "" },
		"oversize pool":      func(c *Config) { c.DBMaxConns = 201 },
		"cors with path":     func(c *Config) { c.CORSAllowedOrigins = "https://web.example/app" },
		"cors second broken": func(c *Config) { c.CORSAllowedOrigins = "https://web.example,https://" },
		"no expected schema": func(c *Config) { c.ExpectedSchemaFile = "" },
		"no schema digest":   func(c *Config) { c.ExpectedSchemaSHA256 = "" },
		"upper digest":       func(c *Config) { c.ExpectedSchemaSHA256 = strings.Repeat("0F", 32) },
	}
	for name, mutate := range cases {
		c := platformConfig()
		mutate(&c)
		if err := c.ValidatePlatform(); err == nil {
			t.Fatalf("%s accepted", name)
		}
	}
}

func TestPlatformSecretsResolveAndValidate(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://kl:kl@localhost:5432/social")
	t.Setenv("REDIS_URL", "redis://localhost:6379")
	t.Setenv("IDENTITY_ASSERTION_SIGNING_KEY", platformSigningKey('k'))
	t.Setenv("IDENTITY_MTLS_CLIENT_KEY_PEM", "")
	t.Setenv("SOCIAL_TLS_KEY_PEM", "server-key-pem")
	t.Setenv("SOCIAL_CURSOR_KEY", strings.Repeat("B", 42)+"A")
	c := platformConfig()
	c.IdentitySigningKey, c.SocialCursorKey = "", ""
	check, stop, err := c.ResolveProductionSecrets(context.Background())
	if err == nil || !strings.Contains(err.Error(), "IDENTITY_MTLS_CLIENT_KEY_PEM") {
		if err == nil {
			stop()
		}
		t.Fatalf("client key PEM secret not required: %v", err)
	}
	t.Setenv("IDENTITY_MTLS_CLIENT_KEY_PEM", "client-key-pem")
	c = platformConfig()
	c.IdentitySigningKey, c.SocialCursorKey = "", ""
	check, stop, err = c.ResolveProductionSecrets(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	if c.IdentitySigningKey != platformSigningKey('k') || c.SocialCursorKey != strings.Repeat("B", 42)+"A" || c.IdentityMTLSClientKeyPEM != "client-key-pem" || c.PrivateTLSKeyPEM != "server-key-pem" {
		t.Fatal("platform secrets not resolved from the provider")
	}
	if err = check(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SOCIAL_CURSOR_KEY", strings.Repeat("C", 42)+"A")
	if err = check(context.Background()); err == nil || !strings.Contains(err.Error(), "rotated") {
		t.Fatalf("rotation not detected: %v", err)
	}
	t.Setenv("SOCIAL_CURSOR_KEY", "short")
	c2 := platformConfig()
	if _, _, err = c2.ResolveProductionSecrets(context.Background()); err == nil {
		t.Fatal("invalid resolved cursor key accepted")
	}
}
