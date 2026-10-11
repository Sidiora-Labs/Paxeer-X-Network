package types

import (
	"errors"
	"testing"
)

func authMsgsPayload(ordinal, variant, fields byte, length int) []byte {
	payload := make([]byte, length)
	payload[0] = 0x71
	payload[1] = ordinal
	payload[2] = variant
	payload[3] = fields
	return payload
}

func authMsgsRevoke(reason byte) []byte {
	payload := authMsgsPayload(6, 0, 3, 45)
	payload[36] = reason
	return payload
}

func TestSessionDecode(t *testing.T) {
	badTag := authMsgsPayload(5, 1, 3, 52)
	badTag[0] = 0x70
	cases := []struct {
		name    string
		payload []byte
		want    error
	}{
		{"session minimum", authMsgsPayload(5, 1, 3, 52), nil},
		{"successor minimum", authMsgsPayload(5, 2, 5, 52), nil},
		{"session maximum", authMsgsPayload(5, 1, 3, 1024), nil},
		{"51 bytes", authMsgsPayload(5, 1, 3, 51), ErrNonCanonical},
		{"empty", nil, ErrNonCanonical},
		{"1025 bytes", authMsgsPayload(5, 1, 3, 1025), ErrNonCanonical},
		{"bad tag", badTag, ErrNonCanonical},
		{"revoke ordinal", authMsgsPayload(6, 1, 3, 52), ErrNonCanonical},
		{"variant 0", authMsgsPayload(5, 0, 3, 52), ErrNonCanonical},
		{"variant 3", authMsgsPayload(5, 3, 3, 52), ErrNonCanonical},
		{"session with 5 fields", authMsgsPayload(5, 1, 5, 52), ErrNonCanonical},
		{"successor with 3 fields", authMsgsPayload(5, 2, 3, 52), ErrNonCanonical},
		{"session with 4 fields", authMsgsPayload(5, 1, 4, 52), ErrNonCanonical},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateSessionPayload(tc.payload)
			if tc.want == nil {
				if err != nil {
					t.Fatalf("got %v, want nil", err)
				}
				return
			}
			if !errors.Is(err, tc.want) {
				t.Fatalf("got %v, want %v", err, tc.want)
			}
		})
	}
}

func TestRevokeDecode(t *testing.T) {
	for reason := byte(1); reason <= 5; reason++ {
		if err := ValidateRevokePayload(authMsgsRevoke(reason)); err != nil {
			t.Fatalf("reason %d: got %v, want nil", reason, err)
		}
	}
	badTag := authMsgsRevoke(1)
	badTag[0] = 0x72
	cases := []struct {
		name    string
		payload []byte
		want    error
	}{
		{"44 bytes", authMsgsPayload(6, 0, 3, 44), ErrNonCanonical},
		{"46 bytes", authMsgsPayload(6, 0, 3, 46), ErrNonCanonical},
		{"empty", nil, ErrNonCanonical},
		{"bad tag", badTag, ErrNonCanonical},
		{"session ordinal", authMsgsPayload(5, 0, 3, 45), ErrNonCanonical},
		{"variant 1", authMsgsPayload(6, 1, 3, 45), ErrNonCanonical},
		{"2 fields", authMsgsPayload(6, 0, 2, 45), ErrNonCanonical},
		{"5 fields", authMsgsPayload(6, 0, 5, 45), ErrNonCanonical},
		{"reason 0", authMsgsRevoke(0), ErrAuthScope},
		{"reason 6", authMsgsRevoke(6), ErrAuthScope},
		{"reason 255", authMsgsRevoke(255), ErrAuthScope},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := ValidateRevokePayload(tc.payload); !errors.Is(err, tc.want) {
				t.Fatalf("got %v, want %v", err, tc.want)
			}
		})
	}
}
