package types

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	sdkerrors "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types/errors"
)

// kernelResultCodes reads the kernel result table so every registered error
// is checked against the code it stands for.
func kernelResultCodes(t *testing.T) map[string]int64 {
	t.Helper()
	header, err := os.ReadFile(filepath.Join("..", "..", "..", "include", "layerx", "lxp_result.h"))
	if err != nil {
		t.Fatalf("read kernel result table: %v", err)
	}
	entry := regexp.MustCompile(`X\((LXP_[A-Z0-9_]+), (-?[0-9]+)\)`)
	codes := make(map[string]int64)
	for _, m := range entry.FindAllStringSubmatch(string(header), -1) {
		value, err := strconv.ParseInt(m[2], 10, 32)
		if err != nil {
			t.Fatalf("parse code of %s: %v", m[1], err)
		}
		codes[m[1]] = value
	}
	if len(codes) == 0 {
		t.Fatal("kernel result table has no entries")
	}
	return codes
}

func TestErrorTexts(t *testing.T) {
	codes := kernelResultCodes(t)

	recoverable := []struct {
		kernelName string
		err        *sdkerrors.Error
	}{
		{"LXP_ERR_NON_CANONICAL", ErrNonCanonical},
		{"LXP_ERR_MALFORMED_SEND", ErrMalformedSend},
		{"LXP_ERR_UNAUTHORIZED_DEBIT", ErrUnauthorizedDebit},
		{"LXP_ERR_CONTEXT_MISMATCH", ErrContextMismatch},
		{"LXP_ERR_NO_PAYER_GRANT", ErrNoPayerGrant},
		{"LXP_ERR_IDEMPOTENT_REPLAY", ErrIdempotentReplay},
		{"LXP_ERR_EXPIRED", ErrExpired},
		{"LXP_ERR_SEQUENCE_MISMATCH", ErrSequenceMismatch},
		{"LXP_ERR_SEQUENCE_EXHAUSTED", ErrSequenceExhausted},
		{"LXP_ERR_INSUFFICIENT_BALANCE", ErrInsufficientBalance},
		{"LXP_ERR_ZERO_AMOUNT", ErrZeroAmount},
		{"LXP_ERR_ASSET_MISMATCH", ErrAssetMismatch},
		{"LXP_ERR_ASSET_PAUSED", ErrAssetPaused},
		{"LXP_ERR_ASSET_ALREADY_REGISTERED", ErrAssetAlreadyRegistered},
		{"LXP_ERR_PAUSED_SCOPE", ErrPausedScope},
		{"LXP_ERR_ARENA_EXHAUSTED", ErrArenaExhausted},
	}
	if len(recoverable) != 16 {
		t.Fatalf("recoverable registry has %d entries, want 16", len(recoverable))
	}

	for _, tc := range recoverable {
		code, ok := codes[tc.kernelName]
		if !ok {
			t.Fatalf("%s is not a kernel result code", tc.kernelName)
		}
		if code > -1 || code <= -1000 {
			t.Fatalf("%s = %d is not a recoverable kernel code", tc.kernelName, code)
		}
		want := "LXP_" + strings.TrimPrefix(tc.kernelName, "LXP_ERR_")
		if got := tc.err.Error(); got != want {
			t.Errorf("%s text = %q, want %q", tc.kernelName, got, want)
		}
		if !strings.HasPrefix(tc.err.Error(), "LXP_") || strings.Contains(tc.err.Error(), "ERR_") {
			t.Errorf("%s text %q does not have the LXP_<CODE> form", tc.kernelName, tc.err.Error())
		}
		if got := tc.err.Codespace(); got != "layerxledger" {
			t.Errorf("%s codespace = %q, want layerxledger", tc.kernelName, got)
		}
		if got, want := int64(tc.err.ABCICode()), -code; got != want {
			t.Errorf("%s code = %d, want %d", tc.kernelName, got, want)
		}
	}

	fatalCode, ok := codes["LXP_FATAL_SUPPLY_MISMATCH"]
	if !ok {
		t.Fatal("LXP_FATAL_SUPPLY_MISMATCH is not a kernel result code")
	}
	if fatalCode > -1000 {
		t.Fatalf("LXP_FATAL_SUPPLY_MISMATCH = %d is not a fatal kernel code", fatalCode)
	}
	if got := ErrFatalSupplyMismatch.Error(); got != "LXP_FATAL_SUPPLY_MISMATCH" {
		t.Errorf("fatal text = %q, want LXP_FATAL_SUPPLY_MISMATCH", got)
	}
	if got, want := int64(ErrFatalSupplyMismatch.ABCICode()), -fatalCode; got != want {
		t.Errorf("fatal code = %d, want %d", got, want)
	}
	for _, tc := range recoverable {
		if tc.err.Is(ErrFatalSupplyMismatch) || ErrFatalSupplyMismatch.Is(tc.err) {
			t.Errorf("LXP_FATAL_SUPPLY_MISMATCH maps to recoverable %s", tc.err.Error())
		}
	}
}
