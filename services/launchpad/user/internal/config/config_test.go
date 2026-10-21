package config_test

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Sidiora-Technologies/KindleLaunch/media/user/internal/config"
)

// baseEnv returns a complete, valid environment map.
func baseEnv() map[string]string {
	return map[string]string{
		"DATABASE_URL":         "postgres://kl:kl@localhost:5432/user_test?sslmode=disable",
		"REDIS_URL":            "redis://localhost:6379/0",
		"S3_ENDPOINT":          "https://r2.example.com",
		"S3_BUCKET":            "user-assets",
		"S3_ACCESS_KEY_ID":     "ak",
		"S3_SECRET_ACCESS_KEY": "sk",
	}
}

func setEnv(t *testing.T, env map[string]string) {
	t.Helper()
	for k, v := range env {
		t.Setenv(k, v)
	}
}

func TestLoadDefaults(t *testing.T) {
	setEnv(t, baseEnv())
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Port != 3000 {
		t.Errorf("Port = %d, want 3000", cfg.Port)
	}
	if cfg.LogLevel != "info" {
		t.Errorf("LogLevel = %q, want info", cfg.LogLevel)
	}
	if cfg.S3Region != "auto" {
		t.Errorf("S3Region = %q, want auto", cfg.S3Region)
	}
	if cfg.MaxAvatarSizeBytes != 2097152 {
		t.Errorf("MaxAvatarSizeBytes = %d, want 2097152", cfg.MaxAvatarSizeBytes)
	}
	if cfg.MaxBannerSizeBytes != 5242880 {
		t.Errorf("MaxBannerSizeBytes = %d, want 5242880", cfg.MaxBannerSizeBytes)
	}
}

func TestLoadMissingRequired(t *testing.T) {
	// Omit each required S3 var in turn; Load must fail.
	required := []string{"S3_ENDPOINT", "S3_BUCKET", "S3_ACCESS_KEY_ID", "S3_SECRET_ACCESS_KEY"}
	for _, miss := range required {
		t.Run("missing_"+miss, func(t *testing.T) {
			env := baseEnv()
			delete(env, miss)
			setEnv(t, env)
			// Ensure the omitted var is truly empty for this subtest.
			t.Setenv(miss, "")
			if _, err := config.Load(); err == nil {
				t.Errorf("expected error when %s missing", miss)
			}
		})
	}
}

func TestValidate(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(c *config.Config)
		wantErr bool
	}{
		{"valid", func(c *config.Config) {}, false},
		{"bad db url", func(c *config.Config) { c.DatabaseURL = "not-a-url" }, true},
		{"empty redis", func(c *config.Config) { c.RedisURL = "  " }, true},
		{"bad s3 endpoint", func(c *config.Config) { c.S3Endpoint = "nope" }, true},
		{"empty bucket", func(c *config.Config) { c.S3Bucket = "" }, true},
		{"empty access key", func(c *config.Config) { c.S3AccessKeyID = "" }, true},
		{"empty secret", func(c *config.Config) { c.S3SecretAccessKey = "" }, true},
		{"bad log level", func(c *config.Config) { c.LogLevel = "trace" }, true},
		{"bad port low", func(c *config.Config) { c.Port = 0 }, true},
		{"bad port high", func(c *config.Config) { c.Port = 70000 }, true},
		{"bad avatar size", func(c *config.Config) { c.MaxAvatarSizeBytes = 0 }, true},
		{"bad banner size", func(c *config.Config) { c.MaxBannerSizeBytes = -1 }, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cfg := config.Config{
				DatabaseURL:        "postgres://localhost:5432/db",
				RedisURL:           "redis://localhost:6379",
				S3Endpoint:         "https://r2.example.com",
				S3Bucket:           "b",
				S3AccessKeyID:      "ak",
				S3SecretAccessKey:  "sk",
				S3Region:           "auto",
				LogLevel:           "info",
				Port:               3000,
				MaxAvatarSizeBytes: 2097152,
				MaxBannerSizeBytes: 5242880,
			}
			c.mutate(&cfg)
			err := cfg.Validate()
			if (err != nil) != c.wantErr {
				t.Errorf("Validate() err = %v, wantErr %v", err, c.wantErr)
			}
		})
	}
}

func accountConfig() config.Config {
	key := base64.StdEncoding.EncodeToString(make([]byte, 32))
	pin := strings.Repeat("ab", 32)
	return config.Config{
		AccountCursorKey: key, AccountExportKey: key, AccountEndpointKey: key, AccountEndpointFingerprintKey: key, AccountAuditKey: key, AccountEndpointProofPublicKey: key, AccountGatewayPublicKey: key, AccountServiceSigningSeed: key,
		AccountGatewayCertSHA256: pin, NotificationCertSHA256: pin, IdentityCertSHA256: pin, MetadataCertSHA256: pin,
		AccountDownloadBaseURL: "https://download.example", NotificationServiceURL: "https://notification.example", IdentityURL: "https://identity.example", MetadataServiceURL: "https://metadata.example", S3Endpoint: "https://r2.example.com",
		S3Bucket: "user-assets", AccountExportBucket: "account-exports", NotificationServiceToken: strings.Repeat("n", 32), NotificationInboundToken: strings.Repeat("i", 32),
		AccountNATSStream: "ACCOUNT", AccountSubjectsDocument: "e30=", AccountSubjectsSHA256: pin, AccountEnvironment: "production", AccountNATSURL: "tls://nats.example:4222", AccountRPCURL: "https://rpc.example",
		AccountTLSCertFile: "/etc/sidiora/pki/account.pem", AccountTLSKeyFile: "/etc/sidiora/pki/account.key", AccountTLSCAFile: "/etc/sidiora/pki/ca.pem",
		AccountNATSCredentialsFile: "/etc/sidiora/nats/account.creds", AccountNATSCAFile: "/etc/sidiora/nats/ca.pem", AccountNATSCertFile: "/etc/sidiora/nats/account.pem", AccountNATSKeyFile: "/etc/sidiora/nats/account.key",
	}
}

