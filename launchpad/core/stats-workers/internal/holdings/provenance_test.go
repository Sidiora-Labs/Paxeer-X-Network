package holdings

import (
	"errors"
	"testing"
)

func TestRecipientProvenance(t *testing.T) {
	e := event(addr(1), addr(9), addr(3), "10", 1)
	p, err := RecipientProvenance(e)
	if err != nil || p.Actor != nil || p.ActorSource != ActorUnknown || p.Recipient != addr(3) || p.RecipientSource != RecipientTransfer || p.TransferIdentity != e.Identity() {
		t.Fatal(p, err)
	}
	enriched, err := p.WithActor(ChainActor{ChainID: e.ChainID, TransactionHash: e.TransactionHash, BlockHash: e.BlockHash, Address: addr(2)})
	if err != nil || enriched.Actor == nil || *enriched.Actor != addr(2) || enriched.ActorSource != ActorChainEvent || enriched.Recipient != addr(3) || p.Actor != nil {
		t.Fatal(enriched, p, err)
	}
	for _, actor := range []ChainActor{
		{ChainID: "1", TransactionHash: e.TransactionHash, BlockHash: e.BlockHash, Address: addr(2)},
		{ChainID: e.ChainID, TransactionHash: event(addr(1), Zero, addr(2), "1", 2).TransactionHash, BlockHash: e.BlockHash, Address: addr(2)},
		{ChainID: e.ChainID, TransactionHash: e.TransactionHash, BlockHash: "0x1234", Address: addr(2)},
	} {
		result, err := p.WithActor(actor)
		if !errors.Is(err, ErrConflict) || result.Actor != nil || result.Recipient != addr(3) {
			t.Fatal(result, err)
		}
	}
	if _, err := p.WithActor(ChainActor{ChainID: e.ChainID, TransactionHash: e.TransactionHash, BlockHash: e.BlockHash, Address: Zero}); err == nil {
		t.Fatal("zero actor")
	}
	burn, err := RecipientProvenance(event(addr(1), addr(2), Zero, "1", 1))
	if err != nil || burn.Recipient != Zero || burn.Actor != nil {
		t.Fatal(burn, err)
	}
}

func TestExplicitDistributionDenominators(t *testing.T) {
	a, err := NewAccumulator("100")
	if err != nil {
		t.Fatal(err)
	}
	rows := []Balance{
		{Address: addr(1), Raw: "75", Category: "pool_reserve"},
		{Address: addr(2), Raw: "20", Category: "external_wallet"},
		{Address: addr(3), Raw: "5", Category: "unknown"},
	}
	for _, b := range rows {
		if err := a.Add(b, b.Address == addr(2)); err != nil {
			t.Fatal(err)
		}
	}
	r, err := a.Finish()
	if err != nil || r.InclusiveDenominator.Raw != "100" || r.InclusiveDenominator.Policy != "indexed_supply_v1" || len(r.InclusiveDenominator.ExcludedCategories) != 0 || r.CirculatingDenominator.Raw != "20" || r.CirculatingDenominator.Policy != "verified_external_wallets_v1" || len(r.CirculatingDenominator.ExcludedCategories) != 5 || r.UnknownCount != 1 || *r.Creator.BasisPoints != "2000" || *r.CirculatingCreator.BasisPoints != "10000" || *r.CirculatingTop10.BasisPoints != "10000" {
		t.Fatal(r, err)
	}
	empty, _ := NewAccumulator("100")
	if err := empty.Add(Balance{Address: addr(1), Raw: "100", Category: "unknown"}, true); err != nil {
		t.Fatal(err)
	}
	r, err = empty.Finish()
	if err != nil || r.CirculatingTop10.BasisPoints != nil || r.CirculatingTop10.Reason != "zero_supply" || r.CirculatingCreator.BasisPoints != nil || r.CirculatingCreator.Reason != "zero_supply" {
		t.Fatal(r, err)
	}
}

func TestMissingOrConflictingProvenanceRejected(t *testing.T) {
	e := event(addr(1), addr(2), addr(3), "7", 1)
	valid, err := RecipientProvenance(e)
	if err != nil {
		t.Fatal(err)
	}
	if err = ValidateProvenance(e, valid); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"chain", "identity", "recipient", "source", "actor_source", "actor"} {
		p := valid
		switch field {
		case "chain":
			p.ChainID = ""
		case "identity":
			p.TransferIdentity = ""
		case "recipient":
			p.Recipient = addr(2)
		case "source":
			p.RecipientSource = ""
		case "actor_source":
			p.ActorSource = ActorChainEvent
		case "actor":
			p.Actor = new(string)
			*p.Actor = Zero
			p.ActorSource = ActorChainEvent
		}
		if !errors.Is(ValidateProvenance(e, p), ErrIntegrity) {
			t.Fatal("invalid provenance accepted", field, p)
		}
	}
}
