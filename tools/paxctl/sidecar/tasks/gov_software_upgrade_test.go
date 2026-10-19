package tasks

import (
	"context"
	"strings"
	"testing"

	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	upgradetypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/upgrade/types"

	"github.com/sei-protocol/seictl/sidecar/engine"
)

func validSoftwareUpgradeRequest() GovSoftwareUpgradeRequest {
	return GovSoftwareUpgradeRequest{
		ChainID:        "pacific-1",
		KeyName:        "node_admin",
		Title:          "Upgrade to v0.42",
		Description:    "Routine release; binaries published in release notes.",
		UpgradeName:    "v0.42",
		UpgradeHeight:  10_000_000,
		UpgradeInfo:    "https://github.com/sei-protocol/sei-chain/releases/tag/v0.42",
		InitialDeposit: "10000000usei",
		Fees:           "4000usei",
		Gas:            300_000,
	}
}

func TestBuildSoftwareUpgradeMsg(t *testing.T) {
	kr, addr := testKeyring(t)
	cfg := engine.ExecutionConfig{Keyring: kr}

	t.Run("happy path", func(t *testing.T) {
		msg, err := buildSoftwareUpgradeMsg(cfg, validSoftwareUpgradeRequest())
		if err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
		if msg.Proposer != addr.String() {
			t.Errorf("proposer = %q, want %q", msg.Proposer, addr.String())
		}
		want, _ := sdk.ParseCoinsNormalized("10000000usei")
		if !msg.InitialDeposit.IsEqual(want) {
			t.Errorf("InitialDeposit = %v, want %v", msg.InitialDeposit, want)
		}
		// Lock that the SDK's MsgSubmitProposal.ValidateBasic and the
		// wrapped SoftwareUpgradeProposal.ValidateBasic both accept what
		// we produce — signAndBroadcast runs them next.
		if err := msg.ValidateBasic(); err != nil {
			t.Errorf("ValidateBasic on returned msg: %v", err)
		}
		content := msg.GetContent()
		if _, ok := content.(*upgradetypes.SoftwareUpgradeProposal); !ok {
			t.Errorf("content type = %T, want *SoftwareUpgradeProposal", content)
		}
	})

	mutations := []struct {
		name      string
		mutate    func(*GovSoftwareUpgradeRequest)
		wantInErr string
	}{
		{"missing keyName", func(r *GovSoftwareUpgradeRequest) { r.KeyName = "" }, "keyName required"},
		{"missing title", func(r *GovSoftwareUpgradeRequest) { r.Title = "" }, "title required"},
		{"missing description", func(r *GovSoftwareUpgradeRequest) { r.Description = "" }, "description required"},
		{"missing upgradeName", func(r *GovSoftwareUpgradeRequest) { r.UpgradeName = "" }, "upgradeName required"},
		{"zero upgradeHeight", func(r *GovSoftwareUpgradeRequest) { r.UpgradeHeight = 0 }, "upgradeHeight required"},
		{"negative upgradeHeight", func(r *GovSoftwareUpgradeRequest) { r.UpgradeHeight = -1 }, "upgradeHeight required"},
		{"unparseable deposit", func(r *GovSoftwareUpgradeRequest) { r.InitialDeposit = "not-a-coin" }, "parse initialDeposit"},
		{"zero deposit", func(r *GovSoftwareUpgradeRequest) { r.InitialDeposit = "0usei" }, "zero coins"},
		{"non-usei deposit", func(r *GovSoftwareUpgradeRequest) { r.InitialDeposit = "10sei" }, "usei"},
		{"mixed-denom deposit", func(r *GovSoftwareUpgradeRequest) { r.InitialDeposit = "10000000usei,1uatom" }, "uatom"},
	}
	for _, m := range mutations {
		t.Run(m.name+" is Terminal", func(t *testing.T) {
			req := validSoftwareUpgradeRequest()
			m.mutate(&req)
			_, err := buildSoftwareUpgradeMsg(cfg, req)
			if !IsTerminal(err) {
				t.Fatalf("want Terminal, got %v", err)
			}
			if !strings.Contains(err.Error(), m.wantInErr) {
				t.Fatalf("err = %q, want substring %q", err.Error(), m.wantInErr)
			}
		})
	}

	t.Run("nil keyring is Terminal", func(t *testing.T) {
		_, err := buildSoftwareUpgradeMsg(engine.ExecutionConfig{}, validSoftwareUpgradeRequest())
		if !IsTerminal(err) {
			t.Fatalf("want Terminal, got %v", err)
		}
	})

	t.Run("missing key is Terminal", func(t *testing.T) {
		req := validSoftwareUpgradeRequest()
		req.KeyName = "ghost"
		_, err := buildSoftwareUpgradeMsg(cfg, req)
		if !IsTerminal(err) {
			t.Fatalf("want Terminal, got %v", err)
		}
	})
}

// TestGovSoftwareUpgradeHandler_HappyPath threads the handler
// end-to-end through signAndBroadcast with a fake txClient. The
// MsgSubmitProposal content is packed as an Any, so reaching the
// broadcast step at all proves newSignTxInterfaceRegistry has the
// upgrade-types registration; a missing registration fails in
// txCfg.TxEncoder before the fakeTxClient ever sees bytes.
func TestGovSoftwareUpgradeHandler_HappyPath(t *testing.T) {
	cfg, _ := newGuardCfg(t, "pacific-1")
	tc := &fakeTxClient{
		accountNumber: 17,
		sequence:      42,
		broadcastResp: &sdk.TxResponse{Code: 0, TxHash: "h", Height: 0},
		queryDefault:  &sdk.TxResponse{Code: 0, Height: 7},
	}

	msg, err := buildSoftwareUpgradeMsg(cfg, validSoftwareUpgradeRequest())
	if err != nil {
		t.Fatalf("buildSoftwareUpgradeMsg: %v", err)
	}
	info, err := cfg.Keyring.Key("node_admin")
	if err != nil {
		t.Fatalf("keyring: %v", err)
	}

	result, err := signAndBroadcast(context.Background(), cfg, tc, SignAndBroadcastInput{
		ChainID: "pacific-1",
		KeyName: "node_admin",
		Msg:     msg,
		Fees:    "4000usei",
		Gas:     300_000,
		TaskID:  "00000000-0000-0000-0000-0000000000aa",
	}, info.GetAddress())
	if err != nil {
		t.Fatalf("signAndBroadcast: %v", err)
	}
	if result.TxHash != "h" {
		t.Errorf("TxHash = %q, want %q", result.TxHash, "h")
	}
	if tc.broadcasts != 1 {
		t.Errorf("broadcasts = %d, want 1", tc.broadcasts)
	}
}
