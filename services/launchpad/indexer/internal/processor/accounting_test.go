package processor

import (
	"context"
	"github.com/Sidiora-Technologies/KindleLaunch/core/indexer/internal/receipt"
	"github.com/Sidiora-Technologies/KindleLaunch/core/indexer/internal/store"
	"testing"
)

func TestAccountQualifiedRejectsUnissuedReceipt(t *testing.T) {
	for _, q := range []*receipt.QualifiedReceipt{nil, new(receipt.QualifiedReceipt)} {
		if _, err := AccountQualified(context.Background(), q); err == nil {
			t.Fatal("unissued receipt accepted")
		}
	}
}
func TestQualifiedProducerRequiresTransactionalDependencies(t *testing.T) {
	p := New(Deps{ChainID: 125})
	if err := p.ProcessQualifiedCanonicalBatch(context.Background(), store.CanonicalBatch{ChainID: 125}, nil); err == nil {
		t.Fatal("missing canonical transaction collaborators accepted")
	}
}
