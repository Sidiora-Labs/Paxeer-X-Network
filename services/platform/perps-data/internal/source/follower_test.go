package source

import (
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"os"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"

	abci "github.com/Sidiora-Labs/Paxeer-X-Network/consensus/abci/types"
	rpchttp "github.com/Sidiora-Labs/Paxeer-X-Network/consensus/rpc/client/http"
	"github.com/Sidiora-Labs/Paxeer-X-Network/services/platform/perps-data/internal/decode"
	"github.com/Sidiora-Labs/Paxeer-X-Network/services/platform/perps-data/internal/delivery"
	"github.com/Sidiora-Labs/Paxeer-X-Network/services/platform/perps-data/internal/markets"
	"github.com/Sidiora-Labs/Paxeer-X-Network/services/platform/perps-data/internal/store"
)

const testChainID = 125

func itoa(n uint64) string { return strconv.FormatUint(n, 10) }

var transferTopic = crypto.Keccak256Hash([]byte("Transfer(address,address,uint256)"))

type harness struct {
	ctx      context.Context
	rpcURL   string
	cometURL string
	db       *sql.DB
	rpc      *RPC
	comet    *Comet
	outbox   *delivery.Store
}

// recorder keeps what the follower hands to its Write and Rebuild steps.
type recorder struct {
	blocks   []*Block
	affected []Affected
}

func (r *recorder) write(_ context.Context, _ *sql.Tx, b *Block) error {
	r.blocks = append(r.blocks, b)
	return nil
}

