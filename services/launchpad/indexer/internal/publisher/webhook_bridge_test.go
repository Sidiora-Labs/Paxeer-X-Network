package publisher

import (
	"testing"
)

func TestWebhookStagerRejectsMissingRealProviders(t *testing.T) {
	if _, err := NewDurableWebhookStager(nil, nil, "generation"); err == nil {
		t.Fatal("missing real durable providers accepted")
	}
	if _, err := NewDurableStagerWithWebhook(nil, "deployment", map[string]string{"Swap": "kindle.chain125.swap"}, nil, "generation"); err == nil {
		t.Fatal("missing real webhook provider accepted")
	}
}
