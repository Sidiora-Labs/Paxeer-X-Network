package ws

import (
	"strings"
	"testing"

	"github.com/Sidiora-Technologies/KindleLaunch/core/api/internal/broker"
)

func TestV2ProtocolCredentialAdmission(t *testing.T) {
	for _, protocols := range [][]string{{"sidiora.v2"}, {"sidiora.v2", "ticket." + strings.Repeat("a", 64)}} {
		if _, err := streamProtocols(protocols); err != nil {
			t.Fatal(protocols, err)
		}
	}
	for _, protocols := range [][]string{{}, {"sidiora.v1"}, {"sidiora.v2", "sidiora.v2"}, {"sidiora.v2", "ticket.bad"}, {"sidiora.v2", "Bearer.secret"}, {"sidiora.v2", "ticket." + strings.Repeat("a", 64), "ticket." + strings.Repeat("b", 64)}} {
		if _, err := streamProtocols(protocols); err == nil {
			t.Fatal("invalid protocol accepted", protocols)
		}
	}
}

func TestV2HubDrainRejectsAdmission(t *testing.T) {
	h := NewHub(Options{Lifecycle: broker.NewLifecycle()})
	if !h.Ready() {
		t.Fatal("initially unready")
	}
	h.Drain()
	if h.Ready() {
		t.Fatal("ready during drain")
	}
	if ok, _ := h.acquire("127.0.0.1"); ok {
		t.Fatal("draining upgrade admitted")
	}
}
