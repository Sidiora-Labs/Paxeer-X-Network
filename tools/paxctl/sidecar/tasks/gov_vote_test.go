package tasks

import (
	"errors"
	"testing"

	govtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/gov/types"

	"github.com/sei-protocol/seictl/sidecar/engine"
)

func TestBuildVoteMsg(t *testing.T) {
	kr, addr := testKeyring(t)
	cfg := engine.ExecutionConfig{Keyring: kr}

	t.Run("happy path", func(t *testing.T) {
		msg, err := buildVoteMsg(cfg, GovVoteRequest{
			KeyName:    "node_admin",
			ProposalID: 42,
			Option:     "yes",
		})
		if err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
		if msg.Voter != addr.String() {
			t.Errorf("voter = %q, want %q", msg.Voter, addr.String())
		}
		if msg.ProposalId != 42 {
			t.Errorf("proposalId = %d, want 42", msg.ProposalId)
		}
		if msg.Option != govtypes.OptionYes {
			t.Errorf("option = %v, want OptionYes", msg.Option)
		}
		// Guard: signAndBroadcast runs ValidateBasic immediately; lock
		// that it accepts the message we produce here.
		if err := msg.ValidateBasic(); err != nil {
			t.Errorf("ValidateBasic on returned msg: %v", err)
		}
	})

	t.Run("zero proposalId is Terminal", func(t *testing.T) {
		_, err := buildVoteMsg(cfg, GovVoteRequest{
			KeyName:    "node_admin",
			ProposalID: 0,
			Option:     "yes",
		})
		if !IsTerminal(err) {
			t.Fatalf("want Terminal, got %v", err)
		}
	})

	t.Run("invalid option is Terminal", func(t *testing.T) {
		_, err := buildVoteMsg(cfg, GovVoteRequest{
			KeyName:    "node_admin",
			ProposalID: 7,
			Option:     "bogus",
		})
		if !IsTerminal(err) {
			t.Fatalf("want Terminal, got %v", err)
		}
	})

	t.Run("nil keyring is Terminal", func(t *testing.T) {
		_, err := buildVoteMsg(engine.ExecutionConfig{}, GovVoteRequest{
			KeyName:    "node_admin",
			ProposalID: 7,
			Option:     "yes",
		})
		if !IsTerminal(err) {
			t.Fatalf("want Terminal, got %v", err)
		}
	})

	t.Run("empty keyName is Terminal", func(t *testing.T) {
		_, err := buildVoteMsg(cfg, GovVoteRequest{
			KeyName:    "",
			ProposalID: 7,
			Option:     "yes",
		})
		if !IsTerminal(err) {
			t.Fatalf("want Terminal, got %v", err)
		}
	})

	t.Run("missing key in keyring is Terminal", func(t *testing.T) {
		_, err := buildVoteMsg(cfg, GovVoteRequest{
			KeyName:    "does-not-exist",
			ProposalID: 7,
			Option:     "yes",
		})
		if !IsTerminal(err) {
			t.Fatalf("want Terminal, got %v", err)
		}
		// Make sure the underlying keyring error is preserved.
		var terr *TerminalError
		if !errors.As(err, &terr) || terr.Unwrap() == nil {
			t.Fatalf("expected wrapped keyring error: %v", err)
		}
	})
}
