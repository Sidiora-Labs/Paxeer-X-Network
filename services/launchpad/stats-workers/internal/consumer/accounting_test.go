package consumer

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/Sidiora-Technologies/KindleLaunch/shared/accounting"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/delivery"
)

func TestAccountingPublicationRejectsInnerPoisonBeforeDatabase(t *testing.T) {
	for _, raw := range []string{`{}`, `{"version":"accounting-publication-v1","version":"accounting-publication-v1"}`, `{"unexpected":true}`} {
		_, err := AccountingPublication(delivery.Envelope{Payload: json.RawMessage(raw)}, delivery.PartitionLease{}, "stats", "generation", "projection")
		var typed *delivery.IntegrityFailure
		if !errors.As(err, &typed) || typed.Class != delivery.InnerSchema {
			t.Fatalf("expected inner poison classification: %v", err)
		}
	}
}

func TestAccountingConsumerRequiresRealDependencies(t *testing.T) {
	if _, err := NewAccountingConsumer(nil, nil, nil, nil, "stats", "generation", "projection"); err == nil {
		t.Fatal("missing durable dependencies accepted")
	}
	if _, err := accounting.DecodePublicationV1([]byte(`{"version":"accounting-publication-v1"}`)); err == nil {
		t.Fatal("incomplete wire accepted")
	}
}
