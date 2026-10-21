package holdings

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/accounting"
	"os"
	"strconv"
	"testing"
)

func TestPersistedCheckpointIndependentDigest(t *testing.T) {
	ctx, r := phase2Repository(t)
	token, a, b := addr(1), addr(2), addr(3)
	if err := r.Discover(ctx, token, 6, "10", true); err != nil {
		t.Fatal(err)
	}
	for _, e := range []Transfer{event(token, Zero, a, "100", 0), event(token, a, b, "25", 1), event(token, b, Zero, "5", 2)} {
		applyFixture(t, ctx, r, e)
	}
	checkpoint, err := r.Checkpoint(ctx, token, "10", fmt.Sprintf("0x%064x", 100), "holdings_v1")
	if err != nil {
		t.Fatal(err)
	}
	independent := sha256.Sum256([]byte("holdings_v1\n125\n" + token + "\n95\n10\n" + a + ":75\n" + b + ":20\n"))
	if checkpoint.Digest != hex.EncodeToString(independent[:]) || checkpoint.Supply != "95" || checkpoint.Revision != 3 {
		t.Fatal(checkpoint)
	}
	var persisted string
	if err = r.pool.QueryRow(ctx, "SELECT digest FROM "+r.schema+".checkpoints WHERE token=$1", token).Scan(&persisted); err != nil || persisted != checkpoint.Digest {
		t.Fatal(persisted, err)
	}
}
func TestRetainedCanonicalReplay(t *testing.T) {
	ctx, r := phase2Repository(t)
	token, start, end, stream, want := os.Getenv("L14_ARCHIVE_TOKEN"), os.Getenv("L14_ARCHIVE_START"), os.Getenv("L14_ARCHIVE_END"), os.Getenv("L14_ARCHIVE_STREAM"), os.Getenv("L14_ARCHIVE_EXPECTED_DIGEST")
	decimals, err := strconv.ParseUint(os.Getenv("L14_ARCHIVE_DECIMALS"), 10, 8)
	if err != nil || token == "" || start == "" || end == "" || stream == "" || len(want) != 64 {
		t.Fatal("real retained canonical indexer fixture and independently computed L14_ARCHIVE_* parameters required")
	}
	checkpoint, err := r.RebuildFromCanonical(ctx, r.pool, stream, token, uint8(decimals), start, end, "holdings_v1")
	if err != nil || checkpoint.Digest != want {
		t.Fatal(checkpoint, err)
	}
}

func TestCanonicalCatchupMatchesIndependentArchiveDigest(t *testing.T) {
	ctx, r := phase2Repository(t)
	token, stream, end, want := os.Getenv("L14_ARCHIVE_TOKEN"), os.Getenv("L14_ARCHIVE_STREAM"), os.Getenv("L14_ARCHIVE_END"), os.Getenv("L14_ARCHIVE_EXPECTED_DIGEST")
	if token == "" || stream == "" || end == "" || len(want) != 64 {
		t.Fatal("real canonical creation, typed Transfer, raw completeness and independent archive fixture required")
	}
	if err := r.DiscoverCanonical(ctx, token, stream); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 10000; i++ {
		batch, err := r.CatchUpCanonicalAt(ctx, token, stream, end, 1000)
		if err != nil || batch.Incident {
			t.Fatal(batch, err)
		}
		if batch.More {
			continue
		}
		if batch.Coverage != "complete" {
			t.Fatal(batch)
		}
		var blockHash string
		if err = r.pool.QueryRow(ctx, "SELECT block_hash FROM indexer.indexer_block_completeness_v1 WHERE chain_id=125 AND stream_id=$1 AND height=$2::bigint", stream, end).Scan(&blockHash); err != nil {
			t.Fatal(err)
		}
		checkpoint, err := r.Checkpoint(ctx, token, end, blockHash, "holdings_v1")
		if err != nil || checkpoint.Digest != want {
			t.Fatal(checkpoint, err)
		}
		return
	}
	t.Fatal("canonical fixture exceeded ten million admitted Transfers")
}

