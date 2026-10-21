package receipt

import (
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

// A block bloom is relevant exactly when it may contain a gated address: the
// filter never yields a false negative, and an unrelated bloom is rejected.
func TestBloomGateRelevance(t *testing.T) {
	pool := common.HexToAddress("0xCE20Da50F92D21d8BeE497910c782E9Aa25b6263")
	other := common.HexToAddress("0x0000000000000000000000000000000000000abc")
	a := &RPCQualification{options: RPCOptions{Epochs: []RPCCodeEpoch{}}}
	a.options.Epochs = append(a.options.Epochs, RPCCodeEpoch{})
	a.options.Epochs[0].Deployment.Address = pool
	gate := a.bloomGate()
	var withPool, withOther, empty types.Bloom
	withPool.Add(pool.Bytes())
	withPool.Add(other.Bytes())
	withOther.Add(other.Bytes())
	if !gate.relevant(withPool) {
		t.Fatal("bloom containing a gated address judged irrelevant")
	}
	if gate.relevant(withOther) || gate.relevant(empty) {
		t.Fatal("bloom without a gated address judged relevant")
	}
	if (bloomGate{}).relevant(withPool) {
		t.Fatal("empty gate admitted a block")
	}
}

func TestArchiveOptionsBounds(t *testing.T) {
	o, err := ArchiveOptions{}.normalized()
	if err != nil || o.Concurrency != DefaultArchiveConcurrency || o.AttemptTimeout != DefaultArchiveAttempt {
		t.Fatalf("defaults %+v %v", o, err)
	}
	for _, bad := range []ArchiveOptions{{Concurrency: MaxArchiveConcurrency + 1}, {Concurrency: -1}, {AttemptTimeout: 100}} {
		if _, err = bad.normalized(); err == nil {
			t.Fatalf("accepted %+v", bad)
		}
	}
	q := &RPCQualification{reads: &rpcReadState{}}
	if err = q.ConfigureArchiveReads(32, -1); err != nil || q.reads.rangeConcurrency() != 32 || q.reads.readaheadDepth() != 0 {
		t.Fatalf("configure: %v", err)
	}
	if err = q.ConfigureArchiveReads(0, MaxArchiveReadahead+1); err == nil {
		t.Fatal("oversized readahead accepted")
	}
}
