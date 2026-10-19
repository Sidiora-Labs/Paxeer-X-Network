package tasks

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tmtypes "github.com/sei-protocol/sei-chain/sei-tendermint/types"

	"github.com/sei-protocol/sei-chain/sei-cosmos/client"
	"github.com/sei-protocol/sei-chain/sei-cosmos/codec"
	"github.com/sei-protocol/sei-chain/sei-cosmos/crypto/keys/ed25519"
	"github.com/sei-protocol/sei-chain/sei-cosmos/crypto/keys/secp256k1"
	cryptotypes "github.com/sei-protocol/sei-chain/sei-cosmos/crypto/types"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/x/genutil"
	genutiltypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/genutil/types"
	stakingtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/staking/types"
)

// buildTestGentx builds an (unsigned) gentx carrying a single MsgCreateValidator
// with the given consensus pubkey and self-bond. Signatures are irrelevant to
// validator derivation, which only decodes the message.
func buildTestGentx(t *testing.T, txCfg client.TxConfig, consPub cryptotypes.PubKey, selfBond sdk.Coin) json.RawMessage {
	t.Helper()
	valAddr := sdk.ValAddress(secp256k1.GenPrivKey().PubKey().Address())
	msg, err := stakingtypes.NewMsgCreateValidator(
		valAddr, consPub, selfBond,
		stakingtypes.Description{Moniker: "val"},
		stakingtypes.NewCommissionRates(sdk.ZeroDec(), sdk.ZeroDec(), sdk.ZeroDec()),
		sdk.OneInt(),
	)
	if err != nil {
		t.Fatalf("NewMsgCreateValidator: %v", err)
	}

	b := txCfg.NewTxBuilder()
	if err := b.SetMsgs(msg); err != nil {
		t.Fatalf("SetMsgs: %v", err)
	}
	bz, err := txCfg.TxJSONEncoder()(b.GetTx())
	if err != nil {
		t.Fatalf("encoding gentx: %v", err)
	}
	return bz
}

// writeAssembledGenesis writes a homeDir/config/genesis.json whose app_state
// carries the given gentxs and staking max_validators — the post-collect,
// pre-populate shape the assembler operates on.
func writeAssembledGenesis(t *testing.T, cdc codec.Codec, homeDir string, maxValidators uint32, genTxs []json.RawMessage) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(homeDir, "config"), 0o755); err != nil {
		t.Fatalf("mkdir config: %v", err)
	}

	stakingGen := stakingtypes.DefaultGenesisState()
	stakingGen.Params.MaxValidators = maxValidators

	appState := map[string]json.RawMessage{
		stakingtypes.ModuleName: cdc.MustMarshalJSON(stakingGen),
		genutiltypes.ModuleName: cdc.MustMarshalJSON(genutiltypes.NewGenesisState(genTxs)),
	}
	appStateBz, err := json.Marshal(appState)
	if err != nil {
		t.Fatalf("marshal app_state: %v", err)
	}

	genDoc := &tmtypes.GenesisDoc{
		ChainID:         "assemble-validators-test",
		GenesisTime:     time.Now(),
		ConsensusParams: tmtypes.DefaultConsensusParams(),
		AppState:        appStateBz,
	}
	if err := genutil.ExportGenesisFile(genDoc, filepath.Join(homeDir, "config", "genesis.json")); err != nil {
		t.Fatalf("export genesis: %v", err)
	}
}

