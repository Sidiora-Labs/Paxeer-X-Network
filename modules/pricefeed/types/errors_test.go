package types

import (
	"fmt"
	"testing"

	xwebtypes "github.com/Sidiora-Labs/Paxeer-X-Network/modules/xweb/types"
	sdkerrors "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types/errors"
)

func TestLXPName(t *testing.T) {
	cases := []struct {
		err  *sdkerrors.Error
		code uint32
		name string
		text string
	}{
		{ErrUnauthorized, 2, "LXP_PRICEFEED_UNAUTHORIZED", "unauthorized"},
		{ErrInvalidParams, 3, "LXP_PRICEFEED_INVALID_PARAMS", "invalid params"},
		{ErrInvalidGenesis, 4, "LXP_PRICEFEED_INVALID_GENESIS", "invalid genesis"},
		{ErrNotValidator, 5, "LXP_PRICEFEED_NOT_VALIDATOR", "not a bonded validator"},
		{ErrInvalidKey, 6, "LXP_PRICEFEED_INVALID_KEY", "feed key must be a 33-byte compressed secp256k1 public key"},
		{ErrKeyInUse, 7, "LXP_PRICEFEED_KEY_IN_USE", "feed account is registered to another validator"},
		{ErrNoKey, 8, "LXP_PRICEFEED_NO_KEY", "validator has no feed key"},
		{ErrUnknownMarket, 9, "LXP_PRICEFEED_UNKNOWN_MARKET", "market is not listed"},
		{ErrUnregisteredFeeder, 10, "LXP_PRICEFEED_UNREGISTERED_FEEDER", "feeder is not a registered feed account"},
		{ErrRateLimited, 11, "LXP_PRICEFEED_RATE_LIMITED", "the validator has already submitted prices at this height"},
		{ErrTooManyObservations, 12, "LXP_PRICEFEED_TOO_MANY_OBSERVATIONS", "too many observations in one submission"},
		{ErrSubmissionTooLarge, 13, "LXP_PRICEFEED_SUBMISSION_TOO_LARGE", "submission is over the byte bound"},
		{ErrDuplicateMarket, 14, "LXP_PRICEFEED_DUPLICATE_MARKET", "a submission carries one observation per market"},
		{ErrSubmissionAlone, 15, "LXP_PRICEFEED_SUBMISSION_ALONE", "price submissions cannot be in the same tx as other messages"},
		{ErrInvalidObservation, 16, "LXP_PRICEFEED_INVALID_OBSERVATION", "invalid observation"},
	}
	if len(cases) != 15 {
		t.Fatalf("error table has %d entries, want 15", len(cases))
	}
	for i, tc := range cases {
		if want := uint32(i + 2); tc.code != want {
			t.Fatalf("entry %d has code %d, want %d", i, tc.code, want)
		}
		if got := tc.err.Codespace(); got != "pricefeed" {
			t.Fatalf("%s codespace = %q, want pricefeed", tc.name, got)
		}
		if got := tc.err.ABCICode(); got != tc.code {
			t.Fatalf("%s ABCI code = %d, want %d", tc.name, got, tc.code)
		}
		if got := tc.err.Error(); got != tc.text {
			t.Fatalf("%s text = %q, want %q", tc.name, got, tc.text)
		}
		name, ok := LXPName(tc.err)
		if !ok || name != tc.name {
			t.Fatalf("LXPName(code %d) = (%q, %v), want (%q, true)", tc.code, name, ok, tc.name)
		}
		wrapped := sdkerrors.Wrapf(tc.err, "market %d", 7)
		name, ok = LXPName(fmt.Errorf("deliver: %w", wrapped))
		if !ok || name != tc.name {
			t.Fatalf("LXPName(wrapped code %d) = (%q, %v), want (%q, true)", tc.code, name, ok, tc.name)
		}
	}

	if name, ok := LXPName(xwebtypes.ErrUnauthorized); ok || name != "" {
		t.Fatalf("LXPName(xweb error) = (%q, %v), want (\"\", false)", name, ok)
	}
	if name, ok := LXPName(fmt.Errorf("plain failure")); ok || name != "" {
		t.Fatalf("LXPName(plain error) = (%q, %v), want (\"\", false)", name, ok)
	}
	if name, ok := LXPName(nil); ok || name != "" {
		t.Fatalf("LXPName(nil) = (%q, %v), want (\"\", false)", name, ok)
	}
}
