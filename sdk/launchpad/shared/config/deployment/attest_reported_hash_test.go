package deployment

import (
	"context"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

// reportedHashReader models a node whose consensus block hash differs from the
// go-ethereum header hash (CometBFT-backed EVMs). The node identifies the block
// by `reported`; the locally recomputed header hash is `actual`. Every pinned
// read addressed by the reported hash is served at the actual block.
type reportedHashReader struct {
	Reader
	reported common.Hash
	actual   common.Hash
	vouch    bool
}

func (r reportedHashReader) translate(h common.Hash) common.Hash {
	if h == r.reported {
		return r.actual
	}
	return h
}

func (r reportedHashReader) HeaderByHash(ctx context.Context, h common.Hash) (*types.Header, error) {
	return r.Reader.HeaderByHash(ctx, r.translate(h))
}

func (r reportedHashReader) CodeAtHash(ctx context.Context, a common.Address, h common.Hash) ([]byte, error) {
	return r.Reader.CodeAtHash(ctx, a, r.translate(h))
}

func (r reportedHashReader) StorageAtHash(ctx context.Context, a common.Address, k, h common.Hash) ([]byte, error) {
	return r.Reader.StorageAtHash(ctx, a, k, r.translate(h))
}

func (r reportedHashReader) CallContractAtHash(ctx context.Context, msg ethereum.CallMsg, h common.Hash) ([]byte, error) {
	return r.Reader.CallContractAtHash(ctx, msg, r.translate(h))
}

// vouchingReader additionally implements ReportedHeaderReader: the node vouches
// for the header behind the reported hash.
type vouchingReader struct{ reportedHashReader }

func (r vouchingReader) HeaderByReportedHash(ctx context.Context, h common.Hash) (*types.Header, error) {
	if h != r.reported {
		return nil, ethereum.NotFound
	}
	return r.Reader.HeaderByHash(ctx, r.actual)
}

func TestAttestAcceptsNodeReportedBlockHashOnlyWhenVouched(t *testing.T) {
	m, r, actual, p := newEVM(t, nil, 125)
	reported := common.HexToHash("0xc0de000000000000000000000000000000000000000000000000000000000125")
	if reported == actual {
		t.Fatal("fixture collision")
	}
	base := reportedHashReader{Reader: r, reported: reported, actual: actual}

	t.Run("plain_reader_cannot_verify_consensus_hash", func(t *testing.T) {
		_, e := Attest(context.Background(), base, m, reported, p)
		if e == nil || !strings.Contains(e.Error(), "block_identity_unverified") {
			t.Fatalf("got %v", e)
		}
	})
	t.Run("vouching_reader_attests_at_reported_hash", func(t *testing.T) {
		a, e := Attest(context.Background(), vouchingReader{base}, m, reported, p)
		if e != nil {
			t.Fatal(e)
		}
		if !a.Eligible {
			t.Fatalf("not eligible: %+v", a.Issues)
		}
		if a.BlockHash != reported.Hex() {
			t.Fatalf("attestation pinned to %s, want reported %s", a.BlockHash, reported.Hex())
		}
		if !a.Identities["FixturePool"].Verified || a.CreationFeeRaw != "1000000" {
			t.Fatalf("reads not served at the reported block: %+v", a)
		}
		if e = a.ValidateFor(m, p); e != nil {
			t.Fatal(e)
		}
	})
	t.Run("vouching_reader_still_verifies_unaliased_hash", func(t *testing.T) {
		a, e := Attest(context.Background(), vouchingReader{base}, m, actual, p)
		if e == nil || !strings.Contains(e.Error(), "block_identity_unverified") {
			t.Fatalf("unvouched hash accepted: %+v %v", a, e)
		}
	})
	t.Run("zero_hash_rejected", func(t *testing.T) {
		_, e := Attest(context.Background(), vouchingReader{base}, m, common.Hash{}, p)
		if e == nil || !strings.Contains(e.Error(), "identity_unverified") {
			t.Fatalf("got %v", e)
		}
	})
}
