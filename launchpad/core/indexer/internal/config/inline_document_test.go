package config

import (
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"strings"
	"testing"
)

func digestOf(raw string) string { return fmt.Sprintf("%x", sha256.Sum256([]byte(raw))) }

func TestInlineBoundDocuments(t *testing.T) {
	receipt := `{"name":"receipt"}`
	delivery := `{"name":"delivery"}`
	lifecycle := `{"name":"lifecycle"}`
	enc := base64.StdEncoding.EncodeToString
	cfg := Config{
		ReceiptConfigDocument: enc([]byte(receipt)), ReceiptConfigSHA256: digestOf(receipt),
		DeliveryConfigDocument: enc([]byte(delivery)), DeliveryConfigSHA256: digestOf(delivery),
		LifecycleConfigDocument: enc([]byte(lifecycle)), LifecycleConfigSHA256: digestOf(lifecycle),
	}
	if err := cfg.registerInlineDocuments(); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct{ file, digest, want string }{
		{cfg.ReceiptConfigFile, cfg.ReceiptConfigSHA256, "receipt"},
		{cfg.DeliveryConfigFile, cfg.DeliveryConfigSHA256, "delivery"},
		{cfg.LifecycleConfigFile, cfg.LifecycleConfigSHA256, "lifecycle"},
	} {
		var out struct {
			Name string `json:"name"`
		}
		if err := ReadBoundDocument(c.file, c.digest, &out); err != nil || out.Name != c.want {
			t.Fatalf("inline %s: got %q, %v", c.want, out.Name, err)
		}
	}

	wrong := Config{DeliveryConfigDocument: enc([]byte(delivery)), DeliveryConfigSHA256: digestOf(receipt + " ")}
	if err := wrong.registerInlineDocuments(); err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := ReadBoundDocument(wrong.DeliveryConfigFile, wrong.DeliveryConfigSHA256, &out); err == nil || err.Error() != "runtime_document_binding_mismatch" {
		t.Fatalf("wrong digest accepted: %v", err)
	}
	if err := ReadBoundDocument("", digestOf(receipt), &out); err == nil || err.Error() != "runtime_document_binding_missing" {
		t.Fatalf("unset document accepted: %v", err)
	}

	budget := `{"budget":1}`
	if raw, err := boundBytes("", enc([]byte(budget)), digestOf(budget)); err != nil || string(raw) != budget {
		t.Fatalf("inline budget: %q, %v", raw, err)
	}
	if _, err := boundBytes("", enc([]byte(budget)), digestOf(budget+"x")); err == nil || err.Error() != "runtime_document_binding_mismatch" {
		t.Fatalf("budget wrong digest accepted: %v", err)
	}
	if _, err := boundBytes("", "not base64!", digestOf(budget)); err == nil || err.Error() != "runtime_document_invalid" {
		t.Fatalf("non-base64 accepted: %v", err)
	}
	if _, err := boundBytes("", enc(make([]byte, 1048577)), digestOf(strings.Repeat("\x00", 1048577))); err == nil {
		t.Fatal("oversize document accepted")
	}
	if _, err := boundBytes("/nonexistent", enc([]byte(budget)), digestOf(budget)); err == nil || err.Error() != "runtime_document_binding_missing" {
		t.Fatalf("file and document both accepted: %v", err)
	}

	for _, name := range []string{"DATABASE_URL", "REDIS_URL", "REDIS_BULL_URL", "RPC_URL", "EVENT_EMITTER_ADDRESS", "POOL_REGISTRY_ADDRESS", "ROUTER_ADDRESS", "FACTORY_ADDRESS", "QUOTER_ADDRESS", "PROTOCOL_CONFIG_ADDRESS", "FEE_ACCUMULATOR_ADDRESS", "SIDIORA_NFT_ADDRESS", "FEES_ROUTER_ADDRESS", "POOL_BEACON_ADDRESS"} {
		t.Setenv(name, "x")
	}
	t.Setenv("DELIVERY_CONFIG_FILE", "/etc/delivery.json")
	t.Setenv("DELIVERY_CONFIG_DOCUMENT", enc([]byte(delivery)))
	t.Setenv("DELIVERY_CONFIG_SHA256", digestOf(delivery))
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "DELIVERY_CONFIG_FILE and DELIVERY_CONFIG_DOCUMENT are both set") {
		t.Fatalf("both sources accepted: %v", err)
	}

	prod := Config{LifecycleConfigDocument: enc([]byte(lifecycle)), LifecycleConfigSHA256: digestOf(lifecycle)}
	prod.NodeEnv = "production"
	if err := prod.Validate(); err != nil && strings.Contains(err.Error(), "lifecycle document binding required") {
		t.Fatalf("inline lifecycle rejected: %v", err)
	}
}
