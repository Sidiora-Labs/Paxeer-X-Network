package config

import (
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"strings"
	"testing"
)

func TestInlineBoundDocuments(t *testing.T) {
	doc := []byte(`{"identityPoolId":"identity-pool"}`)
	inline := base64.StdEncoding.EncodeToString(doc)
	digest := fmt.Sprintf("%x", sha256.Sum256(doc))
	wrong := strings.Repeat("ab", 32)

	setValid(t)
	t.Setenv("GATEWAY_RUNTIME_FILE", "")
	t.Setenv("GATEWAY_RUNTIME_DOCUMENT", inline)
	t.Setenv("GATEWAY_RUNTIME_SHA256", digest)
	cfg, err := Load()
	if err != nil {
		t.Fatalf("inline runtime document rejected by Load: %v", err)
	}
	var split RuntimePoolSplit
	if err = ReadBoundDocument(cfg.GatewayRuntimeFile, cfg.GatewayRuntimeSHA256, &split); err != nil || split.IdentityPoolID != "identity-pool" {
		t.Fatalf("correct digest: err=%v split=%+v", err, split)
	}
	if _, err = boundBytes("", inline, digest); err != nil {
		t.Fatalf("correct digest rejected: %v", err)
	}

	t.Setenv("GATEWAY_RUNTIME_SHA256", wrong)
	if cfg, err = Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if err = ReadBoundDocument(cfg.GatewayRuntimeFile, cfg.GatewayRuntimeSHA256, &split); err == nil || err.Error() != "runtime_document_binding_mismatch" {
		t.Fatalf("wrong digest accepted: %v", err)
	}
	if _, err = boundBytes("", inline, wrong); err == nil {
		t.Fatal("wrong digest accepted by boundBytes")
	}

	t.Setenv("GATEWAY_RUNTIME_DOCUMENT", "")
	if _, err = Load(); err == nil || !strings.Contains(err.Error(), "gateway_runtime_binding_required") {
		t.Fatalf("neither source set: %v", err)
	}

	t.Setenv("GATEWAY_RUNTIME_FILE", "/app/prod/gateway-runtime.json")
	t.Setenv("GATEWAY_RUNTIME_DOCUMENT", inline)
	if _, err = Load(); err == nil || !strings.Contains(err.Error(), "runtime_document_source_ambiguous") || !strings.Contains(err.Error(), "GATEWAY_RUNTIME_FILE and GATEWAY_RUNTIME_DOCUMENT are both set") {
		t.Fatalf("both sources set: %v", err)
	}
}
