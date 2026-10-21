package search

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/delivery"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/delivery/natsdriver"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestProjectionMonotonicRevisionAndDeletion(t *testing.T) {
	pool := integratedDatabase(t)
	d := seedIndexed(t, pool, 1)
	d.Name, d.NameKey, d.MetadataRevision = "Newest", "newest", 9
	d.ProfileWallet, d.ProfileLabel = fixtureAddress(777), "Public bio"
	applyFixture(t, pool, Change{Version: 2, Kind: "metadata", Revision: 9, PoolAddress: d.PoolAddress, Document: &d})
	d.Name, d.NameKey, d.MetadataRevision = "Older", "older", 8
	if applyFixture(t, pool, Change{Version: 2, Kind: "metadata", Revision: 8, PoolAddress: d.PoolAddress, Document: &d}) {
		t.Fatal("stale metadata modified projection")
	}
	for i := 0; i < 100; i++ {
		if applyFixture(t, pool, Change{Version: 2, Kind: "metadata", Revision: 8, PoolAddress: d.PoolAddress, Document: &d}) {
			t.Fatal("duplicate effect")
		}
	}
	applyFixture(t, pool, Change{Version: 2, Kind: "profile_deletion", Revision: 10, ProfileWallet: fixtureAddress(777)})
	d.Name, d.NameKey, d.MetadataRevision = "Latest", "latest", 11
	applyFixture(t, pool, Change{Version: 2, Kind: "metadata", Revision: 11, PoolAddress: d.PoolAddress, Document: &d})
	var raw []byte
	if err := pool.QueryRow(context.Background(), "SELECT payload FROM stats_search.source_documents_v2 WHERE pool_address=$1", d.PoolAddress).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var actual IndexedDocument
	if json.Unmarshal(raw, &actual) != nil || actual.Name != "Latest" || actual.ProfileLabel != "" || actual.ProfileWallet != "" {
		t.Fatal(string(raw))
	}
	applyFixture(t, pool, Change{Version: 2, Kind: "visibility", Revision: 12, PoolAddress: d.PoolAddress, Public: false, Tombstone: true})
	p, err := (Store{Pool: pool}).SearchIntegrated(context.Background(), IntegratedRequest{Request: Request{Query: d.PoolAddress, Kind: "pool", Limit: 25}, Sort: "address"})
	if err != nil || len(p.Items) != 0 || p.Status != "not-indexed" {
		t.Fatal(p, err)
	}
}
func TestProjectionPreferenceIsolationStorage(t *testing.T) {
	pool := integratedDatabase(t)
	a := PreferenceSnapshot{AccountID: uuid.NewString(), Wallet: fixtureAddress(701), Revision: 1, MutedCreators: []string{fixtureAddress(101)}, FollowedCreators: []string{}, SavedPools: []string{}}
	b := PreferenceSnapshot{AccountID: uuid.NewString(), Wallet: fixtureAddress(702), Revision: 1, MutedCreators: []string{}, FollowedCreators: []string{fixtureAddress(101)}, SavedPools: []string{}}
	applyFixture(t, pool, Change{Version: 2, Kind: "preferences", Revision: 1, Preferences: &a})
	applyFixture(t, pool, Change{Version: 2, Kind: "preferences", Revision: 1, Preferences: &b})
	var count int
	if err := pool.QueryRow(context.Background(), "SELECT count(*) FROM stats_search.preferences_v2 WHERE wallet=$1 AND payload->>'accountId'=$2", a.Wallet, b.AccountID).Scan(&count); err != nil || count != 0 {
		t.Fatal(count, err)
	}
}

