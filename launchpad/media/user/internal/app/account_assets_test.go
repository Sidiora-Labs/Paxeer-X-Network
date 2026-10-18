package app

import (
	"errors"
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
