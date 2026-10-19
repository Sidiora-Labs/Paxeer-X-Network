package loadtest

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http/httptest"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/Sidiora-Labs/Paxeer-X-Network/services/platform/perps-data/internal/config"
	"github.com/Sidiora-Labs/Paxeer-X-Network/services/platform/perps-data/internal/delivery"
	"github.com/Sidiora-Labs/Paxeer-X-Network/services/platform/perps-data/internal/delivery/natsdriver"
	"github.com/Sidiora-Labs/Paxeer-X-Network/services/platform/perps-data/internal/gateway"
	"github.com/Sidiora-Labs/Paxeer-X-Network/services/platform/perps-data/internal/publish"
	"github.com/Sidiora-Labs/Paxeer-X-Network/services/platform/perps-data/internal/store"
	"github.com/Sidiora-Labs/Paxeer-X-Network/services/platform/perps-data/internal/stream"
	"github.com/Sidiora-Labs/Paxeer-X-Network/services/platform/perps-data/internal/telemetry"
)

const (
	testDatabaseEnv = "PERPS_DATA_TEST_DATABASE_URL"
	testRPCEnv      = "PERPS_DATA_TEST_RPC"
	testChain       = 125
	testSecret      = "loadtest-secret-of-at-least-32-bytes!"
	testSockets     = 2000
	// headBlock is the head cursor of the seeded books; the fills follow it.
	headBlock = 10
	// levelsPerSide resting orders per side and market, more than the 20
	// levels of a live book frame.
	levelsPerSide = 25
	fillEvery     = 500 * time.Millisecond
	e18           = "000000000000000000"
)

var (
	perpsEmitter = address(0x10, 0x15)
	maker        = address(0xb2, 0x05)
	taker        = address(0xb1, 0x04)
)

func address(hi, lo byte) []byte {
	b := make([]byte, 20)
	b[18], b[19] = hi, lo
	return b
}

func hash32(block int64, txIndex, logIndex int) []byte {
	return []byte(fmt.Sprintf("%016d%08d%08d", block, txIndex+1, logIndex))
}