func TestPopulateGenesisValidators_NValidators(t *testing.T) {
	ensureBech32()
	cdc, txCfg := makeCodec()

	// Distinct ed25519 consensus keys and distinct powers. Stakes are exact
	// multiples of the power reduction (1e6) so truncation is unambiguous.
	type spec struct {
		pub   cryptotypes.PubKey
		stake int64
	}
	specs := []spec{
		{ed25519.GenPrivKey().PubKey(), 3_000_000},
		{ed25519.GenPrivKey().PubKey(), 7_000_000},
	}
	genTxs := make([]json.RawMessage, len(specs))
	for i, s := range specs {
		genTxs[i] = buildTestGentx(t, txCfg, s.pub, sdk.NewCoin("usei", sdk.NewInt(s.stake)))
	}

	homeDir := t.TempDir()
	writeAssembledGenesis(t, cdc, homeDir, 100, genTxs)

	a := NewGenesisAssembler(homeDir, "b", "r", "assemble-validators-test", nil, nil)
	if err := a.populateGenesisValidators(); err != nil {
		t.Fatalf("populateGenesisValidators: %v", err)
	}

	genDoc, err := tmtypes.GenesisDocFromFile(filepath.Join(homeDir, "config", "genesis.json"))
	if err != nil {
		t.Fatalf("reading genesis: %v", err)
	}
	if got, want := len(genDoc.Validators), len(specs); got != want {
		t.Fatalf("len(genDoc.Validators) = %d, want %d", got, want)
	}

	for i, s := range specs {
		gotVal := genDoc.Validators[i]

		// Independent power oracle: reimplement the power reduction as literal
		// integer division rather than calling sdk.TokensToConsensusPower, so a
		// change to Sei's power math breaks this test instead of tracking the
		// derivation that also calls it.
		wantPower := s.stake / 1_000_000
		if got := gotVal.Power; got != wantPower {
			t.Errorf("validator %d power = %d, want %d", i, got, wantPower)
		}

		// Independent pubkey oracle: the cosmos ed25519 pubkey's 32 raw key bytes
		// must survive the proto oneof roundtrip and equal the derived CometBFT
		// pubkey's bytes, without going back through ToTmPubKeyInterface.
		wantPubKey := s.pub.Bytes()
		if got := gotVal.PubKey.Bytes(); !bytes.Equal(got, wantPubKey) {
			t.Errorf("validator %d pubkey = %x, want %x", i, got, wantPubKey)
		}
		if got := len(wantPubKey); got != 32 {
			t.Errorf("validator %d source pubkey length = %d, want 32", i, got)
		}
	}
}

func TestPopulateGenesisValidators_ZeroPowerFailsLoud(t *testing.T) {
	ensureBech32()
	cdc, txCfg := makeCodec()

	// Stake below the power reduction (1e6) => consensus power 0.
	genTxs := []json.RawMessage{
		buildTestGentx(t, txCfg, ed25519.GenPrivKey().PubKey(), sdk.NewCoin("usei", sdk.NewInt(999_999))),
	}
	homeDir := t.TempDir()
	writeAssembledGenesis(t, cdc, homeDir, 100, genTxs)

	a := NewGenesisAssembler(homeDir, "b", "r", "assemble-validators-test", nil, nil)
	err := a.populateGenesisValidators()
	if err == nil {
		t.Fatal("expected zero-power error, got nil")
	}
	if !strings.Contains(err.Error(), "consensus power 0") {
		t.Errorf("error = %q, want substring 'consensus power 0'", err.Error())
	}
}

func TestPopulateGenesisValidators_ExceedsMaxValidatorsFailsLoud(t *testing.T) {
	ensureBech32()
	cdc, txCfg := makeCodec()

	genTxs := []json.RawMessage{
		buildTestGentx(t, txCfg, ed25519.GenPrivKey().PubKey(), sdk.NewCoin("usei", sdk.NewInt(1_000_000))),
		buildTestGentx(t, txCfg, ed25519.GenPrivKey().PubKey(), sdk.NewCoin("usei", sdk.NewInt(1_000_000))),
	}
	homeDir := t.TempDir()
	writeAssembledGenesis(t, cdc, homeDir, 1, genTxs) // max_validators=1, but 2 gentxs

	a := NewGenesisAssembler(homeDir, "b", "r", "assemble-validators-test", nil, nil)
	err := a.populateGenesisValidators()
	if err == nil {
		t.Fatal("expected max-validators error, got nil")
	}
	if !strings.Contains(err.Error(), "max_validators") {
		t.Errorf("error = %q, want substring 'max_validators'", err.Error())
	}
}

// TestPopulateGenesisValidators_DuplicateConsensusKeyFailsLoud: two gentxs (with
// distinct operators, so the delegator/owner guards do not fire) sharing one
// consensus key would panic CometBFT's NewValidatorSet at boot. The derivation
// must reject it at assembly instead.
func TestPopulateGenesisValidators_DuplicateConsensusKeyFailsLoud(t *testing.T) {
	ensureBech32()
	cdc, txCfg := makeCodec()

	shared := ed25519.GenPrivKey().PubKey()
	genTxs := []json.RawMessage{
		buildTestGentx(t, txCfg, shared, sdk.NewCoin("usei", sdk.NewInt(1_000_000))),
		buildTestGentx(t, txCfg, shared, sdk.NewCoin("usei", sdk.NewInt(1_000_000))),
	}
	homeDir := t.TempDir()
	writeAssembledGenesis(t, cdc, homeDir, 100, genTxs)

	a := NewGenesisAssembler(homeDir, "b", "r", "assemble-validators-test", nil, nil)
	err := a.populateGenesisValidators()
	if err == nil {
		t.Fatal("expected duplicate-consensus-key error, got nil")
	}
	if !strings.Contains(err.Error(), "share consensus key") {
		t.Errorf("error = %q, want substring 'share consensus key'", err.Error())
	}
}
