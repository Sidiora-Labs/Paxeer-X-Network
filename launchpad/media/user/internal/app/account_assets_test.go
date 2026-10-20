package app

import (
	"crypto/tls"
	"errors"
	"github.com/Sidiora-Technologies/KindleLaunch/media/user/internal/config"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/testpki"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nkeys"
	"os"
	"strings"
	"testing"

	"github.com/Sidiora-Technologies/KindleLaunch/media/user/internal/account"
)

// The bodies below are byte-exact media/metadata answers of
// GET /internal/v2/accounts/{wallet}/assets/{assetId} (json.Encoder of
// WireAsset, field order and trailing newline included).
const (
	assetOwner       = "0x1dae78b53c9f39930a2ff2b6b7aac12ec75fb154"
	assetID          = "6f1c2a0e-8a4b-4f43-9a51-5f3c1d7e9b20"
	approvedAsset    = `{"assetId":"6f1c2a0e-8a4b-4f43-9a51-5f3c1d7e9b20","ownerWallet":"0x1dae78b53c9f39930a2ff2b6b7aac12ec75fb154","sha256":"0x9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08","mediaType":"image/png","bytes":20480,"state":"approved","publicUrl":"https://metadata-production-3367.up.railway.app/approved/account/0x1dae78b53c9f39930a2ff2b6b7aac12ec75fb154/6f1c2a0e-8a4b-4f43-9a51-5f3c1d7e9b20/512/9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08.png"}` + "\n"
	quarantinedAsset = `{"assetId":"6f1c2a0e-8a4b-4f43-9a51-5f3c1d7e9b20","ownerWallet":"0x1dae78b53c9f39930a2ff2b6b7aac12ec75fb154","sha256":"0x9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08","mediaType":"image/png","bytes":20480,"state":"quarantined","publicUrl":null}` + "\n"
)

func TestMetadataAccountAssetAuthorityAnswers(t *testing.T) {
	decode := func(t *testing.T, raw string) (metadataAccountAsset, error) {
		t.Helper()
		var out metadataAccountAsset
		err := decodeMetadataAccountAsset([]byte(raw), &out)
		return out, err
	}
	t.Run("approved asset admitted", func(t *testing.T) {
		out, err := decode(t, approvedAsset)
		if err != nil {
			t.Fatal(err)
		}
		if err = approvedAccountAsset(out, assetOwner, assetID); err != nil {
			t.Fatalf("approved owned asset = %v", err)
		}
	})
	t.Run("pending asset with null publicUrl is denied, not FIELD_INVALID", func(t *testing.T) {
		out, err := decode(t, quarantinedAsset)
		if err != nil {
			t.Fatalf("null publicUrl of a pending asset must decode: %v", err)
		}
		if err = approvedAccountAsset(out, assetOwner, assetID); !errors.Is(err, account.ErrDenied) {
			t.Fatalf("pending asset = %v, want ErrDenied", err)
		}
		status, wire := account.PublicError(err, [16]byte{1})
		if status != 403 || wire.Code != "SCOPE_DENIED" {
			t.Fatalf("pending asset wire = %d %s, want 403 SCOPE_DENIED", status, wire.Code)
		}
		// The client-body decoder this authority used before reports the
		// pending asset as the caller's invalid field.
		var legacy metadataAccountAsset
		if e := account.Decode(strings.NewReader(quarantinedAsset), &legacy); e == nil {
			t.Fatal("account.Decode accepted a null publicUrl; the dedicated decoder would be unnecessary")
		}
	})
	t.Run("asset of another wallet denied", func(t *testing.T) {
		out, err := decode(t, approvedAsset)
		if err != nil {
			t.Fatal(err)
		}
		if err = approvedAccountAsset(out, "0x0f92fdd087c96dd3413ef98646b94d0d986192fa", assetID); !errors.Is(err, account.ErrDenied) {
			t.Fatalf("foreign asset = %v", err)
		}
	})
	t.Run("malformed authority answers are a dependency failure", func(t *testing.T) {
		for name, raw := range map[string]string{
			"unknown field":  `{"assetId":"x","extra":1}`,
			"duplicate key":  `{"assetId":"a","assetId":"b"}`,
			"trailing value": approvedAsset + `{}`,
			"not json":       `<html>`,
		} {
			if _, err := decode(t, raw); err == nil || err.Error() != "DEPENDENCY_UNAVAILABLE" {
				t.Fatalf("%s = %v, want DEPENDENCY_UNAVAILABLE", name, err)
			}
		}
	})
}

