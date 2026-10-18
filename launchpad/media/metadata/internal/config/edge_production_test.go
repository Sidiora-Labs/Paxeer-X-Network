package config_test

import (
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Sidiora-Technologies/KindleLaunch/media/metadata/internal/config"
)

const publicationDocument = `{"schemaVersion":3,"environment":"paxeer125-production","envelopeHeader":"X-Kindle-Operation","signatureHeader":"X-Kindle-Operation-Signature"}`

func digest(b []byte) string { return fmt.Sprintf("%x", sha256.Sum256(b)) }

// edgeConfig is a complete production configuration for platform-edge
// ingress (Railway): no mTLS material, session introspection identity,
// rlimit raster isolation and the database visibility source.
func edgeConfig() config.Config {
	return config.Config{
		NodeEnv:                    "production",
		Ingress:                    config.IngressEdge,
		RasterIsolation:            config.IsolationRlimit,
		ModerationSource:           config.ModerationDatabase,
		ModerationSchema:           "chat",
		MetadataSchema:             "metadata",
		CanonicalSchema:            "indexer",
		IdentityAudience:           "https://metadata-production-3367.up.railway.app",
		IdentityServiceURL:         "http://gateway.railway.internal:8080",
		IdentityServiceToken:       strings.Repeat("t", 48),
		QuarantineBucket:           "metadata-quarantine",
		ApprovedBucket:             "metadata-media",
		RasterExecutable:           "/app/app",
		ClamAVHost:                 "clamav.railway.internal",
		ClamAVPort:                 3310,
		PublicURL:                  "https://metadata-production-3367.up.railway.app",
		PublicationConfigDocument:  base64.StdEncoding.EncodeToString([]byte(publicationDocument)),
		PublicationConfigSHA256:    digest([]byte(publicationDocument)),
		PublicationAuditKey:        base64.StdEncoding.EncodeToString([]byte(strings.Repeat("k", 32))),
		MediaScanIntervalSeconds:   5,
		MediaExpiryIntervalSeconds: 300,
	}
}

func TestEdgeProductionConfigurationValid(t *testing.T) {
	cfg := edgeConfig()
	if err := cfg.ValidateMediaRuntime(); err != nil {
		t.Fatal(err)
	}
	runtime, key, err := cfg.ReadPublicationRuntime()
	if err != nil || runtime.Environment != "paxeer125-production" || runtime.EnvelopeHeader != "X-Kindle-Operation" || len(key) != 32 {
		t.Fatal(runtime, err)
	}
}