func TestRealBrokerInboxProjectionAndOutboxAtomicity(t *testing.T) {
	brokerURL := os.Getenv("L20_NATS_URL")
	if brokerURL == "" {
		t.Fatal("L20_NATS_URL required for real isolated file-backed NATS qualification")
	}
	pool := integratedDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	sourceDDL, err := os.ReadFile("../../../../spec/production-system/integration/migrations/190001.sql")
	if err != nil {
		t.Fatal(err)
	}
	creationDDL := strings.Split(string(sourceDDL), "CREATE TABLE indexer.indexer_token_transfers")[0]
	if _, err = pool.Exec(ctx, "CREATE SCHEMA indexer;"+creationDDL+"CREATE SCHEMA search_delivery;"); err != nil {
		t.Fatal(err)
	}
	ddl, err := delivery.SchemaDDL("search_delivery")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, ddl); err != nil {
		t.Fatal(err)
	}
	nonce := strings.ReplaceAll(uuid.NewString(), "-", "")
	subject := "search_test_" + nonce + ".creation"
	invalidate := "search_test_" + nonce + ".invalidate"
	stream := "SEARCH_" + nonce
	policy := delivery.Policy{Subjects: map[string]bool{subject: true, invalidate: true}, EventTypes: map[string]bool{"canonical_creation": true, "search_invalidation": true}}
	inbox, err := delivery.NewStore(pool, "search_delivery", policy)
	if err != nil {
		t.Fatal(err)
	}
	nc, err := nats.Connect(brokerURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(nc.Close)
	js, err := jetstream.New(nc)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = js.CreateStream(ctx, jetstream.StreamConfig{Name: stream, Subjects: []string{subject, invalidate}, Storage: jetstream.FileStorage, MaxBytes: 10485760, MaxAge: time.Minute}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := js.DeleteStream(cleanup, stream); err != nil {
			t.Error(err)
		}
	})
	config := natsdriver.ConsumerConfig{Stream: stream, Durable: "search", Subject: subject, Batch: 1, MaxAckPending: 1, MaxWaiting: 1, MaxDeliver: 2, FetchWait: time.Second, AckWait: time.Second}
	nativeConfig, err := config.BrokerConsumerConfig()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = js.CreateOrUpdateConsumer(ctx, stream, nativeConfig); err != nil {
		t.Fatal(err)
	}
	consumer, err := natsdriver.NewConsumer(ctx, nc, config)
	if err != nil {
		t.Fatal(err)
	}
	defer consumer.Close()
	publisher, err := natsdriver.NewPublisher(nc, stream)
	if err != nil {
		t.Fatal(err)
	}
	fact := map[string]any{"token": fixtureAddress(99), "pool": fixtureAddress(1), "creator": fixtureAddress(101), "factory": fixtureAddress(901), "optical": fixtureAddress(902), "nftId": "1", "blockTimestampSeconds": "1700000000", "position": map[string]string{"chainId": "125", "height": "1", "transactionIndex": "0", "logIndex": "0", "transactionHash": "0x" + strings.Repeat("2", 64), "blockHash": "0x" + strings.Repeat("1", 64)}, "metadata": map[string]string{"name": "Canonical name", "symbol": "COIN"}, "provenance": map[string]string{}}
	payload, err := json.Marshal(fact)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO indexer.indexer_canonical_creations VALUES($1,$2,$3,$4,$5,1,1,$6,$7,0,0,1700000000,'{}',$8)`, fixtureAddress(99), fixtureAddress(1), fixtureAddress(101), fixtureAddress(901), fixtureAddress(902), "0x"+strings.Repeat("1", 64), "0x"+strings.Repeat("2", 64), payload); err != nil {
		t.Fatal(err)
	}
	event := delivery.Envelope{EventID: "creation_fixture", EventType: "canonical_creation", SchemaVersion: 1, ChainID: 125, SourceOrder: delivery.SourceOrder{Height: "1", TransactionIndex: "0", LogIndex: "0"}, EmittedAt: time.Now().UTC(), TraceID: "fixture_trace", PayloadHash: delivery.Hash(payload), Payload: payload}
	wire, err := event.Encode(policy, subject)
	if err != nil {
		t.Fatal(err)
	}
	projection := Projection{Delivery: inbox, ConsumerID: "search", InboxGeneration: "generation1", InvalidationSubject: invalidate, InvalidationEventType: "search_invalidation"}
	for i := 0; i < 100; i++ {
		if _, err = publisher.Publish(ctx, subject, fmt.Sprintf("transport_copy_%d", i), wire); err != nil {
			t.Fatal(err)
		}
		messages, err := consumer.Fetch(ctx)
		if err != nil || len(messages) != 1 {
			t.Fatal(len(messages), err)
		}
		if err = projection.ConsumeCanonicalCreation(ctx, subject, messages[0].Wire, messages[0]); err != nil {
			t.Fatal(err)
		}
	}
	var effects, inboxRows, outboxRows int
	if err = pool.QueryRow(ctx, "SELECT (SELECT count(*) FROM stats_search.source_documents_v2),(SELECT count(*) FROM search_delivery.delivery_inbox),(SELECT count(*) FROM search_delivery.delivery_outbox)").Scan(&effects, &inboxRows, &outboxRows); err != nil || effects != 1 || inboxRows != 1 || outboxRows != 1 {
		t.Fatal(effects, inboxRows, outboxRows, err)
	}
	fact["metadata"] = map[string]string{"name": "Forged canonical name", "symbol": "COIN"}
	forgedPayload, err := json.Marshal(fact)
	if err != nil {
		t.Fatal(err)
	}
	event.EventID = "forged_creation_fixture"
	event.Payload = forgedPayload
	event.PayloadHash = delivery.Hash(forgedPayload)
	forgedWire, err := event.Encode(policy, subject)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = publisher.Publish(ctx, subject, "forged_transport", forgedWire); err != nil {
		t.Fatal(err)
	}
	messages, err := consumer.Fetch(ctx)
	if err != nil || len(messages) != 1 {
		t.Fatal(len(messages), err)
	}
	if err = projection.ConsumeCanonicalCreation(ctx, subject, messages[0].Wire, messages[0]); !errors.Is(err, delivery.ErrIntegrity) {
		t.Fatal("unauthorized source changed canonical projection", err)
	}
	if err = pool.QueryRow(ctx, "SELECT count(*) FROM search_delivery.delivery_outbox").Scan(&outboxRows); err != nil || outboxRows != 1 {
		t.Fatal("rejected event emitted an invalidation", outboxRows, err)
	}
}

func TestReorderedDeletionAndMetadataCannotResurrect(t *testing.T) {
	pool := integratedDatabase(t)
	ctx := context.Background()
	for _, deletionFirst := range []bool{false, true} {
		number := 20
		if deletionFirst {
			number = 21
		}
		d := seedIndexed(t, pool, number)
		wallet := fixtureAddress(number + 700)
		if _, err := pool.Exec(ctx, `INSERT INTO stats_search.profile_documents_v2 VALUES($1,'Public profile','public profile',9,clock_timestamp())`, wallet); err != nil {
			t.Fatal(err)
		}
		deletion := Change{Version: 2, Kind: "profile_deletion", Revision: 10, ProfileWallet: wallet}
		if deletionFirst {
			applyFixture(t, pool, deletion)
		}
		d.Name, d.NameKey, d.MetadataRevision = "Latest profile", "latest profile", 9
		d.ProfileWallet, d.ProfileLabel = wallet, "Public profile"
		metadata := Change{Version: 2, Kind: "metadata", Revision: 9, PoolAddress: d.PoolAddress, Document: &d}
		applyFixture(t, pool, metadata)
		if !deletionFirst {
			applyFixture(t, pool, deletion)
		}
		for duplicate := 0; duplicate < 100; duplicate++ {
			if applyFixture(t, pool, metadata) || applyFixture(t, pool, deletion) {
				t.Fatal("duplicate revision changed deleted projection")
			}
		}
		d.MetadataRevision = 11
		metadata.Revision = 11
		applyFixture(t, pool, metadata)
		var profiles, backlinks int
		if err := pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM stats_search.profile_documents_v2 WHERE wallet=$1),(SELECT count(*) FROM stats_search.documents_v2 WHERE payload->>'profileWallet'=$1 OR payload->>'profileLabel'='Public profile')`, wallet).Scan(&profiles, &backlinks); err != nil || profiles != 0 || backlinks != 0 {
			t.Fatal(profiles, backlinks, err)
		}
		applyFixture(t, pool, Change{Version: 2, Kind: "visibility", Revision: 12, PoolAddress: d.PoolAddress, Tombstone: true})
		if applyFixture(t, pool, Change{Version: 2, Kind: "visibility", Revision: 11, PoolAddress: d.PoolAddress, Public: true}) {
			t.Fatal("late visibility restored deleted content")
		}
		d.MetadataRevision = 13
		metadata.Revision = 13
		applyFixture(t, pool, metadata)
		page, err := (Store{Pool: pool}).SearchIntegrated(ctx, IntegratedRequest{Request: Request{Query: d.PoolAddress, Kind: "pool", Limit: 25}, Sort: "address"})
		if err != nil || len(page.Items) != 0 || page.Status != "not-indexed" {
			t.Fatal(page, err)
		}
	}
}

