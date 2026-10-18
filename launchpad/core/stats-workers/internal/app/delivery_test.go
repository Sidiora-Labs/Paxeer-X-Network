package app

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"github.com/Sidiora-Technologies/KindleLaunch/core/stats-workers/internal/config"
)

// readDelivery loads a DELIVERY_CONFIG document exactly as New does (bound by
// sha256, strict decode).
func readDelivery(t *testing.T, path string) statsRuntimeDocument {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(raw)
	var d statsRuntimeDocument
	if err = config.ReadBoundDocument(path, hex.EncodeToString(sum[:]), &d); err != nil {
		t.Fatal(err)
	}
	return d
}

// The production document rendered from the fleet NATS manifest (streams.json)
// passes the boot validation, and its stream names are internally coherent:
// the market and creation durables bind the indexer's stream, and the one
// output publisher's stream is the stream the invalidation durable reads.
func TestProductionDeliveryDocument(t *testing.T) {
	d := readDelivery(t, filepath.Join("testdata", "delivery.production.json"))
	if err := d.validate(); err != nil {
		t.Fatalf("rendered production delivery document rejected: %v", err)
	}
	var creation, invalidation string
	for _, s := range d.Phase2.SearchConsumers {
		switch s.Kind {
		case "creation":
			creation = s.Consumer.Stream
		case "invalidation":
			invalidation = s.Consumer.Stream
		}
	}
	if creation != d.Consumers[0].Stream {
		t.Fatalf("creation durable stream %q != market durable stream %q (both are indexer subjects)", creation, d.Consumers[0].Stream)
	}
	if invalidation != d.Phase2.OutputStream {
		t.Fatalf("invalidation durable stream %q != output stream %q", invalidation, d.Phase2.OutputStream)
	}
}

func TestDeliveryDocumentRejectsUnapprovedNames(t *testing.T) {
	for name, mutate := range map[string]func(*statsRuntimeDocument){
		"market subject":        func(d *statsRuntimeDocument) { d.Consumers[0].Subject = "accounting.inventory.v1" },
		"stream name":           func(d *statsRuntimeDocument) { d.Consumers[0].Stream = "RAW CHAIN" },
		"invalidation subject":  func(d *statsRuntimeDocument) { d.Phase2.SearchInvalidationSubject = "stats.search.invalidation.v1" },
		"output stream":         func(d *statsRuntimeDocument) { d.Phase2.OutputStream = "" },
		"missing creation feed": func(d *statsRuntimeDocument) { d.Phase2.SearchConsumers = d.Phase2.SearchConsumers[1:] },
	} {
		d := readDelivery(t, filepath.Join("testdata", "delivery.production.json"))
		mutate(&d)
		if d.validate() == nil {
			t.Fatalf("%s: mutated document accepted", name)
		}
	}
}