func (r *recorder) rebuild(_ context.Context, _ *sql.Tx, a Affected) error {
	r.affected = append(r.affected, a)
	return nil
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	h := &harness{ctx: context.Background(), rpcURL: os.Getenv("PERPS_DATA_TEST_RPC"), cometURL: os.Getenv("PERPS_DATA_TEST_COMET")}
	dbURL := os.Getenv("PERPS_DATA_TEST_DATABASE_URL")
	if h.rpcURL == "" || h.cometURL == "" || dbURL == "" {
		t.Fatal("TestFollower requires PERPS_DATA_TEST_RPC, PERPS_DATA_TEST_COMET and PERPS_DATA_TEST_DATABASE_URL")
	}
	db, err := store.Open(dbURL)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := store.Migrate(h.ctx, db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	tables := []string{"perps.blocks", "perps.cursors", "perps.delivery_outbox"}
	for _, f := range factTables {
		tables = append(tables, "perps."+f)
	}
	if _, err := db.ExecContext(h.ctx, "TRUNCATE "+strings.Join(tables, ", ")); err != nil {
		t.Fatalf("reset: %v", err)
	}
	h.db = db
	if h.rpc, err = NewRPC(h.ctx, RPCOptions{URLs: []string{h.rpcURL}}); err != nil {
		t.Fatalf("rpc: %v", err)
	}
	t.Cleanup(h.rpc.Close)
	if h.comet, err = NewComet([]string{h.cometURL}, 2*time.Second); err != nil {
		t.Fatalf("comet: %v", err)
	}
	subject := "perps.125.chain.rewind"
	if h.outbox, err = delivery.NewStore(db, "perps", delivery.Policy{Subjects: map[string]bool{subject: true}, EventTypes: map[string]bool{rewindKind: true}}); err != nil {
		t.Fatalf("outbox: %v", err)
	}
	return h
}

func (h *harness) follower(t *testing.T, start uint64, rec *recorder) *Follower {
	t.Helper()
	f, err := NewFollower(h.ctx, FollowerOptions{DB: h.db, RPC: h.rpc, Comet: h.comet, Outbox: h.outbox, ChainID: testChainID, StartHeight: start, Write: rec.write, Rebuild: rec.rebuild})
	if err != nil {
		t.Fatalf("follower: %v", err)
	}
	return f
}

// base is a height 40 blocks below the node head.
func (h *harness) base(t *testing.T) uint64 {
	t.Helper()
	head, err := h.rpc.Head(h.ctx)
	if err != nil {
		t.Fatalf("head: %v", err)
	}
	if head < 1000 {
		t.Fatalf("node head %d below 1000", head)
	}
	return head - 40
}

func (h *harness) cursor(t *testing.T) uint64 {
	t.Helper()
	var height int64
	if err := h.db.QueryRowContext(h.ctx, "SELECT height FROM perps.cursors WHERE name = 'head'").Scan(&height); err != nil {
		t.Fatalf("cursor: %v", err)
	}
	return uint64(height)
}

func (h *harness) count(t *testing.T, query string, args ...any) int {
	t.Helper()
	var n int
	if err := h.db.QueryRowContext(h.ctx, query, args...).Scan(&n); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	return n
}

type outboxRow struct {
	subject  string
	envelope delivery.Envelope
}

func (h *harness) outboxRows(t *testing.T) []outboxRow {
	t.Helper()
	rows, err := h.db.QueryContext(h.ctx, "SELECT subject, envelope FROM perps.delivery_outbox ORDER BY perps_seq")
	if err != nil {
		t.Fatalf("outbox: %v", err)
	}
	defer rows.Close()
	var out []outboxRow
	for rows.Next() {
		var r outboxRow
		var wire []byte
		if err := rows.Scan(&r.subject, &wire); err != nil {
			t.Fatalf("outbox scan: %v", err)
		}
		if err := json.Unmarshal(wire, &r.envelope); err != nil {
			t.Fatalf("envelope: %v", err)
		}
		out = append(out, r)
	}
	return out
}

func (h *harness) follow(t *testing.T, f *Follower) int {
	t.Helper()
	n, err := f.Step(h.ctx)
	if err != nil {
		t.Fatalf("step: %v", err)
	}
	return n
}

func TestFollower(t *testing.T) {
	t.Run("follows_head", func(t *testing.T) {
		h := newHarness(t)
		base := h.base(t)
		rec := &recorder{}
		f := h.follower(t, base, rec)
		n := h.follow(t, f)
		if n < 11 {
			t.Fatalf("stored %d blocks, want at least 11", n)
		}
		if got := h.cursor(t); got != base+uint64(n)-1 {
			t.Fatalf("cursor %d, want %d", got, base+uint64(n)-1)
		}
		if got := h.count(t, "SELECT count(*) FROM perps.blocks"); got != n {
			t.Fatalf("%d block rows, want %d", got, n)
		}
		for i := uint64(0); i <= 10; i++ {
			node, err := h.rpc.Block(h.ctx, base+i)
			if err != nil {
				t.Fatalf("block %d: %v", base+i, err)
			}
			if got := h.count(t, "SELECT count(*) FROM perps.blocks WHERE number = $1 AND hash = $2 AND parent_hash = $3", int64(base+i), node.Hash.Bytes(), node.ParentHash.Bytes()); got != 1 {
				t.Fatalf("block %d stored %d times with the node hash", base+i, got)
			}
			if rec.blocks[i].Number != base+i || rec.blocks[i].Hash != node.Hash {
				t.Fatalf("write %d got block %d %s", i, rec.blocks[i].Number, rec.blocks[i].Hash.Hex())
			}
		}
		if len(rec.blocks) != n {
			t.Fatalf("write saw %d blocks, want %d", len(rec.blocks), n)
		}
	})

	t.Run("detects_parent_mismatch", func(t *testing.T) {
		h := newHarness(t)
		base := h.base(t)
		stored := common.HexToHash("0x07aa")
		if _, err := h.db.ExecContext(h.ctx, "INSERT INTO perps.blocks(number, hash, parent_hash, time, proposer, tx_count) VALUES($1, $2, $3, now(), $4, 0)", int64(base-1), stored.Bytes(), common.HexToHash("0x06aa").Bytes(), strings.Repeat("A", 40)); err != nil {
			t.Fatalf("insert: %v", err)
		}
		if _, err := h.db.ExecContext(h.ctx, "INSERT INTO perps.cursors(name, height, hash) VALUES('head', $1, $2)", int64(base-1), stored.Bytes()); err != nil {
			t.Fatalf("cursor: %v", err)
		}
		rec := &recorder{}
		f := h.follower(t, base, rec)
		if n := h.follow(t, f); n != 0 {
			t.Fatalf("stored %d blocks across a parent mismatch", n)
		}
		rows := h.outboxRows(t)
		if len(rows) != 1 || string(rows[0].envelope.Payload) != `{"from":`+itoa(base-1)+`}` {
			t.Fatalf("rewind messages %+v, want one from %d", rows, base-1)
		}
		if got := h.count(t, "SELECT count(*) FROM perps.blocks"); got != 0 {
			t.Fatalf("%d block rows left", got)
		}
		if len(rec.affected) != 1 || rec.affected[0].From != base-1 || len(rec.affected[0].Accounts) != 0 || len(rec.affected[0].Markets) != 0 {
			t.Fatalf("rebuild %+v", rec.affected)
		}
		if got := h.count(t, "SELECT count(*) FROM perps.cursors"); got != 0 {
			t.Fatalf("cursor kept below the lowest stored block")
		}
	})

	diverge := func(t *testing.T, h *harness, base uint64) (*Follower, *recorder, uint64) {
		t.Helper()
		rec := &recorder{}
		f := h.follower(t, base, rec)
		n := h.follow(t, f)
		if n < 11 {
			t.Fatalf("stored %d blocks, want at least 11", n)
		}
		if _, err := h.db.ExecContext(h.ctx, "UPDATE perps.blocks SET hash = sha256(int8send(number)) WHERE number >= $1", int64(base+6)); err != nil {
			t.Fatalf("diverge: %v", err)
		}
		fork, err := f.rewind(h.ctx, h.cursor(t))
		if err != nil {
			t.Fatalf("rewind: %v", err)
		}
		return f, rec, fork
	}

	t.Run("rewinds_to_fork", func(t *testing.T) {
		h := newHarness(t)
		base := h.base(t)
		_, rec, fork := diverge(t, h, base)
		if fork != base+6 {
			t.Fatalf("fork %d, want %d", fork, base+6)
		}
		if got := h.count(t, "SELECT count(*) FROM perps.blocks WHERE number >= $1", int64(base+6)); got != 0 {
			t.Fatalf("%d rows at or above the fork", got)
		}
		node, err := h.rpc.Block(h.ctx, base+5)
		if err != nil {
			t.Fatalf("block: %v", err)
		}
		if got := h.count(t, "SELECT count(*) FROM perps.blocks WHERE number = $1 AND hash = $2", int64(base+5), node.Hash.Bytes()); got != 1 {
			t.Fatalf("block %d not kept", base+5)
		}
		if got := h.cursor(t); got != base+5 {
			t.Fatalf("cursor %d, want %d", got, base+5)
		}
		if got := h.count(t, "SELECT count(*) FROM perps.cursors WHERE name = 'head' AND hash = $1", node.Hash.Bytes()); got != 1 {
			t.Fatalf("cursor hash is not the hash of %d", base+5)
		}
		if len(rec.affected) != 1 || rec.affected[0].From != base+6 {
			t.Fatalf("rebuild %+v", rec.affected)
		}
		rows := h.outboxRows(t)
		if len(rows) != 1 || string(rows[0].envelope.Payload) != `{"from":`+itoa(base+6)+`}` {
			t.Fatalf("rewind messages %+v", rows)
		}
	})

	t.Run("publishes_rewind", func(t *testing.T) {
		h := newHarness(t)
		base := h.base(t)
		_, _, fork := diverge(t, h, base)
		rows := h.outboxRows(t)
		if len(rows) != 1 {
			t.Fatalf("%d outbox rows, want 1", len(rows))
		}
		e := rows[0].envelope
		if rows[0].subject != "perps.125.chain.rewind" || e.EventType != "rewind" || e.ChainID != 125 || string(e.Payload) != `{"from":`+itoa(fork)+`}` {
			t.Fatalf("message %s %+v", rows[0].subject, e)
		}
		if e.SourceOrder != (delivery.SourceOrder{Height: itoa(fork), TransactionIndex: "0", LogIndex: "0"}) || e.PayloadHash != delivery.Hash(e.Payload) {
			t.Fatalf("source order %+v hash %s", e.SourceOrder, e.PayloadHash)
		}
	})

	t.Run("rpc_fallback", func(t *testing.T) {
		h := newHarness(t)
		l, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatalf("listen: %v", err)
		}
		refused := "http://" + l.Addr().String()
		_ = l.Close()
		pool, err := NewRPC(h.ctx, RPCOptions{URLs: []string{refused, h.rpcURL}})
		if err != nil {
			t.Fatalf("rpc: %v", err)
		}
		defer pool.Close()
		head, err := pool.Head(h.ctx)
		if err != nil || head == 0 {
			t.Fatalf("head %d: %v", head, err)
		}
		if got := pool.rankedIndices(); !reflect.DeepEqual(got, []int{1, 0}) {
			t.Fatalf("rank %v, want [1 0]", got)
		}
	})

	t.Run("synthetic_receipt_check", func(t *testing.T) {
		h := newHarness(t)
		head, err := h.rpc.Head(h.ctx)
		if err != nil {
			t.Fatalf("head: %v", err)
		}
		res, err := h.rpc.Logs(h.ctx, FetchOptions{MonitoredAddresses: []string{strings.ToLower(PerpsModule.Hex())}, FromBlock: int64(head - MaxLogRange + 1), ToBlock: int64(head)})
		if err != nil {
			t.Fatalf("logs: %v", err)
		}
		var height uint64
		for _, l := range res.Logs {
			if l.TxHash == SyntheticHash("perps-endblock", l.BlockNumber) {
				height = l.BlockNumber
				break
			}
		}
		if height == 0 {
			t.Fatalf("no perps_endblock receipt in %d-%d", head-MaxLogRange+1, head)
		}
		var logs []ethtypes.Log
		for _, l := range res.Logs {
			if l.BlockNumber == height {
				logs = append(logs, l)
			}
		}
		evm, err := h.rpc.Block(h.ctx, height)
		if err != nil {
			t.Fatalf("block: %v", err)
		}
		results, err := h.comet.BlockResults(h.ctx, int64(height))
		if err != nil {
			t.Fatalf("block_results: %v", err)
		}
		f := h.follower(t, height, &recorder{})
		when := time.Unix(evm.Time, 0).UTC()
		classified, err := f.checkSynthetic(height, results.TxCount, evm.Transactions, logs, when)
		if err != nil {
			t.Fatalf("check: %v", err)
		}
		found := -1
		for i, l := range classified {
			if l.TxHash == SyntheticHash("perps-endblock", height) {
				found = i
				if l.ReceiptKind != decode.ReceiptPerpsEndBlock || l.TxIndex != uint(results.TxCount) || l.TxFrom != nil {
					t.Fatalf("perps receipt %s index %d from %v, want perps_endblock %d nil", l.ReceiptKind, l.TxIndex, l.TxFrom, results.TxCount)
				}
			} else if l.ReceiptKind == decode.ReceiptTx && (l.TxFrom == nil || l.TxIndex >= uint(results.TxCount)) {
				t.Fatalf("tx receipt %s index %d of %d", l.TxHash.Hex(), l.TxIndex, results.TxCount)
			}
		}
		if found < 0 {
			t.Fatal("perps_endblock receipt not classified")
		}
		served := append([]ethtypes.Log(nil), logs...)
		for i := range served {
			if served[i].TxHash == SyntheticHash("perps-endblock", height) {
				served[i].TxHash = SyntheticHash("spot-endblock", height)
			}
		}
		_, err = f.checkSynthetic(height, results.TxCount, evm.Transactions, served, when)
		var mismatch *SyntheticMismatchError
		if !errors.As(err, &mismatch) {
			t.Fatalf("check of a misplaced spot receipt: %v", err)
		}
		want := SyntheticMismatchError{Height: height, TxHash: SyntheticHash("spot-endblock", height), Kind: decode.ReceiptSpotEndBlock, ExpectedIndex: uint(results.TxCount) + 1, ServedIndex: uint(results.TxCount)}
		if *mismatch != want {
			t.Fatalf("mismatch %+v, want %+v", *mismatch, want)
		}
		if !strings.Contains(err.Error(), itoa(height)) {
			t.Fatalf("error %q does not name height %d", err, height)
		}
	})

	t.Run("comet_header_and_results", func(t *testing.T) {
		h := newHarness(t)
		base := h.base(t)
		rec := &recorder{}
		f := h.follower(t, base, rec)
		if n := h.follow(t, f); n == 0 {
			t.Fatal("stored no blocks")
		}
		b := rec.blocks[0]
		direct, err := rpchttp.New(h.cometURL)
		if err != nil {
			t.Fatalf("comet: %v", err)
		}
		height := int64(base)
		header, err := direct.Header(h.ctx, &height)
		if err != nil {
			t.Fatalf("header: %v", err)
		}
		results, err := direct.BlockResults(h.ctx, &height)
		if err != nil {
			t.Fatalf("block_results: %v", err)
		}
		proposer := strings.ToUpper(hex.EncodeToString(header.Header.ProposerAddress))
		if b.Proposer != proposer || len(proposer) != 40 {
			t.Fatalf("proposer %q, want %q", b.Proposer, proposer)
		}
		if got := h.count(t, "SELECT count(*) FROM perps.blocks WHERE number = $1 AND proposer = $2 AND tx_count = $3", height, proposer, len(results.TxsResults)); got != 1 {
			t.Fatalf("block row of %d lacks proposer %s", height, proposer)
		}
		count := func(events []abci.Event) int {
			n := 0
			for _, e := range events {
				if e.Type == "pricefeed_aggregate" {
					n++
				}
			}
			return n
		}
		if !reflect.DeepEqual(b.FinalizeBlockEvents, results.FinalizeBlockEvents) || count(b.FinalizeBlockEvents) != count(results.FinalizeBlockEvents) {
			t.Fatalf("finalize_block_events: %d pricefeed_aggregate passed, node has %d", count(b.FinalizeBlockEvents), count(results.FinalizeBlockEvents))
		}
	})

	t.Run("two_pass_pointers", func(t *testing.T) {
		h := newHarness(t)
		head, err := h.rpc.Head(h.ctx)
		if err != nil {
			t.Fatalf("head: %v", err)
		}
		var created uint64
		var pointer common.Address
		for to := head; to >= MaxLogRange && created == 0; to -= MaxLogRange {
			res, err := h.rpc.Logs(h.ctx, FetchOptions{MonitoredAddresses: []string{strings.ToLower(SpotVault.Hex())}, FromBlock: int64(to - MaxLogRange + 1), ToBlock: int64(to), Historical: true})
			if err != nil {
				t.Fatalf("logs: %v", err)
			}
			for _, l := range res.Logs {
				if len(l.Topics) != 3 || l.Topics[0] != shareTokenCreatedTopic {
					continue
				}
				p := common.BytesToAddress(l.Topics[2].Bytes())
				same, err := h.rpc.Logs(h.ctx, FetchOptions{MonitoredAddresses: []string{strings.ToLower(p.Hex())}, FromBlock: int64(l.BlockNumber), ToBlock: int64(l.BlockNumber)})
				if err != nil {
					t.Fatalf("pointer logs: %v", err)
				}
				for _, s := range same.Logs {
					if len(s.Topics) > 0 && s.Topics[0] == transferTopic {
						created, pointer = l.BlockNumber, p
					}
				}
			}
		}
		if created == 0 {
			t.Fatal("no ShareTokenCreated with a same-block Transfer on the node")
		}
		rec := &recorder{}
		f := h.follower(t, created, rec)
		if _, seeded := f.pointers[pointer]; !seeded {
			t.Fatalf("pointer %s not seeded from getRegisteredTokens", pointer.Hex())
		}
		delete(f.pointers, pointer)
		if n := h.follow(t, f); n == 0 {
			t.Fatal("stored no blocks")
		}
		if rec.blocks[0].Number != created {
			t.Fatalf("first block %d, want %d", rec.blocks[0].Number, created)
		}
		transfer := false
		for _, l := range rec.blocks[0].Logs {
			if l.Address == pointer && l.Topics[0] == transferTopic {
				transfer = true
			}
		}
		if !transfer {
			t.Fatalf("Transfer of %s in block %d not passed to the writer", pointer.Hex(), created)
		}
		if _, ok := f.pointers[pointer]; !ok {
			t.Fatalf("pointer %s not in the set for %d", pointer.Hex(), created+1)
		}
	})

	t.Run("rewind_keeps_markets", func(t *testing.T) {
		h := newHarness(t)
		client, err := ethclient.Dial(h.rpcURL)
		if err != nil {
			t.Fatalf("dial: %v", err)
		}
		defer client.Close()
		if err := markets.FeeConfig(h.ctx, h.db, client); err != nil {
			t.Fatalf("fee config: %v", err)
		}
		base := h.base(t)
		diverge(t, h, base)
		if got := h.count(t, "SELECT count(*) FROM perps.markets"); got != 18 {
			t.Fatalf("%d markets after rewind, want 18", got)
		}
		if got := h.count(t, "SELECT count(*) FROM perps.fee_config"); got != 1 {
			t.Fatalf("%d fee_config rows after rewind, want 1", got)
		}
	})
}
