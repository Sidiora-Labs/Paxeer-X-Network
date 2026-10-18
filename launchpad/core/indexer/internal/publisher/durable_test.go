package publisher

import (
	"encoding/json"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/delivery"
	"strings"
	"testing"
	"time"
)

func TestDurableEnvelopeDeploymentIdentityAndRawAmount(t *testing.T) {
	// Pure decoder adaptation fixture; no chain execution is asserted.
	s := &DurableStager{deployment: "deployment-a", subjects: map[string]string{"Swap": "kindle.chain125.swap"}}
	input := RawCanonicalEvent{EventType: "Swap", TxHash: "0x" + strings.Repeat("a", 64), TraceID: "trace", SourceOrder: delivery.SourceOrder{Height: "9", TransactionIndex: "1", LogIndex: "2"}, EmittedAt: time.Date(2026, 10, 9, 0, 0, 0, 0, time.UTC), Payload: json.RawMessage(`{"amountRaw":"9007199254740993123456789"}`)}
	subject, e, err := s.Envelope(input)
	if err != nil {
		t.Fatal(err)
	}
	if subject != "kindle.chain125.swap" || e.ChainID != 125 || string(e.Payload) != string(input.Payload) {
		t.Fatal("adapter changed identity/payload")
	}
	input.Payload[0] = 'x'
	if string(e.Payload) == string(input.Payload) {
		t.Fatal("payload aliases caller buffer")
	}
	input.Payload = e.Payload
	_, again, err := s.Envelope(input)
	if err != nil || e.EventID != again.EventID {
		t.Fatal("unstable event identity", err)
	}
	s.deployment = "deployment-b"
	_, other, err := s.Envelope(input)
	if err != nil || other.EventID == e.EventID {
		t.Fatal("deployment identity collapsed", err)
	}
	input.TxHash = "0xabc"
	if _, _, err = s.Envelope(input); err == nil {
		t.Fatal("noncanonical tx hash accepted")
	}
}
