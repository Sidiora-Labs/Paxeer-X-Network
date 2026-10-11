package types

import (
	"bytes"
	"math"
	"testing"
)

func TestParamsDefaultAndValidate(t *testing.T) {
	params := DefaultParams()
	if params.Paused {
		t.Fatal("default params are paused")
	}
	if params.IdempotencyRetention != 0 {
		t.Fatalf("default retention = %d, want 0", params.IdempotencyRetention)
	}
	if err := params.Validate(); err != nil {
		t.Fatalf("default params: %v", err)
	}
	for _, p := range []Params{
		{Paused: true},
		{IdempotencyRetention: 1},
		{Paused: true, IdempotencyRetention: math.MaxUint64},
	} {
		if err := p.Validate(); err != nil {
			t.Fatalf("params %+v: %v", p, err)
		}
	}
}

func TestParamsStoreKeyLayout(t *testing.T) {
	if ModuleName != "layerxauth" || StoreKey != ModuleName || RouterKey != ModuleName || QuerierRoute != ModuleName {
		t.Fatal("module names differ from layerxauth")
	}
	if !bytes.Equal(ParamsKey, []byte{0x01}) || !bytes.Equal(IdempotencyPrefix, []byte{0x02}) || !bytes.Equal(IdempotencyBySequencePrefix, []byte{0x03}) {
		t.Fatal("store prefixes differ from 0x01, 0x02, 0x03")
	}
	var hash [32]byte
	for i := range hash {
		hash[i] = byte(i + 1)
	}
	wantKey := append([]byte{0x02}, hash[:]...)
	if got := IdempotencyKey(hash); !bytes.Equal(got, wantKey) {
		t.Fatalf("IdempotencyKey = %x, want %x", got, wantKey)
	}
	wantSeq := append([]byte{0x03, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08}, hash[:]...)
	if got := IdempotencyBySequenceKey(0x0102030405060708, hash); !bytes.Equal(got, wantSeq) {
		t.Fatalf("IdempotencyBySequenceKey = %x, want %x", got, wantSeq)
	}
	if bytes.Compare(IdempotencyBySequenceKey(255, hash), IdempotencyBySequenceKey(256, [32]byte{})) >= 0 {
		t.Fatal("sequence index does not order by recorded sequence")
	}
}