// seedBooks writes the head cursor and, for each of the eighteen markets,
// levelsPerSide active bids below 1000 and asks above it, each order alone
// at its price with a distinct size.
func seedBooks(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	if _, err := db.ExecContext(ctx, `INSERT INTO perps.cursors (name, height, hash) VALUES ('head', $1, $2)`, headBlock, hash32(headBlock, -1, 0)); err != nil {
		t.Fatal(err)
	}
	for m := range len(FeedSymbols) {
		for k := range 2 * levelsPerSide {
			bid := k < levelsPerSide
			price := 1001 + k - levelsPerSide
			if bid {
				price = 999 - k
			}
			size := strconv.Itoa(1000+k) + e18
			if _, err := db.ExecContext(ctx, `INSERT INTO perps.orders (order_id, account, market_id, order_type, is_long, trigger_price,
				size_usd, limit_price, remaining_size_usd, status, placed_block) VALUES ($1, $2, $3, 1, $4, 0, $5, $6, $5, 'active', 1)`,
				orderID(m, k), maker, m, bid, size, strconv.Itoa(price)+e18); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func orderID(market, k int) int64 { return int64(market*1000 + k + 1) }

// broker starts an in-process JetStream server holding PERPS_V1 of the test
// chain on one replica.
func broker(t *testing.T, ctx context.Context) (*nats.Conn, jetstream.JetStream) {
	t.Helper()
	ns, err := server.NewServer(&server.Options{Host: "127.0.0.1", Port: server.RANDOM_PORT, JetStream: true, StoreDir: t.TempDir(), NoLog: true, NoSigs: true})
	if err != nil {
		t.Fatal(err)
	}
	go ns.Start()
	t.Cleanup(func() { ns.Shutdown(); ns.WaitForShutdown() })
	if !ns.ReadyForConnections(10 * time.Second) {
		t.Fatal("in-process broker not ready")
	}
	nc, err := nats.Connect(ns.ClientURL())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(nc.Close)
	js, err := jetstream.New(nc)
	if err != nil {
		t.Fatal(err)
	}
	cfg := stream.Config(testChain)
	cfg.Replicas = 1
	if _, err := js.CreateStream(ctx, cfg); err != nil {
		t.Fatal(err)
	}
	return nc, js
}

// trader writes one fill per market every fillEvery as the indexer does: the
// fill row, the remaining size of its resting maker order and the head
// cursor in one transaction, then the fill message on PERPS_V1 with the
// headers of the perps publisher.
type trader struct {
	db    *sql.DB
	js    jetstream.JetStream
	block int64
	seq   int64
	fills int
}

func (tr *trader) run(ctx context.Context) error {
	t := time.NewTicker(fillEvery)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-t.C:
		}
		if err := tr.round(ctx); err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
	}
}

func (tr *trader) round(ctx context.Context) error {
	tr.block++
	block := headBlock + tr.block
	now := time.Now().UTC()
	tx, err := tr.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	for m := range len(FeedSymbols) {
		k := tr.fills % (2 * levelsPerSide)
		if _, err := tx.ExecContext(ctx, `INSERT INTO perps.fills (block, block_hash, tx_index, log_index, sub, tx_hash, receipt_kind,
			block_time, emitter, kind, account, tx_from, market_id, maker_order_id, taker_order_id, maker, taker, taker_is_long, price,
			size_usd, maker_fee_usd, taker_fee_usd) VALUES ($1, $2, 0, $3, 0, $4, 'tx', $5, $6, 'fill', $7, $7, $8, $9, $10, $11, $7, $12,
			$13, $14, 0, 0)`,
			block, hash32(block, -1, 0), m, hash32(block, 0, m), now, perpsEmitter, taker, m, orderID(m, k), int64(1_000_000+tr.fills),
			maker, k >= levelsPerSide, "1000"+e18, "1"+e18); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE perps.orders SET remaining_size_usd = remaining_size_usd - $2::numeric WHERE order_id = $1`,
			orderID(m, k), "1"+e18); err != nil {
			return err
		}
		tr.fills++
	}
	if _, err := tx.ExecContext(ctx, `UPDATE perps.cursors SET height = $1, hash = $2 WHERE name = 'head'`, block, hash32(block, -1, 0)); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	for m := range len(FeedSymbols) {
		if err := tr.publish(ctx, m, block, now); err != nil {
			return err
		}
	}
	return nil
}

func (tr *trader) publish(ctx context.Context, market int, block int64, at time.Time) error {
	tr.seq++
	subject, err := stream.MarketSubject(testChain, uint64(market), "fill")
	if err != nil {
		return err
	}
	id, err := natsdriver.MsgID(fmt.Sprintf("%d:%d:0x%x:0:%d:0", testChain, block, hash32(block, -1, 0), market))
	if err != nil {
		return err
	}
	payload := `{"block_time":"` + at.Format(time.RFC3339Nano) + `"}`
	env := delivery.Envelope{
		EventID: id, EventType: "fill", SchemaVersion: 1, ChainID: testChain,
		SourceOrder: delivery.SourceOrder{Height: strconv.FormatInt(block, 10), TransactionIndex: "0", LogIndex: strconv.Itoa(market)},
		EmittedAt:   at.Truncate(time.Microsecond), TraceID: id,
		PayloadHash: delivery.Hash([]byte(payload)), Payload: json.RawMessage(payload),
	}
	wire, err := env.Encode(delivery.Policy{Subjects: map[string]bool{subject: true}, EventTypes: map[string]bool{"fill": true}}, subject)
	if err != nil {
		return err
	}
	msg := &nats.Msg{Subject: subject, Data: wire, Header: nats.Header{}}
	msg.Header.Set(publish.HeaderSeq, strconv.FormatInt(tr.seq, 10))
	msg.Header.Set(publish.HeaderBlock, env.SourceOrder.Height)
	msg.Header.Set(publish.HeaderKind, "fill")
	_, err = tr.js.PublishMsg(ctx, msg, jetstream.WithMsgID(id), jetstream.WithExpectStream(stream.Name))
	return err
}

func TestLoadProfile(t *testing.T) {
	dbURL := os.Getenv(testDatabaseEnv)
	if dbURL == "" {
		t.Fatal(testDatabaseEnv + " is unset")
	}
	rpcURL := os.Getenv(testRPCEnv)
	if rpcURL == "" {
		t.Fatal(testRPCEnv + " is unset")
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	db, err := store.Open(dbURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.ExecContext(ctx, `DROP SCHEMA IF EXISTS perps CASCADE`); err != nil {
		t.Fatal(err)
	}
	if err := store.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	seedBooks(t, ctx, db)
	nc, js := broker(t, ctx)
	node, err := ethclient.DialContext(ctx, rpcURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(node.Close)
	metrics, err := telemetry.New(config.Gateway)
	if err != nil {
		t.Fatal(err)
	}
	srv, err := gateway.NewServer(ctx, gateway.Options{DB: db, NATS: nc, Chain: testChain, AuthSecret: testSecret, Node: node, Metrics: metrics})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	runErr := make(chan error, 1)
	go func() { runErr <- srv.Run(ctx) }()
	deadline := time.Now().Add(30 * time.Second)
	for !srv.Ready() {
		select {
		case err := <-runErr:
			t.Fatalf("gateway run stopped before ready: %v", err)
		default:
		}
		if time.Now().After(deadline) {
			t.Fatal("gateway not ready within 30 seconds")
		}
		time.Sleep(50 * time.Millisecond)
	}

	tradeCtx, stopTrades := context.WithCancel(ctx)
	tradeErr := make(chan error, 1)
	tr := &trader{db: db, js: js}
	go func() { tradeErr <- tr.run(tradeCtx) }()

	socketURL, err := SocketURL(ts.URL)
	if err != nil {
		t.Fatal(err)
	}
	profile := FullProfile().Scaled(testSockets)
	report, err := Run(ctx, socketURL, profile)
	stopTrades()
	if err != nil {
		t.Fatal(err)
	}
	if err := <-tradeErr; err != nil {
		t.Fatalf("trader: %v", err)
	}
	raw, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("report %s", raw)

	t.Run("sockets", func(t *testing.T) {
		if profile.Sockets != testSockets || FullProfile().Sockets != 140000 || len(profile.Symbols) != 18 {
			t.Fatalf("profile %d sockets over %d symbols, full %d", profile.Sockets, len(profile.Symbols), FullProfile().Sockets)
		}
		if got := profile.Topics(19); len(got) != 3 || got[0] != "ETH@perp_orderbook" || got[1] != "ETH@perp_trade" || got[2] != "markets@all" {
			t.Fatalf("topics of socket 19 %v", got)
		}
		if report.Welcomed != testSockets || report.Guarded != testSockets {
			t.Fatalf("welcomed %d, guarded %d, want %d each", report.Welcomed, report.Guarded, testSockets)
		}
		if report.Snapshots < 3*testSockets {
			t.Fatalf("%d snapshots, want at least %d", report.Snapshots, 3*testSockets)
		}
		if report.ErrorFrames != 0 || report.DialFailures != 0 {
			t.Fatalf("%d error frames, %d dial failures", report.ErrorFrames, report.DialFailures)
		}
	})

	t.Run("reconnect_storm", func(t *testing.T) {
		if report.Storm != 400 || report.Recovered != 400 {
			t.Fatalf("storm %d, recovered %d, want 400 each", report.Storm, report.Recovered)
		}
		if report.MaxRecovery <= 0 || report.MaxRecovery > 60*time.Second {
			t.Fatalf("slowest recovery %s, want within 60s", report.MaxRecovery)
		}
		if report.Live != testSockets {
			t.Fatalf("%d sockets live at the end, want %d", report.Live, testSockets)
		}
	})

	t.Run("sequence_and_latency", func(t *testing.T) {
		if report.Finished.Sub(report.Started) < 60*time.Second {
			t.Fatalf("ran %s, want at least 60s", report.Finished.Sub(report.Started))
		}
		if report.Frames <= report.Snapshots {
			t.Fatalf("%d frames, %d snapshots: no live frame", report.Frames, report.Snapshots)
		}
		if report.UnresolvedGaps != 0 || report.Resynced > report.Gaps {
			t.Fatalf("gaps %d, resynced %d, unresolved %d", report.Gaps, report.Resynced, report.UnresolvedGaps)
		}
		if report.P99Delay > 250*time.Millisecond {
			t.Fatalf("p99 frame delay %s above 250ms", report.P99Delay)
		}
		if report.Closes1013NoRetry != 0 {
			t.Fatalf("%d 1013 closes without retry-after", report.Closes1013NoRetry)
		}
	})

	t.Run("book_checksums", func(t *testing.T) {
		if report.BookFrames == 0 || report.Checksums < int64(testSockets) {
			t.Fatalf("%d book frames, %d checksums, want at least one checksum per socket", report.BookFrames, report.Checksums)
		}
		if report.ChecksumMismatches != 0 || report.ChecksumLate != 0 {
			t.Fatalf("%d checksum mismatches, %d late", report.ChecksumMismatches, report.ChecksumLate)
		}
		if report.BookRateViolations != 0 || report.BookLevelViolations != 0 {
			t.Fatalf("%d frames beyond 10 per second, %d beyond 20 levels", report.BookRateViolations, report.BookLevelViolations)
		}
	})

	if err := report.Err(); err != nil {
		t.Fatalf("report: %v", err)
	}
	cancel()
	select {
	case err := <-runErr:
		if err != nil && !errors.Is(err, context.Canceled) {
			t.Fatalf("gateway run: %v", err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("gateway run did not stop after cancel")
	}
}