func TestCanonicalActorProvenance(t *testing.T) {
	ctx, r := phase2Repository(t)
	token, stream, end := os.Getenv("L14_ARCHIVE_TOKEN"), os.Getenv("L14_ARCHIVE_STREAM"), os.Getenv("L14_ARCHIVE_END")
	fillID, transaction, logIndex, want := os.Getenv("L14_ACTOR_FILL_ID"), os.Getenv("L14_ACTOR_TRANSACTION_HASH"), os.Getenv("L14_ACTOR_TRANSFER_LOG_INDEX"), os.Getenv("L14_ACTOR_EXPECTED_ADDRESS")
	if token == "" || stream == "" || end == "" || fillID == "" || transaction == "" || logIndex == "" || want == "" {
		t.Fatal("real matching canonical Fill/Transfer and independent actor expectation required")
	}
	if err := r.DiscoverCanonical(ctx, token, stream); err != nil {
		t.Fatal(err)
	}
	caught := false
	for i := 0; i < 10000; i++ {
		batch, err := r.CatchUpCanonicalAt(ctx, token, stream, end, 1000)
		if err != nil || batch.Incident {
			t.Fatal(batch, err)
		}
		if !batch.More {
			caught = true
			break
		}
	}
	if !caught {
		t.Fatal("canonical fixture exceeded ten million Transfers")
	}
	e := Transfer{ChainID: "125"}
	if err := r.pool.QueryRow(ctx, "SELECT token,from_address,to_address,amount_raw::text,height::text,transaction_index::text,log_index::text,transaction_hash,block_hash FROM indexer.indexer_token_transfers_v1 WHERE chain_id=125 AND token=$1 AND transaction_hash=$2 AND log_index=$3::bigint", token, transaction, logIndex).Scan(&e.Token, &e.From, &e.To, &e.Amount, &e.Height, &e.TransactionIndex, &e.LogIndex, &e.TransactionHash, &e.BlockHash); err != nil {
		t.Fatal(err)
	}
	var payload []byte
	var fill accounting.FillV1
	if err := r.pool.QueryRow(ctx, "SELECT payload FROM indexer.indexer_accounting_fills_v1 WHERE source_fill_id=$1", fillID).Scan(&payload); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(payload, &fill); err != nil {
		t.Fatal(err)
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = r.EnrichCanonicalActorTx(ctx, tx, e, fill, stream); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	var provenance Provenance
	if err = r.pool.QueryRow(ctx, "SELECT payload FROM "+r.schema+".provenance WHERE identity=$1", e.Identity()).Scan(&payload); err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(payload, &provenance); err != nil || provenance.Actor == nil || *provenance.Actor != want || provenance.Recipient != e.To || provenance.ActorSource != ActorChainEvent || provenance.RecipientSource != RecipientTransfer {
		t.Fatal(provenance, err)
	}
	before, err := r.Snapshot(ctx, token, 100)
	if err != nil {
		t.Fatal(err)
	}
	mismatch := e
	mismatch.BlockHash = fmt.Sprintf("0x%064x", 999999)
	tx, err = r.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = r.EnrichCanonicalActorTx(ctx, tx, mismatch, fill, stream); !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
	if err = tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	after, err := r.Snapshot(ctx, token, 100)
	if err != nil || after.Revision != before.Revision || after.Supply != before.Supply {
		t.Fatal(after, err)
	}
}

func TestCanonicalArchiveReplayExactlyOnceAndRejectsConflicts(t *testing.T) {
	ctx, r := phase2Repository(t)
	token, stream, end := os.Getenv("L14_ARCHIVE_TOKEN"), os.Getenv("L14_ARCHIVE_STREAM"), os.Getenv("L14_ARCHIVE_END")
	if token == "" || stream == "" || end == "" {
		t.Fatal("real retained canonical L14_ARCHIVE_* fixture required")
	}
	if err := r.DiscoverCanonical(ctx, token, stream); err != nil {
		t.Fatal(err)
	}
	complete := false
	for batch := 0; batch < 10000; batch++ {
		out, err := r.CatchUpCanonicalAt(ctx, token, stream, end, 1000)
		if err != nil || out.Incident {
			t.Fatal(out, err)
		}
		if !out.More {
			complete = true
			break
		}
		if out.Coverage != "incomplete" {
			t.Fatal("partial catch-up retained complete coverage", out)
		}
	}
	if !complete {
		t.Fatal("canonical fixture exceeded bounded catch-up")
	}
	before, err := r.Snapshot(ctx, token, 100)
	if err != nil || before.Coverage != "complete" {
		t.Fatal(before, err)
	}
	var expected, actual int64
	if err = r.pool.QueryRow(ctx, "SELECT count(*) FROM indexer.indexer_token_transfers_v1 WHERE chain_id=125 AND token=$1 AND height<=$2::bigint", token, end).Scan(&expected); err != nil {
		t.Fatal(err)
	}
	if err = r.pool.QueryRow(ctx, "SELECT count(*) FROM "+r.schema+".transfers WHERE token=$1", token).Scan(&actual); err != nil || expected != actual || actual == 0 {
		t.Fatal(expected, actual, err)
	}
	var afterRevision int64
	for {
		rows, err := r.pool.Query(ctx, "SELECT revision,event FROM "+r.schema+".transfers WHERE token=$1 AND revision>$2 ORDER BY revision LIMIT 1000", token, afterRevision)
		if err != nil {
			t.Fatal(err)
		}
		type retainedEvent struct {
			revision int64
			event    Transfer
		}
		events := make([]retainedEvent, 0, 1000)
		for rows.Next() {
			var raw []byte
			var e retainedEvent
			if err = rows.Scan(&e.revision, &raw); err != nil {
				rows.Close()
				t.Fatal(err)
			}
			if err = json.Unmarshal(raw, &e.event); err != nil {
				rows.Close()
				t.Fatal(err)
			}
			events = append(events, e)
		}
		rows.Close()
		if err = rows.Err(); err != nil {
			t.Fatal(err)
		}
		if len(events) == 0 {
			break
		}
		for _, retained := range events {
			e := retained.event
			result, err := r.ApplyCanonicalTransfer(ctx, e, stream)
			if err != nil || !result.Duplicate {
				t.Fatal(result, err)
			}
			changed := e
			changed.Amount = "0"
			if e.Amount == "0" {
				changed.Amount = "1"
			}
			if _, err = r.ApplyCanonicalTransfer(ctx, changed, stream); err == nil {
				t.Fatal("conflicting canonical amount accepted", changed)
			}
			changed = e
			changed.BlockHash = fmt.Sprintf("0x%064x", 999999)
			if _, err = r.ApplyCanonicalTransfer(ctx, changed, stream); err == nil {
				t.Fatal("conflicting canonical block accepted", changed)
			}
			afterRevision = retained.revision
		}
	}
	after, err := r.Snapshot(ctx, token, 100)
	beforeJSON, marshalErr := json.Marshal(before)
	if marshalErr != nil {
		t.Fatal(marshalErr)
	}
	afterJSON, marshalErr := json.Marshal(after)
	if err != nil || marshalErr != nil || string(beforeJSON) != string(afterJSON) {
		t.Fatal("canonical replay changed projection", before, after, err, marshalErr)
	}
}