func TestEdgeProductionConfigurationFailsClosed(t *testing.T) {
	cases := map[string]func(*config.Config){
		"unknown ingress":              func(c *config.Config) { c.Ingress = "public" },
		"unknown isolation":            func(c *config.Config) { c.RasterIsolation = "none" },
		"unknown moderation source":    func(c *config.Config) { c.ModerationSource = "allow-all" },
		"edge with service moderation": func(c *config.Config) { c.ModerationSource = config.ModerationService },
		"identity url missing":         func(c *config.Config) { c.IdentityServiceURL = "" },
		"identity over public http":    func(c *config.Config) { c.IdentityServiceURL = "http://gateway-production-800c.up.railway.app" },
		"identity url with path":       func(c *config.Config) { c.IdentityServiceURL = "https://gateway.example/internal" },
		"short service token":          func(c *config.Config) { c.IdentityServiceToken = strings.Repeat("t", 31) },
		"audience missing":             func(c *config.Config) { c.IdentityAudience = "" },
		"shared bucket":                func(c *config.Config) { c.QuarantineBucket = c.ApprovedBucket },
		"relative worker":              func(c *config.Config) { c.RasterExecutable = "app" },
		"cgroup without parent":        func(c *config.Config) { c.RasterIsolation = config.IsolationCgroup },
		"scanner missing":              func(c *config.Config) { c.ClamAVHost = "" },
		"public origin over http":      func(c *config.Config) { c.PublicURL = "http://metadata.example" },
		"public origin with path":      func(c *config.Config) { c.PublicURL = "https://metadata.example/x" },
		"moderation schema public":     func(c *config.Config) { c.ModerationSchema = "public" },
		"publication doc and file":     func(c *config.Config) { c.PublicationConfigFile = "/run/kindle/publication.json" },
		"publication doc missing":      func(c *config.Config) { c.PublicationConfigDocument = "" },
		"audit key short":              func(c *config.Config) { c.PublicationAuditKey = base64.StdEncoding.EncodeToString([]byte("short")) },
		"trusted proxy everything":     func(c *config.Config) { c.TrustedProxyCIDRs = "0.0.0.0/0" },
		"trusted proxy unmasked":       func(c *config.Config) { c.TrustedProxyCIDRs = "10.1.2.3/8" },
	}
	for name, mutate := range cases {
		cfg := edgeConfig()
		mutate(&cfg)
		if err := cfg.ValidateMediaRuntime(); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

// The private-channel (mtls) profile keeps every requirement it had.
func TestMTLSProductionConfigurationUnchanged(t *testing.T) {
	cfg := edgeConfig()
	cfg.Ingress = config.IngressMTLS
	if err := cfg.ValidateMediaRuntime(); err == nil || err.Error() != "metadata_mtls_configuration_missing" {
		t.Fatal("mtls ingress without TLS material accepted:", err)
	}
	cfg.TLSCertFile, cfg.TLSKeyFile, cfg.TLSClientCAFile = "/run/tls/tls.crt", "/run/tls/tls.key", "/run/tls/ca.crt"
	if err := cfg.ValidateMediaRuntime(); err == nil || err.Error() != "metadata_gateway_authority_missing" {
		t.Fatal(err)
	}
	cfg.GatewayPublicKeyHex, cfg.GatewayPeerCertSHA256 = strings.Repeat("a", 64), strings.Repeat("b", 64)
	if err := cfg.ValidateMediaRuntime(); err == nil || err.Error() != "metadata_identity_authority_missing" {
		t.Fatal(err)
	}
	cfg.MetadataAssertionPrivateKeyHex, cfg.IdentityPeerCertSHA256, cfg.UserPeerCertSHA256, cfg.UserAssertionPublicKeyHex = strings.Repeat("c", 128), strings.Repeat("d", 64), strings.Repeat("e", 64), strings.Repeat("f", 64)
	cfg.IdentityEndpoint = "https://identity.internal.example"
	if err := cfg.ValidateMediaRuntime(); err != nil {
		t.Fatal(err)
	}
	cfg.ModerationSource = config.ModerationService
	if err := cfg.ValidateMediaRuntime(); err == nil {
		t.Fatal("service moderation without pinned endpoint accepted")
	}
	cfg.ModerationPeerCertSHA256, cfg.ModerationEndpoint, cfg.ModerationBearerKey = strings.Repeat("9", 64), "https://social.internal.example", "MODERATION_SERVICE_ACCESS_TOKEN"
	if err := cfg.ValidateMediaRuntime(); err != nil {
		t.Fatal(err)
	}
}

func TestBoundRuntimeDocumentSources(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "publication.json")
	if err := os.WriteFile(path, []byte(publicationDocument), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := edgeConfig()
	cfg.PublicationConfigDocument, cfg.PublicationConfigFile = "", path
	if _, _, err := cfg.ReadPublicationRuntime(); err != nil {
		t.Fatal("file source:", err)
	}
	for name, mutate := range map[string]func(*config.Config){
		"both sources": func(c *config.Config) {
			c.PublicationConfigDocument = base64.StdEncoding.EncodeToString([]byte(publicationDocument))
		},
		"digest drift":   func(c *config.Config) { c.PublicationConfigSHA256 = digest([]byte(publicationDocument + " ")) },
		"no source":      func(c *config.Config) { c.PublicationConfigFile = "" },
		"inline garbage": func(c *config.Config) { c.PublicationConfigFile, c.PublicationConfigDocument = "", "%%%" },
		"inline altered": func(c *config.Config) {
			c.PublicationConfigFile, c.PublicationConfigDocument = "", base64.StdEncoding.EncodeToString([]byte(strings.Replace(publicationDocument, "production", "staging", 1)))
		},
	} {
		c := cfg
		mutate(&c)
		if _, _, err := c.ReadPublicationRuntime(); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestPrivateServiceOrigin(t *testing.T) {
	for raw, want := range map[string]bool{
		"https://gateway-production-800c.up.railway.app": true,
		"http://gateway.railway.internal:8080":           true,
		"http://gateway.railway.internal":                true,
		"http://.railway.internal":                       false,
		"http://railway.internal":                        false,
		"http://gateway.railway.internal.evil.example":   false,
		"http://gateway-production-800c.up.railway.app":  false,
		"https://user:pw@gateway.example":                false,
		"https://gateway.example/path":                   false,
		"https://gateway.example?x=1":                    false,
		"ftp://gateway.railway.internal":                 false,
	} {
		if got := config.PrivateServiceOrigin(raw); got != want {
			t.Errorf("%s = %v, want %v", raw, got, want)
		}
	}
}

func TestTrustedProxies(t *testing.T) {
	cfg := config.Config{TrustedProxyCIDRs: "100.64.0.0/10, fd00::/8"}
	got, err := cfg.TrustedProxies()
	if err != nil || len(got) != 2 || got[0].String() != "100.64.0.0/10" || got[1].String() != "fd00::/8" {
		t.Fatal(got, err)
	}
	for _, bad := range []string{"0.0.0.0/0", "::/0", "fc00::/7", "10.0.0.0/7", "not-a-cidr", "10.1.0.0/8"} {
		if _, err := (config.Config{TrustedProxyCIDRs: bad}).TrustedProxies(); err == nil {
			t.Errorf("%s accepted", bad)
		}
	}
}
