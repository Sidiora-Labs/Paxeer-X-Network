package types_test

import (
	"bytes"
	"encoding/hex"
	"testing"

	"github.com/Sidiora-Labs/Paxeer-X-Network/layerxproof/codec"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/layerxpay/types"
)

func TestEscrowPayloadsCGoldens(t *testing.T) {
	owner := escrowPayloadID(t, "efc9802f76722dfc48ebfed35bfd8b20dbc2775fe2f027d6cbd595aff1307454")
	account := escrowPayloadID(t, "a781282c755f899aeff3881c53d68b14ed796f97c5f05d9b5b9da90d7c7aecc2")
	cases := []struct {
		name    string
		payload []byte
		want    string
		size    int
	}{
		{
			name:    "test_escrow_open.c",
			payload: types.EncodeEscrowOpen([32]byte{9}, owner, account, [32]byte{7}, [32]byte{8}, [32]byte{1}, codec.U128{Lo: 40}, 5000, 600, [32]byte{10}, [32]byte{11}),
			want: "0900000000000000000000000000000000000000000000000000000000000000" +
				"efc9802f76722dfc48ebfed35bfd8b20dbc2775fe2f027d6cbd595aff1307454" +
				"a781282c755f899aeff3881c53d68b14ed796f97c5f05d9b5b9da90d7c7aecc2" +
				"0700000000000000000000000000000000000000000000000000000000000000" +
				"0800000000000000000000000000000000000000000000000000000000000000" +
				"0100000000000000000000000000000000000000000000000000000000000000" +
				"0000000000000000000000000000002800000000000013880000000000000258" +
				"0a00000000000000000000000000000000000000000000000000000000000000" +
				"0b00000000000000000000000000000000000000000000000000000000000000",
			size: 288,
		},
		{
			name:    "test_escrow_capture.c/partial",
			payload: types.EncodeEscrowCapture([32]byte{3}, codec.U128{Lo: 30}, [32]byte{1}),
			want: "0300000000000000000000000000000000000000000000000000000000000000" +
				"0000000000000000000000000000001e01000000000000000000000000000000" +
				"00000000000000000000000000000000",
			size: 80,
		},
		{
			name:    "test_escrow_capture.c/full",
			payload: types.EncodeEscrowCapture([32]byte{3}, codec.U128{}, [32]byte{2}),
			want: "0300000000000000000000000000000000000000000000000000000000000000" +
				"0000000000000000000000000000000002000000000000000000000000000000" +
				"00000000000000000000000000000000",
			size: 80,
		},
		{
			name:    "test_escrow_timeout.c/release",
			payload: types.EncodeEscrowRelease([32]byte{4}, [32]byte{5}),
			want: "0400000000000000000000000000000000000000000000000000000000000000" +
				"0500000000000000000000000000000000000000000000000000000000000000",
			size: 64,
		},
		{
			name:    "test_escrow_timeout.c/timeout",
			payload: types.EncodeEscrowRelease([32]byte{5}, [32]byte{6}),
			want: "0500000000000000000000000000000000000000000000000000000000000000" +
				"0600000000000000000000000000000000000000000000000000000000000000",
			size: 64,
		},
		{
			name:    "test_escrow_dispute.c/open",
			payload: types.EncodeEscrowDisputeOpen([32]byte{4}),
			want:    "0400000000000000000000000000000000000000000000000000000000000000",
			size:    32,
		},
		{
			name:    "test_escrow_dispute.c/resolve",
			payload: types.EncodeEscrowDisputeResolve([32]byte{4}, 3333, [32]byte{1}),
			want: "0400000000000000000000000000000000000000000000000000000000000000" +
				"00000d0501000000000000000000000000000000000000000000000000000000" +
				"00000000",
			size: 68,
		},
		{
			name:    "test_escrow_dispute.c/non_canonical",
			payload: types.EncodeEscrowDisputeResolve([32]byte{4}, 10001, [32]byte{1}),
			want: "0400000000000000000000000000000000000000000000000000000000000000" +
				"0000271101000000000000000000000000000000000000000000000000000000" +
				"00000000",
			size: 68,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assertEscrowPayload(t, tc.payload, tc.want, tc.size)
		})
	}
}

