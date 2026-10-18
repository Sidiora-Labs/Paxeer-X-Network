package publisher

import (
	"encoding/json"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/delivery"
	"strings"
	"testing"
	"time"
)

func TestCanonicalShadowUsesFrozenDurableIdentity(t *testing.T) {
	s := &DurableStager{deployment: "deployment", subjects: map[string]string{"Swap": "kindle.chain125.swap"}}
	event := RawCanonicalEvent{EventType: "Swap", TxHash: "0x" + strings.Repeat("a", 64), TraceID: "trace", SourceOrder: delivery.SourceOrder{Height: "10", TransactionIndex: "1", LogIndex: "2"}, EmittedAt: time.Unix(10, 0).UTC(), Payload: json.RawMessage(`{"amountRaw":"9007199254740993123456789"}`)}
	_, legacy, err := s.Envelope(event)
	if err != nil {
		t.Fatal(err)
	}
	_, durable, err := s.Envelope(event)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = delivery.CompareShadow([]delivery.ShadowRecord{{EventID: legacy.EventID, PayloadHash: legacy.PayloadHash}}, []delivery.ShadowRecord{{EventID: durable.EventID, PayloadHash: durable.PayloadHash}}); err != nil {
		t.Fatal(err)
	}
	event.Payload = json.RawMessage(`{"amountRaw":"9007199254740993123456790"}`)
	_, changed, err := s.Envelope(event)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = delivery.CompareShadow([]delivery.ShadowRecord{{EventID: legacy.EventID, PayloadHash: legacy.PayloadHash}}, []delivery.ShadowRecord{{EventID: changed.EventID, PayloadHash: changed.PayloadHash}}); err == nil {
		t.Fatal("changed source payload accepted")
	}
}
