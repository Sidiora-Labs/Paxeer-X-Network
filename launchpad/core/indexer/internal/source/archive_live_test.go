package source

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
)

func liveRPC(t *testing.T, ctx context.Context, url, method string, params []any, out any) {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": method, "params": params})
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("%s %s: %v", url, method, err)
	}
	defer response.Body.Close()
	var envelope struct {
		Result json.RawMessage
		Error  *struct{ Message string }
	}
	if err = json.NewDecoder(response.Body).Decode(&envelope); err != nil || envelope.Error != nil {
		t.Fatalf("%s %s: %v %+v", url, method, err, envelope.Error)
	}
	if err = json.Unmarshal(envelope.Result, out); err != nil {
		t.Fatal(err)
	}
}

// The archive path (bloom-gated blocks plus verified receipts) must reproduce
// exactly what the primary's eth_getLogs path yields for the same range and
// address set, on real chain-125 activity both endpoints still serve
// (INDEXER_LIVE_INGEST=1, NETWORK_RPC_URL, INDEXER_HISTORICAL_RPC_URL;
// INDEXER_LIVE_PARITY_FROM/TO bound the range, default the real market's
// creation window).
func TestLiveArchiveLogsMatchPrimaryGetLogs(t *testing.T) {
	if os.Getenv("INDEXER_LIVE_INGEST") != "1" {
		t.Skip("live chain-125 parity requires INDEXER_LIVE_INGEST=1")
	}
	primaryURL, archiveURL := os.Getenv("NETWORK_RPC_URL"), os.Getenv("INDEXER_HISTORICAL_RPC_URL")
	if primaryURL == "" || archiveURL == "" {
		t.Fatal("NETWORK_RPC_URL and INDEXER_HISTORICAL_RPC_URL are required")
	}
	from, to := uint64(33963700), uint64(33964199)
	for name, field := range map[string]*uint64{"INDEXER_LIVE_PARITY_FROM": &from, "INDEXER_LIVE_PARITY_TO": &to} {
		if raw := os.Getenv(name); raw != "" {
			v, err := strconv.ParseUint(raw, 10, 64)
			if err != nil {
				t.Fatal(err)
			}
			*field = v
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	var anchor struct{ Hash string }
	liveRPC(t, ctx, archiveURL, "eth_getBlockByNumber", []any{fmtHeight(from - 1), false}, &anchor)
	// Every emitter active in the range, as the primary reports it.
	var emitted []struct{ Address string }
	liveRPC(t, ctx, primaryURL, "eth_getLogs", []any{map[string]any{"fromBlock": fmtHeight(from), "toBlock": fmtHeight(to)}}, &emitted)
	seen := map[string]bool{}
	const manifest = "live-archive-parity"
	var epochs []AddressEpoch
	for _, l := range emitted {
		a := strings.ToLower(l.Address)
		if !seen[a] {
			seen[a] = true
			epochs = append(epochs, AddressEpoch{Address: a, Role: "Emitter", ABIRevision: "v1", ManifestDigest: manifest, ActivationHeight: from})
		}
	}
	if len(epochs) == 0 {
		t.Fatalf("no logs in %d..%d", from, to)
	}
	s, err := NewCommittedSource(ctx, CommittedOptions{Endpoints: map[string]string{"paxeer-primary": primaryURL}, HistoricalEndpoints: map[string]string{"paxeer-historical": archiveURL}, Anchor: &CommittedAnchor{Height: from - 1, Hash: common.HexToHash(anchor.Hash)}, BlockIdentity: "reported", ManifestDigest: manifest, Epochs: epochs})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var primary, archive *committedEndpoint
	for _, e := range s.endpoints {
		if e.historical {
			archive = e
		} else {
			primary = e
		}
	}
	if primary == nil || archive == nil {
		t.Fatal("both endpoints must be admitted")
	}
	results := map[string]FetchResult{}
	for _, e := range []*committedEndpoint{primary, archive} {
		var merged FetchResult
		started := time.Now()
		for start := from; start <= to; start += 64 {
			end := min(start+63, to)
			r, err := s.fetch(ctx, e, FetchOptions{FromBlock: int64(start), ToBlock: int64(end)}, true, epochs)
			if err != nil {
				t.Fatalf("%s %d..%d: %v", e.id, start, end, err)
			}
			if merged.Blocks == nil {
				merged = r
				continue
			}
			merged.Logs = append(merged.Logs, r.Logs...)
			for k, v := range r.Blocks {
				merged.Blocks[k] = v
			}
			for k, v := range r.TxFromMap {
				merged.TxFromMap[k] = v
			}
		}
		t.Logf("%s: %d blocks, %d logs in %s", e.id, len(merged.Blocks), len(merged.Logs), time.Since(started).Round(time.Millisecond))
		results[e.id] = merged
	}
	p, a := results[primary.id], results[archive.id]
	sort.SliceStable(p.Logs, func(i, j int) bool { return p.Logs[i].BlockNumber < p.Logs[j].BlockNumber })
	if len(p.Logs) != len(emitted) || len(a.Logs) != len(p.Logs) {
		t.Fatalf("log counts: primary getLogs %d, primary fetch %d, archive %d", len(emitted), len(p.Logs), len(a.Logs))
	}
	for i := range p.Logs {
		x, y := p.Logs[i], a.Logs[i]
		if x.Address != y.Address || x.BlockNumber != y.BlockNumber || x.BlockHash != y.BlockHash || x.TxHash != y.TxHash || x.TxIndex != y.TxIndex || x.Index != y.Index || !reflect.DeepEqual(x.Topics, y.Topics) || !bytes.Equal(x.Data, y.Data) || x.Removed != y.Removed {
			t.Fatalf("log %d differs:\nprimary %+v\narchive %+v", i, x, y)
		}
	}
	for h, x := range p.Blocks {
		y := a.Blocks[h]
		if x.Hash != y.Hash || x.ParentHash != y.ParentHash || x.TimestampSeconds != y.TimestampSeconds || x.TransactionCount != y.TransactionCount {
			t.Fatalf("block %d differs: %+v vs %+v", h, x, y)
		}
	}
	if !reflect.DeepEqual(p.TxFromMap, a.TxFromMap) {
		t.Fatalf("tx senders differ: %d vs %d", len(p.TxFromMap), len(a.TxFromMap))
	}
	m := s.Metrics()
	t.Logf("%s", fmt.Sprintf("parity %d..%d: %d logs from %d emitters identical; archive bloomSkipped=%d receiptBlocks=%d", from, to, len(p.Logs), len(epochs), m.BloomSkipped, m.ReceiptFallbacks))
}
