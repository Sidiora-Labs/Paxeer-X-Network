package attestor_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Sidiora-Labs/Paxeer-X-Network/human/wallet/ceremony/internal/attestor"
)

var goldenDir = filepath.Join("..", "..", "..", "schema", "attestor-api", "golden")

func readGolden(t *testing.T, name string) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(goldenDir, name+".json"))
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func decodeGolden(t *testing.T, name string, v any) {
	t.Helper()
	dec := json.NewDecoder(bytes.NewReader(readGolden(t, name)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	if dec.More() {
		t.Fatalf("%s: trailing data", name)
	}
}

func sameJSON(t *testing.T, name string, v any) {
	t.Helper()
	got, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var a, b any
	if err := json.Unmarshal(got, &a); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(readGolden(t, name), &b); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(a, b) {
		t.Fatalf("%s: client encoding differs from the golden\n got %s", name, got)
	}
}

func TestGoldenVectorsRoundTripThroughClientTypes(t *testing.T) {
	cases := map[string]func() any{
		"keys.import.request":                func() any { return &attestor.ImportRequest{} },
		"keys.refresh.request":               func() any { return &attestor.RefreshRequest{} },
		"keys.response":                      func() any { return &attestor.KeyResponse{} },
		"sign.request":                       func() any { return &attestor.SignRequest{} },
		"sign.request.lx_grant":              func() any { return &attestor.SignRequest{} },
		"sign.request.operator_verification": func() any { return &attestor.SignRequest{} },
		"sign.response":                      func() any { return &attestor.SignResponse{} },
		"error.policy":                       func() any { return &attestor.ErrorBody{} },
		"error.token":                        func() any { return &attestor.ErrorBody{} },
	}
	for name, mk := range cases {
		v := mk()
		decodeGolden(t, name, v)
		sameJSON(t, name, v)
	}
}

func TestGoldenImportShareDecodesAndReencodes(t *testing.T) {
	var req attestor.ImportRequest
	decodeGolden(t, "keys.import.request", &req)
	if req.SessionID == "" || req.KeyID == "" || req.Owner == "" || req.Share.Threshold != 3 || len(req.Share.PartialPublicKeys) != 5 {
		t.Fatalf("import golden fields: %+v", req)
	}
	b, err := attestor.DecodeBundle(req.Share)
	if err != nil {
		t.Fatal(err)
	}
	if err := b.Validate(); err != nil {
		t.Fatalf("golden share does not validate against its partial keys: %v", err)
	}
	back, err := attestor.EncodeBundle(b)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(back, req.Share) {
		t.Fatalf("share re-encodes as %+v", back)
	}
	pub, err := attestor.PublicKeyHex(b.PublicKey)
	if err != nil || len(pub) != 130 || !strings.HasPrefix(pub, "04") || !strings.HasSuffix(pub, req.Share.PublicKey.Y) {
		t.Fatalf("public key hex %s: %v", pub, err)
	}
}

func TestGoldenResponsesMatchClientExpectations(t *testing.T) {
	var key attestor.KeyResponse
	decodeGolden(t, "keys.response", &key)
	if key.AuditSequence != 3 || !key.Refreshed || key.Epoch != 1 || len(key.PublicKey) != 130 {
		t.Fatalf("key response %+v", key)
	}
	var sig attestor.SignResponse
	decodeGolden(t, "sign.response", &sig)
	if sig.RecoveryID == nil || *sig.RecoveryID != 1 || len(sig.Signature) != 130 || !strings.HasSuffix(sig.Signature, "01") || sig.AuditSequence != 9 {
		t.Fatalf("sign response %+v", sig)
	}
	var sign attestor.SignRequest
	decodeGolden(t, "sign.request", &sign)
	if len(sign.Signers) != attestor.SignQuorum || sign.Transaction == "" {
		t.Fatalf("sign request %+v", sign)
	}
	var verify attestor.SignRequest
	decodeGolden(t, "sign.request.operator_verification", &verify)
	if verify.Kind != attestor.KindVerify || verify.ImportSessionID == "" || len(verify.Signers) != attestor.SignQuorum || verify.Message != "" || verify.Transaction != "" || verify.Digest != "" {
		t.Fatalf("verification request %+v", verify)
	}
	var e attestor.ErrorBody
	decodeGolden(t, "error.policy", &e)
	if e.Error.Category != "policy" || e.Error.Code != "policy_denied" || e.Error.PolicyCode != "value_cap" {
		t.Fatalf("policy error %+v", e)
	}
	if attestor.PathImport != "/v1/keys/import" || attestor.PathRefresh != "/v1/keys/refresh" || attestor.PathSign != "/v1/sign" {
		t.Fatal("client paths differ from the schema")
	}
}
