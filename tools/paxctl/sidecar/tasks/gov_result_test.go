package tasks

import (
	"errors"
	"fmt"
	"testing"
	"time"

	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	govtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/gov/types"

	"github.com/sei-protocol/seictl/sidecar/wire"
)

// mustMarshalTxMsgData builds the baseapp-encoded TxMsgData bytes a
// committed tx carries in its result Data, wrapping one message response
// under the given type URL — the shape parseProposalID decodes. Mirrors
// baseapp's runMsgs (sei-cosmos abci.pb.go's TxMsgData/MsgData wrapping a
// raw, non-Any response).
func mustMarshalTxMsgData(t *testing.T, msgType string, respBytes []byte) string {
	t.Helper()
	dataBytes, err := (&sdk.TxMsgData{Data: []*sdk.MsgData{{
		MsgType: msgType,
		Data:    respBytes,
	}}}).Marshal()
	if err != nil {
		t.Fatalf("marshal TxMsgData: %v", err)
	}
	return string(dataBytes)
}

func mustMarshalSubmitProposalResponse(t *testing.T, proposalID uint64) string {
	t.Helper()
	respBytes, err := (&govtypes.MsgSubmitProposalResponse{ProposalId: proposalID}).Marshal()
	if err != nil {
		t.Fatalf("marshal MsgSubmitProposalResponse: %v", err)
	}
	return mustMarshalTxMsgData(t, submitProposalMsgType, respBytes)
}

// An unverifiable result (broadcast accepted, outcome unobservable because the
// node's tx index is off) classifies as a TERMINAL InclusionUnverifiable
// wrapping errTxIndexingDisabled — the honest "outcome unknown, stop retrying",
// distinct from both committed_failed and the retryable pending case.
func TestClassifyGovResult_Unverifiable_Terminal(t *testing.T) {
	out, err := classifyGovResult("gov-param-change", &SignAndBroadcastResult{
		TxHash: "ABC", Unverifiable: true,
	})
	if out.InclusionStatus != wire.InclusionUnverifiable || out.TxHash != "ABC" {
		t.Fatalf("unexpected result: %+v", out)
	}
	if !IsTerminal(err) {
		t.Fatalf("unverifiable must be terminal (retrying is futile), got %v", err)
	}
	if !errors.Is(err, errTxIndexingDisabled) {
		t.Fatalf("terminal error should wrap errTxIndexingDisabled, got %v", err)
	}
}

func TestClassifyGovResult_CommittedOK(t *testing.T) {
	now := time.Now().UTC()
	out, err := classifyGovResult("gov-software-upgrade", &SignAndBroadcastResult{
		TxHash: "ABC", Height: 10, Code: 0, IncludedAt: &now, ProposalID: 5,
	})
	if err != nil {
		t.Fatalf("committed-ok should not error, got %v", err)
	}
	if out.InclusionStatus != wire.InclusionCommittedOK || out.ProposalID != 5 || out.TxHash != "ABC" {
		t.Fatalf("unexpected result: %+v", out)
	}
}

func TestClassifyGovResult_CommittedFailed_Terminal(t *testing.T) {
	now := time.Now().UTC()
	out, err := classifyGovResult("gov-param-change", &SignAndBroadcastResult{
		TxHash: "ABC", Height: 10, Code: 11, Codespace: "sdk", RawLog: "insufficient funds", IncludedAt: &now,
	})
	if err == nil || !IsTerminal(err) {
		t.Fatalf("committed-failed should be a terminal error, got %v", err)
	}
	if out.InclusionStatus != wire.InclusionCommittedFailed || out.Code != 11 {
		t.Fatalf("unexpected result: %+v", out)
	}
}

func TestClassifyGovResult_Pending_NonTerminal(t *testing.T) {
	out, err := classifyGovResult("gov-vote", &SignAndBroadcastResult{
		TxHash: "ABC", IncludedAt: nil,
	})
	if err == nil || IsTerminal(err) {
		t.Fatalf("pending should be a non-terminal error (controller re-submits), got %v", err)
	}
	if out.InclusionStatus != wire.InclusionPending || out.TxHash != "ABC" {
		t.Fatalf("unexpected result: %+v", out)
	}
}

func TestParseProposalID(t *testing.T) {
	resp := &sdk.TxResponse{
		TxHash: "ABC",
		Height: 10,
		Data:   mustMarshalSubmitProposalResponse(t, 42),
	}
	if got := parseProposalID(resp); got != 42 {
		t.Fatalf("proposal id = %d, want 42", got)
	}
}

// A tx result carrying no message responses (empty Data) yields 0, not a panic
// — e.g. a vote or non-Msg-service tx passed through the same helper.
func TestParseProposalID_EmptyData(t *testing.T) {
	if got := parseProposalID(&sdk.TxResponse{}); got != 0 {
		t.Fatalf("empty Data should yield 0, got %d", got)
	}
}

// Garbage Data (not a valid TxMsgData) yields 0, not a panic or error.
func TestParseProposalID_MalformedData(t *testing.T) {
	resp := &sdk.TxResponse{TxHash: "ABC", Height: 10, Data: "not a valid protobuf message"}
	if got := parseProposalID(resp); got != 0 {
		t.Fatalf("malformed Data should yield 0, got %d", got)
	}
}