func TestEscrowPayloadsFieldWidths(t *testing.T) {
	escrowID := escrowPayloadID(t, "000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f")
	owner := escrowPayloadID(t, "202122232425262728292a2b2c2d2e2f303132333435363738393a3b3c3d3e3f")
	account := escrowPayloadID(t, "404142434445464748494a4b4c4d4e4f505152535455565758595a5b5c5d5e5f")
	beneficiary := escrowPayloadID(t, "606162636465666768696a6b6c6d6e6f707172737475767778797a7b7c7d7e7f")
	arbiter := escrowPayloadID(t, "808182838485868788898a8b8c8d8e8f909192939495969798999a9b9c9d9e9f")
	assetID := escrowPayloadID(t, "a0a1a2a3a4a5a6a7a8a9aaabacadaeafb0b1b2b3b4b5b6b7b8b9babbbcbdbebf")
	termsHash := escrowPayloadID(t, "e0e1e2e3e4e5e6e7e8e9eaebecedeeeff0f1f2f3f4f5f6f7f8f9fafbfcfdfeff")
	agreementReference := escrowPayloadID(t, "000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f")
	cases := []struct {
		name    string
		payload []byte
		want    string
		size    int
	}{
		{
			name:    "open",
			payload: types.EncodeEscrowOpen(escrowID, owner, account, beneficiary, arbiter, assetID, codec.U128{Hi: 0xc0c1c2c3c4c5c6c7, Lo: 0xc8c9cacbcccdcecf}, 0xd0d1d2d3d4d5d6d7, 0xd8d9dadbdcdddedf, termsHash, agreementReference),
			want: "000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f" +
				"202122232425262728292a2b2c2d2e2f303132333435363738393a3b3c3d3e3f" +
				"404142434445464748494a4b4c4d4e4f505152535455565758595a5b5c5d5e5f" +
				"606162636465666768696a6b6c6d6e6f707172737475767778797a7b7c7d7e7f" +
				"808182838485868788898a8b8c8d8e8f909192939495969798999a9b9c9d9e9f" +
				"a0a1a2a3a4a5a6a7a8a9aaabacadaeafb0b1b2b3b4b5b6b7b8b9babbbcbdbebf" +
				"c0c1c2c3c4c5c6c7c8c9cacbcccdcecfd0d1d2d3d4d5d6d7d8d9dadbdcdddedf" +
				"e0e1e2e3e4e5e6e7e8e9eaebecedeeeff0f1f2f3f4f5f6f7f8f9fafbfcfdfeff" +
				"000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f",
			size: 288,
		},
		{
			name:    "capture",
			payload: types.EncodeEscrowCapture(escrowID, codec.U128{Hi: 0x2021222324252627, Lo: 0x28292a2b2c2d2e2f}, escrowPayloadID(t, "303132333435363738393a3b3c3d3e3f404142434445464748494a4b4c4d4e4f")),
			want: "000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f" +
				"202122232425262728292a2b2c2d2e2f303132333435363738393a3b3c3d3e3f" +
				"404142434445464748494a4b4c4d4e4f",
			size: 80,
		},
		{
			name:    "release",
			payload: types.EncodeEscrowRelease(escrowID, owner),
			want: "000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f" +
				"202122232425262728292a2b2c2d2e2f303132333435363738393a3b3c3d3e3f",
			size: 64,
		},
		{
			name:    "dispute_open",
			payload: types.EncodeEscrowDisputeOpen(escrowID),
			want:    "000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f",
			size:    32,
		},
		{
			name:    "dispute_resolve",
			payload: types.EncodeEscrowDisputeResolve(escrowID, 0x20212223, escrowPayloadID(t, "2425262728292a2b2c2d2e2f303132333435363738393a3b3c3d3e3f40414243")),
			want: "000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f" +
				"202122232425262728292a2b2c2d2e2f303132333435363738393a3b3c3d3e3f" +
				"40414243",
			size: 68,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assertEscrowPayload(t, tc.payload, tc.want, tc.size)
		})
	}
}