func TestAccountTLSAndNATSFromInlineMaterial(t *testing.T) {
	pki := testpki.New(t)
	leaf := pki.Leaf("account", "user.railway.internal")
	caPEM, err := os.ReadFile(pki.CAFile)
	if err != nil {
		t.Fatal(err)
	}
	user, err := nkeys.CreateUser()
	if err != nil {
		t.Fatal(err)
	}
	seed, err := user.Seed()
	if err != nil {
		t.Fatal(err)
	}
	jwt := "eyJ0eXAiOiJKV1QiLCJhbGciOiJlZDI1NTE5LW5rZXkifQ.eyJzdWIiOiJ1c2VyIn0.c2lnbmF0dXJl"
	creds := "-----BEGIN NATS USER JWT-----\n" + jwt + "\n------END NATS USER JWT------\n\n-----BEGIN USER NKEY SEED-----\n" + string(seed) + "\n------END USER NKEY SEED------\n"
	cfg := config.Config{AccountTLSCertPEM: string(leaf.CertPEM), AccountTLSKeyPEM: string(leaf.KeyPEM), AccountTLSCAPEM: string(caPEM),
		AccountNATSCreds: creds, AccountNATSCertPEM: string(leaf.CertPEM), AccountNATSKeyPEM: string(leaf.KeyPEM), AccountNATSCAPEM: string(caPEM)}
	accounts, err := accountTLS(cfg)
	if err != nil || len(accounts.Certificates) != 1 || accounts.RootCAs == nil || accounts.ClientCAs == nil || accounts.MinVersion != tls.VersionTLS13 {
		t.Fatalf("inline account TLS: %+v %v", accounts, err)
	}
	options, err := accountNATSOptions(cfg)
	if err != nil {
		t.Fatal(err)
	}
	applied := nats.GetDefaultOptions()
	for _, option := range options {
		if err = option(&applied); err != nil {
			t.Fatal(err)
		}
	}
	if !applied.Secure || applied.TLSConfig == nil || len(applied.TLSConfig.Certificates) != 1 || applied.TLSConfig.RootCAs == nil || applied.UserJWT == nil || applied.SignatureCB == nil {
		t.Fatalf("inline NATS options not applied: %+v", applied)
	}
	if got, err := applied.UserJWT(); err != nil || got != jwt {
		t.Fatalf("inline NATS jwt: %q %v", got, err)
	}
	nonce := []byte("nonce")
	signature, err := applied.SignatureCB(nonce)
	if err != nil || user.Verify(nonce, signature) != nil {
		t.Fatalf("inline NATS seed does not sign as the user: %v", err)
	}
	cfg.AccountNATSCreds = "not a creds file"
	if _, err = accountNATSOptions(cfg); err == nil {
		t.Fatal("malformed inline creds accepted")
	}
	files := config.Config{AccountTLSCertFile: leaf.CertFile, AccountTLSKeyFile: leaf.KeyFile, AccountTLSCAFile: pki.CAFile, AccountNATSCredentialsFile: "/etc/sidiora/nats/account.creds", AccountNATSCertFile: leaf.CertFile, AccountNATSKeyFile: leaf.KeyFile, AccountNATSCAFile: pki.CAFile}
	if accounts, err = accountTLS(files); err != nil || len(accounts.Certificates) != 1 {
		t.Fatalf("file account TLS: %v", err)
	}
	if options, err = accountNATSOptions(files); err != nil || len(options) != 3 {
		t.Fatalf("file NATS options: %d %v", len(options), err)
	}
}