// TxMsgData with zero message responses yields 0, not an index panic.
func TestParseProposalID_NoMessageResponses(t *testing.T) {
	dataBytes, err := (&sdk.TxMsgData{}).Marshal()
	if err != nil {
		t.Fatalf("marshal empty TxMsgData: %v", err)
	}
	resp := &sdk.TxResponse{TxHash: "ABC", Height: 10, Data: string(dataBytes)}
	if got := parseProposalID(resp); got != 0 {
		t.Fatalf("zero message responses should yield 0, got %d", got)
	}
}

// A vote's response at index 0 must not be decoded as a MsgSubmitProposal
// response: MsgVoteResponse is an empty message, so a naive decode would
// succeed and silently return 0 for the wrong reason — the MsgType guard
// must reject it before the decode is even attempted. Pins the guard
// against a future message type whose response DOES have a field 1, which
// would otherwise decode into a plausible-looking, wrong proposal ID.
func TestParseProposalID_WrongMsgType(t *testing.T) {
	respBytes, err := (&govtypes.MsgVoteResponse{}).Marshal()
	if err != nil {
		t.Fatalf("marshal MsgVoteResponse: %v", err)
	}
	resp := &sdk.TxResponse{
		TxHash: "ABC", Height: 10,
		Data: mustMarshalTxMsgData(t, sdk.MsgTypeURL(&govtypes.MsgVote{}), respBytes),
	}
	if got := parseProposalID(resp); got != 0 {
		t.Fatalf("a vote response must not be decoded as MsgSubmitProposalResponse, got %d", got)
	}
}

// Index 0 is the MsgSubmitProposal response even when the tx result carries
// additional message responses after it — pins the index-0, not-last
// semantics the fix relies on (SignAndBroadcastInput never actually batches
// today, but this documents why index 0 specifically is correct).
func TestParseProposalID_MultipleMessageResponses(t *testing.T) {
	submitBytes, err := (&govtypes.MsgSubmitProposalResponse{ProposalId: 7}).Marshal()
	if err != nil {
		t.Fatalf("marshal MsgSubmitProposalResponse: %v", err)
	}
	voteBytes, err := (&govtypes.MsgVoteResponse{}).Marshal()
	if err != nil {
		t.Fatalf("marshal MsgVoteResponse: %v", err)
	}
	dataBytes, err := (&sdk.TxMsgData{Data: []*sdk.MsgData{
		{MsgType: submitProposalMsgType, Data: submitBytes},
		{MsgType: sdk.MsgTypeURL(&govtypes.MsgVote{}), Data: voteBytes},
	}}).Marshal()
	if err != nil {
		t.Fatalf("marshal TxMsgData: %v", err)
	}
	resp := &sdk.TxResponse{TxHash: "ABC", Height: 10, Data: string(dataBytes)}
	if got := parseProposalID(resp); got != 7 {
		t.Fatalf("proposal id = %d, want 7 (from index 0)", got)
	}
}

func TestRequireProposalID(t *testing.T) {
	t.Run("committed-ok with zero ID is escalated to Terminal", func(t *testing.T) {
		out := &wire.GovTxResult{TxHash: "ABC", InclusionStatus: wire.InclusionCommittedOK, ProposalID: 0}
		err := requireProposalID(out, nil)
		if err == nil || !IsTerminal(err) {
			t.Fatalf("want a Terminal error, got %v", err)
		}
	})
	t.Run("committed-ok with a real ID passes through nil", func(t *testing.T) {
		out := &wire.GovTxResult{TxHash: "ABC", InclusionStatus: wire.InclusionCommittedOK, ProposalID: 5}
		if err := requireProposalID(out, nil); err != nil {
			t.Fatalf("want nil, got %v", err)
		}
	})
	t.Run("pending with zero ID is not escalated — a vote or not-yet-included tx legitimately has no ID", func(t *testing.T) {
		out := &wire.GovTxResult{TxHash: "ABC", InclusionStatus: wire.InclusionPending, ProposalID: 0}
		if err := requireProposalID(out, nil); err != nil {
			t.Fatalf("pending must not be escalated, got %v", err)
		}
	})
	t.Run("an existing cerr is preserved, not overridden", func(t *testing.T) {
		out := &wire.GovTxResult{TxHash: "ABC", InclusionStatus: wire.InclusionCommittedFailed, ProposalID: 0}
		want := Terminal(fmt.Errorf("committed but failed"))
		if got := requireProposalID(out, want); got != want {
			t.Fatalf("want the original error preserved, got %v", got)
		}
	})
}

// TestVoteOptionValuesMatchGovtypes guards the wire.VoteOption consts against
// drift from the cosmos govtypes enum the server casts to
// (govtypes.VoteOption(wire.Option*)). A renumber would silently mismap a vote.
func TestVoteOptionValuesMatchGovtypes(t *testing.T) {
	cases := []struct {
		w wire.VoteOption
		g govtypes.VoteOption
	}{
		{wire.OptionEmpty, govtypes.OptionEmpty},
		{wire.OptionYes, govtypes.OptionYes},
		{wire.OptionAbstain, govtypes.OptionAbstain},
		{wire.OptionNo, govtypes.OptionNo},
		{wire.OptionNoWithVeto, govtypes.OptionNoWithVeto},
	}
	for _, c := range cases {
		if int(c.w) != int(c.g) {
			t.Fatalf("wire.VoteOption %d != govtypes %d — ParseVoteOption cast would mismap", c.w, c.g)
		}
	}
}