func TestEscrowPayloadsIntegerBounds(t *testing.T) {
	const idHex = "0100000000000000000000000000000000000000000000000000000000000000"
	escrowID := [32]byte{1}
	cases := []struct {
		name      string
		amount    codec.U128
		amountHex string
		timestamp uint64
		timeHex   string
		bps       uint32
		bpsHex    string
	}{
		{"zero", codec.U128{}, "00000000000000000000000000000000", 0, "0000000000000000", 0, "00000000"},
		{"low_word", codec.U128{Lo: 1}, "00000000000000000000000000000001", 1, "0000000000000001", 1, "00000001"},
		{"high_word", codec.U128{Hi: 1}, "00000000000000010000000000000000", 0x8000000000000000, "8000000000000000", 3333, "00000d05"},
		{"varied_bytes", codec.U128{Hi: 0x0102030405060708, Lo: 0x1112131415161718}, "01020304050607081112131415161718", 0x0102030405060708, "0102030405060708", 10000, "00002710"},
		{"maximum", codec.U128{Hi: ^uint64(0), Lo: ^uint64(0)}, "ffffffffffffffffffffffffffffffff", ^uint64(0), "ffffffffffffffff", ^uint32(0), "ffffffff"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			open := types.EncodeEscrowOpen(escrowID, escrowID, escrowID, escrowID, escrowID, escrowID, tc.amount, tc.timestamp, tc.timestamp, escrowID, escrowID)
			assertEscrowPayload(t, open, idHex+idHex+idHex+idHex+idHex+idHex+tc.amountHex+tc.timeHex+tc.timeHex+idHex+idHex, 288)
			capture := types.EncodeEscrowCapture(escrowID, tc.amount, escrowID)
			assertEscrowPayload(t, capture, idHex+tc.amountHex+idHex, 80)
			resolve := types.EncodeEscrowDisputeResolve(escrowID, tc.bps, escrowID)
			assertEscrowPayload(t, resolve, idHex+tc.bpsHex+idHex, 68)
		})
	}
}

func TestEscrowPayloadsIndependentBuffers(t *testing.T) {
	escrowID := [32]byte{1}
	key := [32]byte{2}
	cases := []struct {
		name   string
		encode func() []byte
	}{
		{"open", func() []byte {
			return types.EncodeEscrowOpen(escrowID, key, key, key, key, key, codec.U128{Hi: 3, Lo: 4}, 5, 6, key, key)
		}},
		{"capture", func() []byte { return types.EncodeEscrowCapture(escrowID, codec.U128{Hi: 3, Lo: 4}, key) }},
		{"release", func() []byte { return types.EncodeEscrowRelease(escrowID, key) }},
		{"dispute_open", func() []byte { return types.EncodeEscrowDisputeOpen(escrowID) }},
		{"dispute_resolve", func() []byte { return types.EncodeEscrowDisputeResolve(escrowID, 3333, key) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			first := tc.encode()
			snapshot := bytes.Clone(first)
			second := tc.encode()
			for i := range first {
				first[i] ^= 0xff
			}
			if !bytes.Equal(second, snapshot) {
				t.Fatalf("second encoding changed after modifying the first: got %x, want %x", second, snapshot)
			}
			if third := tc.encode(); !bytes.Equal(third, snapshot) {
				t.Fatalf("encoding changed after modifying a returned payload: got %x, want %x", third, snapshot)
			}
			if escrowID != ([32]byte{1}) || key != ([32]byte{2}) {
				t.Fatalf("input arrays changed: escrow id %x, key %x", escrowID, key)
			}
		})
	}
}

func escrowPayloadID(t *testing.T, value string) [32]byte {
	t.Helper()
	raw, err := hex.DecodeString(value)
	if err != nil {
		t.Fatalf("invalid id golden: %v", err)
	}
	if len(raw) != 32 {
		t.Fatalf("id golden length = %d, want 32", len(raw))
	}
	var id [32]byte
	copy(id[:], raw)
	return id
}

func assertEscrowPayload(t *testing.T, got []byte, wantHex string, size int) {
	t.Helper()
	want, err := hex.DecodeString(wantHex)
	if err != nil {
		t.Fatalf("invalid payload golden: %v", err)
	}
	if len(want) != size {
		t.Fatalf("golden length = %d, want %d", len(want), size)
	}
	if len(got) != size {
		t.Fatalf("payload length = %d, want %d", len(got), size)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("payload = %x, want %x", got, want)
	}
}
