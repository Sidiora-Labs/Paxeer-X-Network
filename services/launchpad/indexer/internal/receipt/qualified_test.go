package receipt

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/Sidiora-Technologies/KindleLaunch/core/indexer/internal/store"
	"github.com/ethereum/go-ethereum/common"
	types "github.com/ethereum/go-ethereum/core/types"
)

func TestUnissuedReceiptCannotAuthorizeProjection(t *testing.T) {
	ctx := context.Background()
	for _, q := range []*QualifiedReceipt{nil, {}, {issuer: &issuance{}}} {
		if _, err := q.Fills(ctx); !errors.Is(err, ErrQualification) {
			t.Fatalf("unissued fills: %v", err)
		}
		if _, err := q.Events(ctx); !errors.Is(err, ErrQualification) {
			t.Fatalf("unissued events: %v", err)
		}
		if _, _, _, err := q.SourceContext(ctx); !errors.Is(err, ErrQualification) {
			t.Fatalf("unissued context: %v", err)
		}
		if err := ValidateBindings(ctx, store.CanonicalBatch{ChainID: 125}, []*QualifiedReceipt{q}); !errors.Is(err, ErrQualification) {
			t.Fatalf("unissued binding: %v", err)
		}
	}
	if _, err := Qualify(ctx, nil, nil, Bundle{}); !errors.Is(err, ErrQualification) {
		t.Fatalf("missing authority admitted: %v", err)
	}
	ctx, cancel := context.WithCancel(ctx)
	cancel()
	if err := ValidateBindings(ctx, store.CanonicalBatch{ChainID: 125}, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation: %v", err)
	}
}

func TestCanonicalRecordRequiresQualifiedReceipt(t *testing.T) {
	b := store.CanonicalBatch{ChainID: 125, Records: []store.RawRecord{{TransactionHash: "0x" + strings.Repeat("1", 64)}}}
	if err := ValidateBindings(context.Background(), b, nil); !errors.Is(err, ErrBinding) {
		t.Fatalf("record without issuer admitted: %v", err)
	}
}

func genuineGoldenLog(t *testing.T) *types.Log {
	t.Helper()
	raw, err := os.ReadFile("../../../../spec/production-system/integration/contracts/v1/chain/golden-fixtures.json")
	if err != nil {
		t.Fatal(err)
	}
	var golden struct {
		ChainID        uint64 `json:"chainId"`
		ReplayVerified bool   `json:"replayVerified"`
		Receipts       []struct {
			Label string `json:"label"`
			Logs  []struct {
				Address, BlockHash, TransactionHash, Data string
				BlockNumber                               uint64
				TransactionIndex, Index                   uint
				Topics                                    []string
			} `json:"logs"`
		} `json:"receipts"`
	}
	if err = json.Unmarshal(raw, &golden); err != nil {
		t.Fatal(err)
	}
	if golden.ChainID != 125 || !golden.ReplayVerified {
		t.Fatal("genuine archived fixture identity absent")
	}
	for _, r := range golden.Receipts {
		if r.Label != "buy" || len(r.Logs) == 0 {
			continue
		}
		l := r.Logs[0]
		topics := make([]common.Hash, len(l.Topics))
		for i, s := range l.Topics {
			topics[i] = common.HexToHash(s)
		}
		return &types.Log{Address: common.HexToAddress(l.Address), BlockHash: common.HexToHash(l.BlockHash), TxHash: common.HexToHash(l.TransactionHash), Data: common.FromHex(l.Data), Topics: topics, BlockNumber: l.BlockNumber, TxIndex: l.TransactionIndex, Index: l.Index}
	}
	t.Fatal("archived actual buy receipt absent")
	return nil
}

func TestGenuineReceiptLogBindingRejectsEveryChangedCoordinate(t *testing.T) {
	l := genuineGoldenLog(t)
	topics := make([]string, len(l.Topics))
	for i, h := range l.Topics {
		topics[i] = strings.ToLower(h.Hex())
	}
	raw := store.RawRecord{TransactionHash: strings.ToLower(l.TxHash.Hex()), BlockHash: strings.ToLower(l.BlockHash.Hex()), Emitter: strings.ToLower(l.Address.Hex()), Height: int64(l.BlockNumber), TransactionIndex: uint32(l.TxIndex), LogIndex: uint32(l.Index), Topics: topics, Data: append([]byte(nil), l.Data...)}
	if !matches(raw, l) {
		t.Fatal("actual frozen log did not bind byte-for-byte")
	}
	cases := map[string]func(*store.RawRecord){
		"transaction":       func(r *store.RawRecord) { r.TransactionHash = "0x" + strings.Repeat("f", 64) },
		"block":             func(r *store.RawRecord) { r.BlockHash = "0x" + strings.Repeat("f", 64) },
		"emitter":           func(r *store.RawRecord) { r.Emitter = "0x" + strings.Repeat("f", 40) },
		"height":            func(r *store.RawRecord) { r.Height++ },
		"negative-height":   func(r *store.RawRecord) { r.Height = -1 },
		"transaction-index": func(r *store.RawRecord) { r.TransactionIndex++ },
		"log-index":         func(r *store.RawRecord) { r.LogIndex++ },
		"topic-order":       func(r *store.RawRecord) { r.Topics[0], r.Topics[1] = r.Topics[1], r.Topics[0] },
		"topic-count":       func(r *store.RawRecord) { r.Topics = r.Topics[:1] },
		"payload":           func(r *store.RawRecord) { r.Data[0] ^= 1 },
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			candidate := raw
			candidate.Topics = append([]string(nil), raw.Topics...)
			candidate.Data = append([]byte(nil), raw.Data...)
			change(&candidate)
			if matches(candidate, l) {
				t.Fatal("changed immutable evidence accepted")
			}
		})
	}
	if !matches(raw, l) {
		t.Fatal("caller mutation changed archived evidence")
	}
}
