package deployment

import (
	"context"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

// TestFinalizedGovernanceActivatesShippedManifest attests the shipped manifest
// (status active) against real contract bytecode whose PoolBeacon owner is the
// Timelock and whose pendingOwner is zero: the post-handoff chain state.
func TestFinalizedGovernanceActivatesShippedManifest(t *testing.T) {
	shipped := Current()
	if shipped.Status != "active" {
		t.Fatalf("shipped manifest status %q, want active", shipped.Status)
	}
	timelock := common.HexToAddress("0xE3e71911664062753b56F72965D906295625d6c0")
	if common.HexToAddress(shipped.Governance.ExpectedBeaconOwner) != timelock {
		t.Fatalf("expected beacon owner %s, want Timelock", shipped.Governance.ExpectedBeaconOwner)
	}
	m, r, b, p := newEVM(t, func(m *Manifest, a types.GenesisAlloc) {
		m.Status = shipped.Status
		addr := common.HexToAddress(m.Roles["PoolBeacon"].Address)
		acc := a[addr]
		acc.Storage[slot(1)] = addrWord(timelock.Hex())
		acc.Storage[slot(2)] = common.Hash{}
		a[addr] = acc
	}, 125)
	a, e := Attest(context.Background(), r, m, b, p)
	if e != nil {
		t.Fatal(e)
	}
	if common.HexToAddress(a.ActualOwner) != timelock || a.PendingOwner != (common.Address{}).Hex() {
		t.Fatalf("owner %s pending %s", a.ActualOwner, a.PendingOwner)
	}
	if !a.Eligible || hasIssue(a, "manifest_governance_pending", "") || hasIssue(a, "governance_handoff_pending", "") {
		t.Fatalf("finalized deployment not eligible: %+v", a.Issues)
	}
	if e = a.ValidateFor(m, p); e != nil {
		t.Fatal(e)
	}
	if e = a.ValidateFamilyFor(m, p, []string{"Router", "Quoter", "SidioraFactory"}, true); e != nil {
		t.Fatal(e)
	}
	// The same chain state with a pending owner set must still block.
	a.PendingOwner = timelock.Hex()
	if e = a.ValidateFor(m, p); e == nil || !strings.Contains(e.Error(), "governance_handoff_pending") {
		t.Fatalf("pending owner accepted: %v", e)
	}
}