func TestAccountRuntimeInlineMaterial(t *testing.T) {
	if err := accountConfig().ValidateAccountRuntime(); err != nil {
		t.Fatalf("file form refused: %v", err)
	}
	for name, set := range map[string]func(*config.Config, string){
		"ACCOUNT_TLS_CERT_PEM":  func(c *config.Config, v string) { c.AccountTLSCertPEM = v },
		"ACCOUNT_TLS_KEY_PEM":   func(c *config.Config, v string) { c.AccountTLSKeyPEM = v },
		"ACCOUNT_TLS_CA_PEM":    func(c *config.Config, v string) { c.AccountTLSCAPEM = v },
		"ACCOUNT_NATS_CREDS":    func(c *config.Config, v string) { c.AccountNATSCreds = v },
		"ACCOUNT_NATS_CA_PEM":   func(c *config.Config, v string) { c.AccountNATSCAPEM = v },
		"ACCOUNT_NATS_CERT_PEM": func(c *config.Config, v string) { c.AccountNATSCertPEM = v },
		"ACCOUNT_NATS_KEY_PEM":  func(c *config.Config, v string) { c.AccountNATSKeyPEM = v },
	} {
		fileVar := strings.TrimSuffix(strings.TrimSuffix(name, "_PEM"), "_CREDS") + "_FILE"
		if name == "ACCOUNT_NATS_CREDS" {
			fileVar = "ACCOUNT_NATS_CREDENTIALS_FILE"
		}
		both := accountConfig()
		set(&both, "inline")
		err := both.ValidateAccountRuntime()
		if err == nil || !strings.Contains(err.Error(), fileVar) || !strings.Contains(err.Error(), name) {
			t.Fatalf("%s with its file accepted or unnamed: %v", name, err)
		}
		inline := accountConfig()
		set(&inline, "inline")
		clearFile(&inline, fileVar)
		if err := inline.ValidateAccountRuntime(); err != nil {
			t.Fatalf("%s alone refused: %v", name, err)
		}
		neither := accountConfig()
		clearFile(&neither, fileVar)
		if err := neither.ValidateAccountRuntime(); err == nil || !strings.Contains(err.Error(), name) {
			t.Fatalf("neither %s nor %s rejected: %v", fileVar, name, err)
		}
	}
}

func clearFile(c *config.Config, fileVar string) {
	for name, field := range map[string]*string{"ACCOUNT_TLS_CERT_FILE": &c.AccountTLSCertFile, "ACCOUNT_TLS_KEY_FILE": &c.AccountTLSKeyFile, "ACCOUNT_TLS_CA_FILE": &c.AccountTLSCAFile, "ACCOUNT_NATS_CREDENTIALS_FILE": &c.AccountNATSCredentialsFile, "ACCOUNT_NATS_CA_FILE": &c.AccountNATSCAFile, "ACCOUNT_NATS_CERT_FILE": &c.AccountNATSCertFile, "ACCOUNT_NATS_KEY_FILE": &c.AccountNATSKeyFile} {
		if name == fileVar {
			*field = ""
		}
	}
}

func TestMaterialInlineWins(t *testing.T) {
	path := filepath.Join(t.TempDir(), "material.pem")
	if err := os.WriteFile(path, []byte("from-file"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got, err := config.Material(path, "inline"); err != nil || string(got) != "inline" {
		t.Fatalf("inline: %q %v", got, err)
	}
	if got, err := config.Material(path, ""); err != nil || string(got) != "from-file" {
		t.Fatalf("file: %q %v", got, err)
	}
}

func TestValidateNamesBothDocumentForms(t *testing.T) {
	c := config.Config{DatabaseURL: "postgres://localhost:5432/db", RedisURL: "redis://localhost:6379", S3Endpoint: "https://r2.example.com", S3Bucket: "b", S3AccessKeyID: "ak", S3SecretAccessKey: "sk", LogLevel: "info", Port: 3000, MaxAvatarSizeBytes: 1, MaxBannerSizeBytes: 1,
		RuntimeBudgetFile: "/etc/sidiora/budget.json", RuntimeBudgetDocument: "e30=", AccountSubjectsFile: "/etc/sidiora/subjects.json", AccountSubjectsDocument: "e30="}
	err := c.Validate()
	if err == nil || !strings.Contains(err.Error(), "DB_BUDGET_FILE and DB_BUDGET_DOCUMENT are both set") || !strings.Contains(err.Error(), "ACCOUNT_SUBJECTS_FILE and ACCOUNT_SUBJECTS_DOCUMENT are both set") {
		t.Fatalf("both forms not named: %v", err)
	}
}
