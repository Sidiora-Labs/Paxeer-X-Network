package tasks

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	codectypes "github.com/sei-protocol/sei-chain/sei-cosmos/codec/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/crypto/keys/secp256k1"
	cryptotypes "github.com/sei-protocol/sei-chain/sei-cosmos/crypto/types"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	stakingtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/staking/types"
)

// randomSeiAddr returns a fresh bech32 "sei" account address.
func randomSeiAddr(t *testing.T) string {
	t.Helper()
	ensureBech32()
	return sdk.AccAddress(secp256k1.GenPrivKey().PubKey().Address()).String()
}

// writeGentxFixture writes a minimal-but-decodable MsgCreateValidator gentx for
// the given delegator into dir/filename with a fresh, unique consensus pubkey.
func writeGentxFixture(t *testing.T, dir, filename, delegator string) {
	t.Helper()
	writeGentxFixtureWithPubKey(t, dir, filename, delegator, secp256k1.GenPrivKey().PubKey())
}

// writeGentxFixtureWithPubKey is writeGentxFixture with an explicit consensus
// pubkey, so a test can force two gentxs to share one — the copied-validator-key
// case the dedup guard must reject. Signatures are irrelevant to the invariant
// (it inspects only delegator, operator address, and pubkey), so the tx is left
// unsigned.
func writeGentxFixtureWithPubKey(t *testing.T, dir, filename, delegator string, pk cryptotypes.PubKey) {
	t.Helper()
	ensureBech32()
	_, txCfg := makeCodec()

	addr, err := sdk.AccAddressFromBech32(delegator)
	if err != nil {
		t.Fatalf("parsing delegator %q: %v", delegator, err)
	}

	pkAny, err := codectypes.NewAnyWithValue(pk)
	if err != nil {
		t.Fatalf("packing pubkey: %v", err)
	}

	msg := &stakingtypes.MsgCreateValidator{
		Description:       stakingtypes.NewDescription("moniker", "", "", "", ""),
		Commission:        stakingtypes.NewCommissionRates(sdk.ZeroDec(), sdk.ZeroDec(), sdk.ZeroDec()),
		MinSelfDelegation: sdk.OneInt(),
		DelegatorAddress:  addr.String(),
		ValidatorAddress:  sdk.ValAddress(addr).String(),
		Pubkey:            pkAny,
		Value:             sdk.NewCoin("usei", sdk.NewInt(1)),
	}

	txb := txCfg.NewTxBuilder()
	if err := txb.SetMsgs(msg); err != nil {
		t.Fatalf("set msgs: %v", err)
	}
	bz, err := txCfg.TxJSONEncoder()(txb.GetTx())
	if err != nil {
		t.Fatalf("encoding gentx: %v", err)
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, filename), bz, 0o644); err != nil {
		t.Fatalf("write gentx: %v", err)
	}
}

// TestVerifyAssembledGentxs_DuplicateDelegator: two gentxs for the same
// delegator (the failure that panicked InitChain with "account sequence
// mismatch, expected 1, got 0") must be rejected up front rather than producing
// a chain-wedging genesis.
func TestVerifyAssembledGentxs_DuplicateDelegator(t *testing.T) {
	homeDir := t.TempDir()
	a := NewGenesisAssembler(homeDir, "b", "r", "chain", nil, nil)
	dir := a.assembledGentxDir()

	dup := randomSeiAddr(t)
	writeGentxFixture(t, dir, "gentx-val-0.json", dup)
	writeGentxFixture(t, dir, "gentx-val-1.json", dup) // same delegator → duplicate

	err := a.verifyAssembledGentxs([]string{"val-0", "val-1"})
	if err == nil {
		t.Fatal("expected duplicate-delegator error, got nil")
	}
	if !strings.Contains(err.Error(), "duplicate delegator") {
		t.Errorf("error = %q, want substring 'duplicate delegator'", err.Error())
	}
}

// TestVerifyAssembledGentxs_DuplicateConsensusPubKey covers the copied-key case:
// distinct delegators that share one consensus pubkey would abort InitChain in
// x/staking, so the guard must reject it even though the delegators differ.
func TestVerifyAssembledGentxs_DuplicateConsensusPubKey(t *testing.T) {
	homeDir := t.TempDir()
	a := NewGenesisAssembler(homeDir, "b", "r", "chain", nil, nil)
	dir := a.assembledGentxDir()

	shared := secp256k1.GenPrivKey().PubKey()
	writeGentxFixtureWithPubKey(t, dir, "gentx-val-0.json", randomSeiAddr(t), shared)
	writeGentxFixtureWithPubKey(t, dir, "gentx-val-1.json", randomSeiAddr(t), shared)

	err := a.verifyAssembledGentxs([]string{"val-0", "val-1"})
	if err == nil {
		t.Fatal("expected duplicate-consensus-pubkey error, got nil")
	}
	if !strings.Contains(err.Error(), "duplicate consensus pubkey") {
		t.Errorf("error = %q, want substring 'duplicate consensus pubkey'", err.Error())
	}
}

func TestVerifyAssembledGentxs_DistinctOK(t *testing.T) {
	homeDir := t.TempDir()
	a := NewGenesisAssembler(homeDir, "b", "r", "chain", nil, nil)
	dir := a.assembledGentxDir()

	writeGentxFixture(t, dir, "gentx-val-0.json", randomSeiAddr(t))
	writeGentxFixture(t, dir, "gentx-val-1.json", randomSeiAddr(t))

	if err := a.verifyAssembledGentxs([]string{"val-0", "val-1"}); err != nil {
		t.Fatalf("expected distinct gentxs to pass, got %v", err)
	}
}

// TestVerifyAssembledGentxs_MissingGentx covers the by-name 1:1 assertion: an
// expected node with no gentx in the assemble dir must fail before mutation.
func TestVerifyAssembledGentxs_MissingGentx(t *testing.T) {
	homeDir := t.TempDir()
	a := NewGenesisAssembler(homeDir, "b", "r", "chain", nil, nil)
	dir := a.assembledGentxDir()

	writeGentxFixture(t, dir, "gentx-val-0.json", randomSeiAddr(t)) // val-1 absent

	err := a.verifyAssembledGentxs([]string{"val-0", "val-1"})
	if err == nil {
		t.Fatal("expected missing-gentx error, got nil")
	}
	if !strings.Contains(err.Error(), "reading gentx for node val-1") {
		t.Errorf("error = %q, want substring 'reading gentx for node val-1'", err.Error())
	}
}
