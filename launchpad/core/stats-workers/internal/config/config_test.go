package config

import (
	"encoding/base64"
	"strings"
	"testing"

	"github.com/Sidiora-Technologies/KindleLaunch/shared/config/deployment"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nkeys"
)

// setBaseEnv sets every required base + stats env var to a valid value.
func setBaseEnv(t *testing.T) {
	t.Helper()
	roles := deployment.Current().Roles
	vars := map[string]string{
		"DATABASE_URL":            "postgres://kl:kl@localhost:5432/stats",
		"REDIS_URL":               "redis://localhost:6379",
		"REDIS_BULL_URL":          "redis://localhost:6379/1",
		"RPC_URL":                 "http://localhost:8545",
		"EVENT_EMITTER_ADDRESS":   roles["EventEmitter"].Address,
		"POOL_REGISTRY_ADDRESS":   roles["PoolRegistry"].Address,
		"ROUTER_ADDRESS":          roles["Router"].Address,
		"FACTORY_ADDRESS":         roles["SidioraFactory"].Address,
		"QUOTER_ADDRESS":          roles["Quoter"].Address,
		"PROTOCOL_CONFIG_ADDRESS": roles["ProtocolConfig"].Address,
		"FEE_ACCUMULATOR_ADDRESS": roles["FeeAccumulator"].Address,
		"SIDIORA_NFT_ADDRESS":     roles["SidioraNFT"].Address,
		"FEES_ROUTER_ADDRESS":     roles["FeesRouter"].Address,
		"POOL_BEACON_ADDRESS":     roles["PoolBeacon"].Address,
		"WEBHOOK_HMAC_SECRET":     "0123456789abcdef0123456789abcdef",
	}
	for k, v := range vars {
		t.Setenv(k, v)
	}
}

// setIdentityPEM sets every inline identity mTLS value so a production
// profile Load reaches the check under test.
func setIdentityPEM(t *testing.T) {
	t.Helper()
	for _, name := range []string{"IDENTITY_MTLS_CA_PEM", "IDENTITY_MTLS_CLIENT_CERT_PEM", "IDENTITY_MTLS_CLIENT_KEY_PEM"} {
		t.Setenv(name, "inline")
	}
}

func TestLoadInlineIdentityMaterial(t *testing.T) {
	pairs := []struct {
		file, inline string
		value        func(*Config) string
	}{
		{"IDENTITY_MTLS_CA_FILE", "IDENTITY_MTLS_CA_PEM", func(c *Config) string { return c.IdentityClient().CAPEM }},
		{"IDENTITY_MTLS_CLIENT_CERT_FILE", "IDENTITY_MTLS_CLIENT_CERT_PEM", func(c *Config) string { return c.IdentityClient().ClientCertPEM }},
	}
	for _, p := range pairs {
		t.Run(p.inline, func(t *testing.T) {
			setBaseEnv(t)
			setIdentityPEM(t)
			value := "-----BEGIN CERTIFICATE-----\n" + p.inline + "\n-----END CERTIFICATE-----\n"
			t.Setenv(p.inline, value)
			cfg, err := Load()
			if err != nil {
				t.Fatalf("inline %s rejected: %v", p.inline, err)
			}
			if got := p.value(&cfg); got != value {
				t.Fatalf("identity client %s = %q", p.inline, got)
			}
			t.Setenv(p.file, "/etc/identity/material.pem")
			if _, err := Load(); err == nil || !strings.Contains(err.Error(), p.file+" and "+p.inline+" are both set") {
				t.Fatalf("both %s and %s accepted: %v", p.file, p.inline, err)
			}
			t.Setenv(p.file, "")
			t.Setenv(p.inline, "")
			t.Setenv("NODE_ENV", "production")
			t.Setenv("RUNTIME_PROFILE", "container")
			if _, err := Load(); err == nil || !strings.Contains(err.Error(), p.file+" or "+p.inline+" is required") {
				t.Fatalf("missing %s accepted in a production profile: %v", p.inline, err)
			}
		})
	}
}

// inlineNATSCreds returns a user JWT signed by a fresh account nkey and the
// .creds content carrying it with the user seed.
func inlineNATSCreds(t *testing.T) (string, string) {
	t.Helper()
	account, err := nkeys.CreateAccount()
	if err != nil {
		t.Fatal(err)
	}
	user, err := nkeys.CreateUser()
	if err != nil {
		t.Fatal(err)
	}
	accountPub, _ := account.PublicKey()
	userPub, _ := user.PublicKey()
	enc := base64.RawURLEncoding.EncodeToString
	signing := enc([]byte(`{"typ":"JWT","alg":"ed25519-nkey"}`)) + "." + enc([]byte(`{"sub":"`+userPub+`","iss":"`+accountPub+`","nats":{"type":"user","version":2}}`))
	signature, err := account.Sign([]byte(signing))
	if err != nil {
		t.Fatal(err)
	}
	userJWT := signing + "." + enc(signature)
	seed, err := user.Seed()
	if err != nil {
		t.Fatal(err)
	}
	return userJWT, "-----BEGIN NATS USER JWT-----\n" + userJWT + "\n------END NATS USER JWT------\n\n-----BEGIN USER NKEY SEED-----\n" + string(seed) + "\n------END USER NKEY SEED------\n"
}

func TestInlineNATSCredentials(t *testing.T) {
	userJWT, creds := inlineNATSCreds(t)
	setBaseEnv(t)
	setIdentityPEM(t)
	t.Setenv("NATS_CREDENTIALS_CREDS", creds)
	cfg, err := Load()
	if err != nil {
		t.Fatalf("inline NATS_CREDENTIALS_CREDS rejected: %v", err)
	}
	options, err := cfg.RuntimeNATSOptions()
	if err != nil {
		t.Fatalf("inline NATS_CREDENTIALS_CREDS options: %v", err)
	}
	var applied nats.Options
	for _, option := range options {
		if err := option(&applied); err != nil {
			t.Fatal(err)
		}
	}
	if applied.UserJWT == nil || applied.SignatureCB == nil {
		t.Fatal("inline NATS_CREDENTIALS_CREDS set no user JWT handler")
	}
	if got, err := applied.UserJWT(); err != nil || got != userJWT {
		t.Fatalf("user JWT = %q, %v", got, err)
	}
	if _, err := applied.SignatureCB([]byte("nonce")); err != nil {
		t.Fatalf("inline seed cannot sign: %v", err)
	}
	t.Setenv("NATS_CREDENTIALS", "/etc/nats/user.creds")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "NATS_CREDENTIALS and NATS_CREDENTIALS_CREDS are both set") {
		t.Fatalf("both NATS credential sources accepted: %v", err)
	}
	t.Setenv("NATS_CREDENTIALS", "")
	t.Setenv("NATS_CREDENTIALS_CREDS", "")
	t.Setenv("NODE_ENV", "production")
	t.Setenv("RUNTIME_PROFILE", "container")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "NATS_CREDENTIALS or NATS_CREDENTIALS_CREDS is required") {
		t.Fatalf("missing NATS credentials accepted in a production profile: %v", err)
	}
}
