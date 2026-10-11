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

func TestErrorTexts(t *testing.T) {
	header, err := os.ReadFile(filepath.Join("..", "..", "..", "include", "layerx", "lxp_result.h"))
	if err != nil {
		t.Fatal(err)
	}
	kernelNames := make(map[int32]string)
	for _, match := range regexp.MustCompile(`X\((LXP_[A-Z_]+), (-?[0-9]+)\)`).FindAllStringSubmatch(string(header), -1) {
		code, err := strconv.ParseInt(match[2], 10, 32)
		if err != nil {
			t.Fatal(err)
		}
		kernelNames[int32(code)] = match[1]
	}
	cases := []struct {
		name       string
		err        *sdkerrors.Error
		code       uint32
		kernelCode int32
		text       string
	}{
		{"auth_scope", ErrAuthScope, 2, -204, "LXP_AUTH_SCOPE"},
		{"auth_revoked", ErrAuthRevoked, 3, -203, "LXP_AUTH_REVOKED"},
		{"auth_expired", ErrAuthExpired, 4, -202, "LXP_AUTH_EXPIRED"},
		{"auth_not_mature", ErrAuthNotMature, 5, -222, "LXP_AUTH_NOT_MATURE"},
		{"not_yet_valid", ErrNotYetValid, 6, -304, "LXP_NOT_YET_VALID"},
		{"sequence_reused", ErrSequenceReused, 7, -301, "LXP_SEQUENCE_REUSED"},
		{"non_canonical", ErrNonCanonical, 8, -3, "LXP_NON_CANONICAL"},
		{"unauthorized_debit", ErrUnauthorizedDebit, 9, -207, "LXP_UNAUTHORIZED_DEBIT"},
		{"idempotent_replay", ErrIdempotentReplay, 10, -302, "LXP_IDEMPOTENT_REPLAY"},
		{"paused_scope", ErrPausedScope, 11, -735, "LXP_PAUSED_SCOPE"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			kernelName, ok := kernelNames[tc.kernelCode]
			if !ok {
				t.Fatalf("kernel result %d is missing", tc.kernelCode)
			}
			prefix := strings.Replace(kernelName, "LXP_ERR_", "LXP_", 1)
			if prefix != tc.text {
				t.Fatalf("kernel text = %q, want %q", prefix, tc.text)
			}
			if !strings.HasPrefix(tc.err.Error(), prefix) {
				t.Errorf("error text = %q, want prefix %q", tc.err.Error(), prefix)
			}
			if tc.err.Error() != tc.text {
				t.Errorf("error text = %q, want %q", tc.err.Error(), tc.text)
			}
			if tc.err.Codespace() != "layerxauth" {
				t.Errorf("codespace = %q, want layerxauth", tc.err.Codespace())
			}
			if tc.err.ABCICode() != tc.code {
				t.Errorf("ABCI code = %d, want %d", tc.err.ABCICode(), tc.code)
			}
			if registered := sdkerrors.ABCIError("layerxauth", tc.code, ""); !tc.err.Is(registered) {
				t.Errorf("ABCI registry does not resolve %d to %q", tc.code, tc.text)
			}
		})
	}
}
