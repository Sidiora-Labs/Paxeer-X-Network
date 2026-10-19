package tasks

import (
	"context"
	"strings"
	"testing"

	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	proposal "github.com/sei-protocol/sei-chain/sei-cosmos/x/params/types/proposal"

	"github.com/sei-protocol/seictl/sidecar/engine"
)

func validParamChangeRequest() GovParamChangeRequest {
	return GovParamChangeRequest{
		ChainID:     "arctic-1",
		KeyName:     "node_admin",
		Title:       "Update Consensus Timeout Params",
		Description: "Tighten consensus timeouts.",
		Changes: []paramChange{
			// struct-valued param (object)
			{Subspace: "baseapp", Key: "TimeoutParams", Value: []byte(`{"propose":"300000000","commit":"200000000"}`)},
		},
		InitialDeposit: "10000000usei",
		Fees:           "8000usei",
		Gas:            300_000,
	}
}

func TestBuildParamChangeMsg(t *testing.T) {
	kr, addr := testKeyring(t)
	cfg := engine.ExecutionConfig{Keyring: kr}

	t.Run("happy path", func(t *testing.T) {
		msg, err := buildParamChangeMsg(cfg, validParamChangeRequest())
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
		if err := msg.ValidateBasic(); err != nil {
			t.Errorf("ValidateBasic on returned msg: %v", err)
		}
		if _, ok := msg.GetContent().(*proposal.ParameterChangeProposal); !ok {
			t.Errorf("content type = %T, want *ParameterChangeProposal", msg.GetContent())
		}
	})

	// Regression guard for the prop-252 double-encode bug: the raw JSON
	// value must reach ParamChange.Value stringified exactly ONCE, for any
	// JSON shape — an object or a bare scalar. A value of {"a":"b"} must
	// become {"a":"b"}, never "{\"a\":\"b\"}".
	t.Run("value single-encoded for object and scalar", func(t *testing.T) {
		cases := []struct {
			name, raw string
		}{
			{"object", `{"propose":"300000000","commit":"200000000"}`},
			{"scalar-string", `"86400000000000"`},
			{"scalar-number", `100`},
			{"scalar-bool", `true`},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				req := validParamChangeRequest()
				req.Changes = []paramChange{{Subspace: "staking", Key: "K", Value: []byte(tc.raw)}}
				msg, err := buildParamChangeMsg(cfg, req)
				if err != nil {
					t.Fatalf("unexpected err: %v", err)
				}
				pcp := msg.GetContent().(*proposal.ParameterChangeProposal)
				if got := pcp.Changes[0].Value; got != tc.raw {
					t.Errorf("ParamChange.Value = %q, want %q (double-encoded?)", got, tc.raw)
				}
			})
		}
	})

	t.Run("non-usei deposit rejected", func(t *testing.T) {
		req := validParamChangeRequest()
		req.InitialDeposit = "10000000uatom"
		if _, err := buildParamChangeMsg(cfg, req); err == nil {
			t.Fatal("expected error for non-usei deposit")
		} else if !strings.Contains(err.Error(), "not permitted") {
			t.Errorf("err = %v, want denom-not-permitted", err)
		}
	})

	t.Run("validation failures are Terminal", func(t *testing.T) {
		cases := []struct {
			name string
			mut  func(*GovParamChangeRequest)
		}{
			{"missing keyName", func(r *GovParamChangeRequest) { r.KeyName = "" }},
			{"missing title", func(r *GovParamChangeRequest) { r.Title = "" }},
			{"missing description", func(r *GovParamChangeRequest) { r.Description = "" }},
			{"empty changes", func(r *GovParamChangeRequest) { r.Changes = nil }},
			{"empty subspace", func(r *GovParamChangeRequest) { r.Changes[0].Subspace = "" }},
			{"empty key", func(r *GovParamChangeRequest) { r.Changes[0].Key = "" }},
			{"empty value", func(r *GovParamChangeRequest) { r.Changes[0].Value = nil }},
			{"non-usei deposit", func(r *GovParamChangeRequest) { r.InitialDeposit = "1uatom" }},
			{"zero-coin deposit", func(r *GovParamChangeRequest) { r.InitialDeposit = "0usei" }},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				req := validParamChangeRequest()
				tc.mut(&req)
				_, err := buildParamChangeMsg(cfg, req)
				if err == nil {
					t.Fatalf("expected error")
				}
				if !IsTerminal(err) {
					t.Errorf("err = %v, want Terminal", err)
				}
			})
		}
	})
}

// TestGovParamChangeHandler_HappyPath threads the handler end-to-end
// through signAndBroadcast with a fake txClient. The MsgSubmitProposal
// content (a ParameterChangeProposal) is packed as an Any, so reaching
// the broadcast step proves newSignTxInterfaceRegistry registers the
// x/params proposal interfaces; a missing registration fails in
// txCfg.TxEncoder before the fakeTxClient ever sees bytes.
func TestGovParamChangeHandler_HappyPath(t *testing.T) {
	cfg, _ := newGuardCfg(t, "arctic-1")
	tc := &fakeTxClient{
		accountNumber: 17,
		sequence:      42,
		broadcastResp: &sdk.TxResponse{Code: 0, TxHash: "h", Height: 0},
		queryDefault:  &sdk.TxResponse{Code: 0, Height: 7},
	}

	req := validParamChangeRequest()
	msg, err := buildParamChangeMsg(cfg, req)
	if err != nil {
		t.Fatalf("buildParamChangeMsg: %v", err)
	}
	info, err := cfg.Keyring.Key("node_admin")
	if err != nil {
		t.Fatalf("keyring: %v", err)
	}

	result, err := signAndBroadcast(context.Background(), cfg, tc, SignAndBroadcastInput{
		ChainID: "arctic-1",
		KeyName: "node_admin",
		Msg:     msg,
		Fees:    req.Fees,
		Gas:     req.Gas,
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