func TestProjectionRejectsMutatedCreationProvenance(t *testing.T) {
	pool := integratedDatabase(t)
	original := seedIndexed(t, pool, 30)
	for _, mutate := range []func(*IndexedDocument){
		func(d *IndexedDocument) { d.CreationEventID = "different_creation" },
		func(d *IndexedDocument) { d.SourceBlockHash = "0x" + strings.Repeat("2", 64) },
		func(d *IndexedDocument) { d.SourceHeight = "2"; d.ChainHeight = "2" },
	} {
		d := original
		mutate(&d)
		d.Name, d.NameKey, d.MetadataRevision = "Forged", "forged", 2
		ctx := context.Background()
		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		changed, err := applyChangeTx(ctx, tx, Change{Version: 2, Kind: "metadata", Revision: 2, PoolAddress: d.PoolAddress, Document: &d})
		rollbackSearch(tx)
		if changed || err != delivery.ErrIntegrity {
			t.Fatal(changed, err)
		}
	}
	page, err := (Store{Pool: pool}).SearchIntegrated(context.Background(), IntegratedRequest{Request: Request{Query: original.PoolAddress, Kind: "pool", Limit: 25}, Sort: "address"})
	if err != nil || len(page.Items) != 1 || page.Items[0].Label != original.Name {
		t.Fatal(page, err)
	}
}
