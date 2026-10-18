package sourcebootstrap_test

import (
	"testing"

	"github.com/Sidiora-Technologies/KindleLaunch/shared/accounting"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/accounting/sourcebootstrap"
)

// The golden digests were produced by the stats (consumer/accounting.go) and
// charts (consumer/source_bootstrap.go) inline tuples before they moved here;
// every checkpoint already stored in delivery_partition_offsets depends on
// these bytes staying identical.
func TestCheckpointTuplesAreFrozen(t *testing.T) {
	pool := "0xce20da50f92d21d8bee497910c782e9aa25b6263"
	publication := accounting.HashTuple("fixture-publication", pool)
	payload := accounting.Digest(accounting.HashTuple("fixture-payload", pool))
	hash := "0x81a18d8200000000000000000000000000000000000000000000000000192b"
	stats := sourcebootstrap.StatsEvidence{SourceStream: "chain125-canonical", Pool: pool, ProjectionVersion: "accounting-market-v1", PublicationID: publication, PayloadDigest: payload, InitialPreviousHeight: 32226214, FirstCommitID: "trades", FirstCommitLastHeight: 33963761, FirstCommitBlockHash: hash}
	charts := sourcebootstrap.ChartsEvidence{SourceStream: "chain125-canonical", Pool: pool, ProjectionVersion: "accounting-market-v1", PublicationID: publication, PayloadDigest: payload, ReplayFromHeight: 33963700, CreationTransactionHash: "0xe200486fb275ad363101dd09455b23842f1a002b839062732230fd4c908cbfca", CreationLogIndex: 7, FirstCommitID: "trades", FirstCommitLastHeight: 33963761, FirstCommitBlockHash: hash}
	if got := stats.Checkpoint(); got != "892018775ef3aeab5a632f8c4bc7c4574609cbee92961ef803570ddeb7073d89" {
		t.Fatal("stats checkpoint tuple changed", got)
	}
	if got := charts.Checkpoint(); got != "d887ad24c516574788b95d883f7fd1d9be08029aca0faa122ea41a2f101993ba" {
		t.Fatal("charts checkpoint tuple changed", got)
	}
	other := stats
	other.Pool = "0x2222222222222222222222222222222222222222"
	if other.Checkpoint() == stats.Checkpoint() {
		t.Fatal("checkpoint is not bound to the pool")
	}
}
