package source

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Sidiora-Technologies/KindleLaunch/core/indexer/internal/internaltest"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/jackc/pgx/v5"
)

const canonicalExplorerSchema = `
CREATE TABLE public.blocks(number bigint,hash bytea,parent_hash bytea,timestamp timestamptz,consensus boolean);
CREATE TABLE public.transactions(hash bytea,block_hash bytea,"index" bigint,from_address_hash bytea);
CREATE TABLE public.logs(address_hash bytea,first_topic bytea,second_topic bytea,third_topic bytea,fourth_topic bytea,data bytea,transaction_hash bytea,block_hash bytea,block_number bigint,"index" bigint,transaction_index bigint);
CREATE ROLE explorer_reader LOGIN PASSWORD 'owned-fixture-reader' NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION NOBYPASSRLS;
GRANT USAGE ON SCHEMA public TO explorer_reader;
GRANT SELECT ON public.blocks,public.transactions,public.logs TO explorer_reader;
`

func genuineHistoricalNode(t *testing.T) (context.Context, *ethclient.Client, string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Minute)
	t.Cleanup(cancel)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()
	var output bytes.Buffer
	command := exec.CommandContext(ctx, "anvil", "--host", "127.0.0.1", "--port", strconv.Itoa(port), "--threads", "2", "--chain-id", "125", "--accounts", "1", "--silent")
	command.Stderr = &output
	if err = command.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = command.Process.Kill(); _ = command.Wait() })
	endpoint := "http://127.0.0.1:" + strconv.Itoa(port)
	client, err := ethclient.DialContext(ctx, endpoint)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(client.Close)
	tick := time.NewTicker(20 * time.Millisecond)
	defer tick.Stop()
	for {
		chain, err := client.ChainID(ctx)
		if err == nil && chain.Cmp(big.NewInt(125)) == 0 {
			return ctx, client, endpoint
		}
		select {
		case <-ctx.Done():
			t.Fatalf("node unavailable: %s", output.String())
		case <-tick.C:
		}
	}
}
func historicalArtifact(t *testing.T) (abi.ABI, []byte, string, string) {
	t.Helper()
	path := filepath.Join("..", "..", "..", "..", "..", "contracts", "artifacts", "contracts", "core", "SidioraERC20.sol", "SidioraERC20.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var artifact struct {
		ABI      json.RawMessage `json:"abi"`
		Bytecode string          `json:"bytecode"`
	}
	if err = json.Unmarshal(data, &artifact); err != nil {
		t.Fatal(err)
	}
	parsed, err := abi.JSON(bytes.NewReader(artifact.ABI))
	if err != nil {
		t.Fatal(err)
	}
	var compact bytes.Buffer
	if err = json.Compact(&compact, artifact.ABI); err != nil {
		t.Fatal(err)
	}
	abiHash := sha256.Sum256(compact.Bytes())
	sourceHash := sha256.Sum256(data)
	return parsed, common.FromHex(artifact.Bytecode), hex.EncodeToString(abiHash[:]), hex.EncodeToString(sourceHash[:])
}
func TestHistoricalRealRangeExplorerSuffixAndParity(t *testing.T) {
	ctx, client, endpoint := genuineHistoricalNode(t)
	key, err := crypto.HexToECDSA("ac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80")
	if err != nil {
		t.Fatal(err)
	}
	sender := crypto.PubkeyToAddress(key.PublicKey)
	parsed, bytecode, revision, manifest := historicalArtifact(t)
	auth, err := bind.NewKeyedTransactorWithChainID(key, big.NewInt(125))
	if err != nil {
		t.Fatal(err)
	}
	auth.Context = ctx
	token, creation, _, err := bind.DeployContract(auth, parsed, bytecode, client, "Historical", "HIST", big.NewInt(1000000000000), sender)
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := bind.WaitMined(ctx, client, creation)
	if err != nil || receipt.Status != 1 {
		t.Fatalf("real token deployment: %v", err)
	}
	if receipt.BlockNumber.Uint64() != 1 {
		t.Fatal("unexpected private fixture deployment height")
	}
	if err = client.Client().CallContext(ctx, nil, "evm_setAutomine", false); err != nil {
		t.Fatal(err)
	}
	nonce, err := client.PendingNonceAt(ctx, sender)
	if err != nil {
		t.Fatal(err)
	}
	price, err := client.SuggestGasPrice(ctx)
	if err != nil {
		t.Fatal(err)
	}
	price.Mul(price, big.NewInt(4))
	input, err := parsed.Pack("transfer", common.HexToAddress("0x000000000000000000000000000000000000beef"), big.NewInt(1))
	if err != nil {
		t.Fatal(err)
	}
	for block := 0; block < 100; block++ {
		for item := 0; item < 100; item++ {
			tx, err := ethtypes.SignTx(ethtypes.NewTransaction(nonce, token, big.NewInt(0), 80000, price, input), ethtypes.LatestSignerForChainID(big.NewInt(125)), key)
			if err != nil {
				t.Fatal(err)
			}
			if err = client.SendTransaction(ctx, tx); err != nil {
				t.Fatal(err)
			}
			nonce++
		}
		if err = client.Client().CallContext(ctx, nil, "anvil_mine", "0x1"); err != nil {
			t.Fatal(err)
		}
	}
	if err = client.Client().CallContext(ctx, nil, "anvil_mine", "0x9"); err != nil {
		t.Fatal(err)
	}
	height, err := client.BlockNumber(ctx)
	if err != nil || height != 110 {
		t.Fatalf("actual head %d %v", height, err)
	}
	var oversized atomic.Bool
	oversized.Store(true)
	upstream := &http.Client{Timeout: 15 * time.Second}
	defer upstream.CloseIdleConnections()
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		if err != nil {
			http.Error(w, "transport", 502)
			return
		}
		var q struct {
			ID     json.RawMessage   `json:"id"`
			Method string            `json:"method"`
			Params []json.RawMessage `json:"params"`
		}
		if json.Unmarshal(body, &q) != nil {
			http.Error(w, "request", 400)
			return
		}
		if q.Method == "eth_getLogs" {
			var filter struct {
				From string `json:"fromBlock"`
				To   string `json:"toBlock"`
			}
			if len(q.Params) == 0 || json.Unmarshal(q.Params[0], &filter) != nil {
				http.Error(w, "filter", 400)
				return
			}
			from, _ := strconv.ParseUint(strings.TrimPrefix(filter.From, "0x"), 16, 64)
			to, _ := strconv.ParseUint(strings.TrimPrefix(filter.To, "0x"), 16, 64)
			if to-from >= 100 {
				_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": q.ID, "error": map[string]any{"code": -32005, "message": "block range capped at 100"}})
				return
			}
		}
		request, err := http.NewRequestWithContext(r.Context(), http.MethodPost, endpoint, bytes.NewReader(body))
		if err != nil {
			http.Error(w, "request", 502)
			return
		}
		request.Header.Set("Content-Type", "application/json")
		response, err := upstream.Do(request)
		if err != nil {
			http.Error(w, "upstream", 502)
			return
		}
		defer response.Body.Close()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(response.StatusCode)
		_, _ = io.Copy(w, response.Body)
		if q.Method == "eth_getLogs" && oversized.Load() {
			var filter map[string]string
			_ = json.Unmarshal(q.Params[0], &filter)
			if filter["fromBlock"] == "0x1" && filter["toBlock"] == "0x1" {
				_, _ = io.WriteString(w, strings.Repeat(" ", 512<<10))
			}
		}
	}))
	defer proxy.Close()
	genesis, err := client.HeaderByNumber(ctx, big.NewInt(0))
	if err != nil {
		t.Fatal(err)
	}
	options := CommittedOptions{Endpoints: map[string]string{"owned-anvil-125": proxy.URL}, GenesisHash: genesis.Hash(), ManifestDigest: manifest, Epochs: []AddressEpoch{{Address: token.Hex(), Role: "SidioraERC20", ABIRevision: revision, ManifestDigest: manifest, ActivationHeight: 1}}, MaxResponseBytes: 512 << 10}
	rpcSource, err := NewCommittedSource(ctx, options)
	if err != nil {
		t.Fatal(err)
	}
	defer rpcSource.Close()
	fallback, err := rpcSource.FetchCommitted(ctx, FetchOptions{FromBlock: 1, ToBlock: 1}, true, nil)
	if err != nil || !fallback.Complete || len(fallback.Logs) != 1 || rpcSource.Metrics().ReceiptFallbacks != 1 {
		t.Fatalf("real receipt fallback: %+v %v", rpcSource.Metrics(), err)
	}
	oversized.Store(false)
	before := rpcSource.Metrics()
	enriched, err := rpcSource.FetchCommitted(ctx, FetchOptions{FromBlock: 2, ToBlock: 101}, true, nil)
	after := rpcSource.Metrics()
	if err != nil || len(enriched.Logs) != 10000 || after.HeaderFetches-before.HeaderFetches > 100 || len(enriched.TxFromMap) != 0 {
		t.Fatalf("10000 real logs/100 headers: %+v %v", after, err)
	}
	canonical := FetchResult{ManifestDigest: manifest, Blocks: map[uint64]BlockRecord{}, BlockTimestamps: map[uint64]int64{}, TxFromMap: map[string]string{}}
	prefix, err := rpcSource.FetchCommitted(ctx, FetchOptions{FromBlock: 1, ToBlock: 110}, true, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = mergeFetch(&canonical, prefix); err != nil {
		t.Fatal(err)
	}
	canonical.Complete = true
	dsn, pool := internaltest.NewPostgres(t)
	if _, err = pool.Exec(ctx, canonicalExplorerSchema); err != nil {
		t.Fatal(err)
	}
	seedBlocks := func(from, to uint64) {
		blocks := [][]any{}
		transactions := [][]any{}
		logs := [][]any{}
		for height := from; height <= to; height++ {
			record := canonical.Blocks[height]
			blocks = append(blocks, []any{int64(height), record.Hash.Bytes(), record.ParentHash.Bytes(), time.Unix(record.TimestampSeconds, 0).UTC(), true})
			if record.TransactionCount > 0 {
				block, err := client.BlockByNumber(ctx, new(big.Int).SetUint64(height))
				if err != nil {
					t.Fatal(err)
				}
				for index, tx := range block.Transactions() {
					origin, err := ethtypes.Sender(ethtypes.LatestSignerForChainID(big.NewInt(125)), tx)
					if err != nil {
						t.Fatal(err)
					}
					transactions = append(transactions, []any{tx.Hash().Bytes(), record.Hash.Bytes(), int64(index), origin.Bytes()})
				}
			}
		}
		for _, log := range canonical.Logs {
			if log.BlockNumber < from || log.BlockNumber > to {
				continue
			}
			var topics [4]any
			for i, topic := range log.Topics {
				topics[i] = topic.Bytes()
			}
			logs = append(logs, []any{log.Address.Bytes(), topics[0], topics[1], topics[2], topics[3], append([]byte{}, log.Data...), log.TxHash.Bytes(), log.BlockHash.Bytes(), int64(log.BlockNumber), int64(log.Index), int64(log.TxIndex)})
		}
		for _, batch := range []struct {
			table   string
			columns []string
			rows    [][]any
		}{{"blocks", []string{"number", "hash", "parent_hash", "timestamp", "consensus"}, blocks}, {"transactions", []string{"hash", "block_hash", "index", "from_address_hash"}, transactions}, {"logs", []string{"address_hash", "first_topic", "second_topic", "third_topic", "fourth_topic", "data", "transaction_hash", "block_hash", "block_number", "index", "transaction_index"}, logs}} {
			if _, err = pool.CopyFrom(ctx, pgx.Identifier{"public", batch.table}, batch.columns, pgx.CopyFromRows(batch.rows)); err != nil {
				t.Fatal(err)
			}
		}
	}
	seedBlocks(1, 100)
	readerURL, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	readerURL.User = url.UserPassword("explorer_reader", "owned-fixture-reader")
	explorer, err := NewCommittedPaxscan(ctx, readerURL.String(), CommittedPaxscanOptions{SourceID: "owned-explorer", SchemaVersion: ExplorerSchemaV1, ExpectedRole: "explorer_reader", MaxConnections: 2, ManifestDigest: manifest, Epochs: options.Epochs, RPC: rpcSource})
	if err != nil {
		t.Fatal(err)
	}
	defer explorer.Close()
	history := &HistoricalSource{RPC: rpcSource, Explorer: explorer}
	combined, err := history.FetchHistorical(ctx, FetchOptions{FromBlock: 1, ToBlock: 110}, 0)
	if err != nil || !combined.Complete || combined.Heads.ExplorerIndexedHeight == nil || *combined.Heads.ExplorerIndexedHeight != 100 || combined.Heads.RPCCommittedHeight != 110 || len(combined.Blocks) != 110 || len(combined.Logs) != 10001 {
		t.Fatalf("explorer100 RPC suffix: %+v %v", combined.Heads, err)
	}
	if err = client.Client().CallContext(ctx, nil, "anvil_mine", fmtHeight(147)); err != nil {
		t.Fatal(err)
	}
	range257, err := rpcSource.FetchCommitted(ctx, FetchOptions{FromBlock: 1, ToBlock: 257}, true, nil)
	if err != nil || !range257.Complete || len(range257.Blocks) != 257 || len(range257.Logs) != 10001 || rpcSource.Metrics().RangeBisections == 0 {
		t.Fatalf("257 range: blocks=%d logs=%d err=%v", len(range257.Blocks), len(range257.Logs), err)
	}
	if err = client.Client().CallContext(ctx, nil, "anvil_mine", fmtHeight(10000-257)); err != nil {
		t.Fatal(err)
	}
	for from := int64(111); from <= 10000; from += 2000 {
		page, err := rpcSource.FetchCommitted(ctx, FetchOptions{FromBlock: from, ToBlock: min(from+1999, int64(10000))}, true, nil)
		if err != nil {
			t.Fatal(err)
		}
		if err = mergeFetch(&canonical, page); err != nil {
			t.Fatal(err)
		}
	}
	canonical.Complete = true
	seedBlocks(101, 10000)
	fetchExplorer := func(diagnostic bool) FetchResult {
		t.Helper()
		all := FetchResult{ManifestDigest: manifest, Blocks: map[uint64]BlockRecord{}, BlockTimestamps: map[uint64]int64{}, TxFromMap: map[string]string{}}
		for from := int64(1); from <= 10000; from += 2000 {
			var page FetchResult
			var err error
			if diagnostic {
				page, err = explorer.FetchParityLogs(ctx, FetchOptions{FromBlock: from, ToBlock: from + 1999})
			} else {
				page, err = explorer.FetchLogs(ctx, FetchOptions{FromBlock: from, ToBlock: from + 1999})
			}
			if err != nil {
				t.Fatal(err)
			}
			if err = mergeFetch(&all, page); err != nil {
				t.Fatal(err)
			}
		}
		all.Complete = true
		return all
	}
	explorerRows := fetchExplorer(false)
	report, err := CompareParity(1, 10000, ParityInput{"owned-rpc", "seconds", canonical}, ParityInput{"owned-explorer", "seconds", explorerRows})
	if err != nil || !report.Certified || report.EmptyBlockCount == 0 || report.HighLogBlockCount != 100 {
		t.Fatalf("real 10000 block parity: certified=%v blocks=%d high=%d err=%v", report.Certified, report.CompleteBlockCount, report.HighLogBlockCount, err)
	}
	if err = WriteParityReport(t.TempDir(), report); err != nil {
		t.Fatal(err)
	}
	if err = ValidateParityAdmission(&report, manifest, "owned-explorer"); err != nil {
		t.Fatal(err)
	}
	milliseconds := explorerRows
	milliseconds.Blocks = map[uint64]BlockRecord{}
	milliseconds.BlockTimestamps = map[uint64]int64{}
	for height, record := range explorerRows.Blocks {
		record.TimestampSeconds *= 1000
		milliseconds.Blocks[height] = record
		milliseconds.BlockTimestamps[height] = record.TimestampSeconds
	}
	normalized, err := CompareParity(1, 10000, ParityInput{"owned-rpc", "seconds", canonical}, ParityInput{"owned-explorer", "milliseconds", milliseconds})
	if err != nil || !normalized.Certified {
		t.Fatalf("explicit millisecond normalization: %v", err)
	}
	missing, duplicate := canonical.Logs[0], canonical.Logs[1]
	if _, err = pool.Exec(ctx, `DELETE FROM public.logs WHERE block_hash=$1 AND transaction_hash=$2 AND "index"=$3`, missing.BlockHash.Bytes(), missing.TxHash.Bytes(), int64(missing.Index)); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO public.logs SELECT * FROM public.logs WHERE block_hash=$1 AND transaction_hash=$2 AND "index"=$3`, duplicate.BlockHash.Bytes(), duplicate.TxHash.Bytes(), int64(duplicate.Index)); err != nil {
		t.Fatal(err)
	}
	corrupt := fetchExplorer(true)
	report, err = CompareParity(1, 10000, ParityInput{"owned-rpc", "seconds", canonical}, ParityInput{"owned-explorer", "seconds", corrupt})
	if err != nil || report.Certified || report.LeftCount != report.RightCount || len(report.Missing) != 1 || len(report.Duplicates) != 1 {
		t.Fatalf("equal-count missing/duplicate: missing=%d duplicate=%d %v", len(report.Missing), len(report.Duplicates), err)
	}
	mismatch := canonical.Logs[2]
	if _, err = pool.Exec(ctx, `UPDATE public.logs SET data=$1 WHERE transaction_hash=$2 AND "index"=$3`, []byte{1, 2, 3}, mismatch.TxHash.Bytes(), int64(mismatch.Index)); err != nil {
		t.Fatal(err)
	}
	report, err = CompareParity(1, 10000, ParityInput{"owned-rpc", "seconds", canonical}, ParityInput{"owned-explorer", "seconds", fetchExplorer(true)})
	if err != nil || len(report.Different) != 1 || report.Certified {
		t.Fatalf("real PG payload disagreement: differences=%d %v", len(report.Different), err)
	}
	if _, err = pool.Exec(ctx, `DELETE FROM public.logs WHERE transaction_hash=$1 AND "index"=$2`, duplicate.TxHash.Bytes(), int64(duplicate.Index)); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `UPDATE public.logs SET data=$1 WHERE transaction_hash=$2 AND "index"=$3`, mismatch.Data, mismatch.TxHash.Bytes(), int64(mismatch.Index)); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `UPDATE public.blocks SET timestamp=to_timestamp(0) WHERE number=50`); err != nil {
		t.Fatal(err)
	}
	if _, err = explorer.FetchLogs(ctx, FetchOptions{FromBlock: 1, ToBlock: 100}); err == nil {
		t.Fatal("zero timestamp completed")
	}
	if _, err = pool.Exec(ctx, `UPDATE public.blocks SET timestamp=to_timestamp($1) WHERE number=50`, canonical.Blocks[50].TimestampSeconds+1); err != nil {
		t.Fatal(err)
	}
	disagreements := explorer.Metrics().SampleDisagreements
	if _, err = explorer.FetchLogs(ctx, FetchOptions{FromBlock: 1, ToBlock: 100}); err == nil || explorer.Metrics().SampleDisagreements != disagreements+1 {
		t.Fatal("RPC sampled timestamp disagreement accepted")
	}
	if _, err = pool.Exec(ctx, `UPDATE public.blocks SET timestamp=to_timestamp($1) WHERE number=50`, canonical.Blocks[50].TimestampSeconds); err != nil {
		t.Fatal(err)
	}
	if _, err = NewCommittedPaxscan(ctx, dsn, CommittedPaxscanOptions{SourceID: "denied-writer", SchemaVersion: ExplorerSchemaV1, ExpectedRole: "kl", ManifestDigest: manifest, Epochs: options.Epochs, RPC: rpcSource}); err == nil {
		t.Fatal("write-capable explorer role accepted")
	}
	if _, err = NewCommittedPaxscan(ctx, readerURL.String(), CommittedPaxscanOptions{SourceID: "denied-schema", SchemaVersion: "unknown", ExpectedRole: "explorer_reader", ManifestDigest: manifest, Epochs: options.Epochs, RPC: rpcSource}); err == nil {
		t.Fatal("unapproved explorer schema accepted")
	}
}

func TestCommittedAdversarialRecoveryGenuineChain(t *testing.T) {
	ctx, client, endpoint := genuineHistoricalNode(t)
	var ignored any
	if err := client.Client().CallContext(ctx, &ignored, "anvil_mine", "0x80"); err != nil {
		t.Fatal(err)
	}
	genesis, err := client.HeaderByNumber(ctx, big.NewInt(0))
	if err != nil {
		t.Fatal(err)
	}
	var conflict, paused, stale, incorrectSample atomic.Bool
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		var request struct {
			Method string
			Params []json.RawMessage
		}
		if err = json.Unmarshal(body, &request); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		latest := request.Method == "eth_getBlockByNumber" && len(request.Params) > 0 && string(request.Params[0]) == `"latest"`
		if latest && paused.Load() {
			<-r.Context().Done()
			return
		}
		if latest && stale.Load() {
			var envelope map[string]json.RawMessage
			if err = json.Unmarshal(body, &envelope); err != nil {
				http.Error(w, err.Error(), 400)
				return
			}
			params := append([]json.RawMessage(nil), request.Params...)
			params[0] = json.RawMessage(`"0x70"`)
			envelope["params"], _ = json.Marshal(params)
			body, _ = json.Marshal(envelope)
		}
		upstream, err := http.NewRequestWithContext(r.Context(), http.MethodPost, endpoint, bytes.NewReader(body))
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		upstream.Header.Set("Content-Type", "application/json")
		response, err := http.DefaultClient.Do(upstream)
		if err != nil {
			http.Error(w, err.Error(), 502)
			return
		}
		defer response.Body.Close()
		data, err := io.ReadAll(response.Body)
		if err != nil {
			http.Error(w, err.Error(), 502)
			return
		}
		if latest && conflict.Load() || request.Method == "eth_getBlockByNumber" && len(request.Params) > 0 && string(request.Params[0]) == `"0x7f"` && incorrectSample.Load() {
			var envelope map[string]json.RawMessage
			var block map[string]json.RawMessage
			if err = json.Unmarshal(data, &envelope); err != nil {
				http.Error(w, err.Error(), 502)
				return
			}
			if err = json.Unmarshal(envelope["result"], &block); err != nil {
				http.Error(w, err.Error(), 502)
				return
			}
			block["hash"], _ = json.Marshal(common.HexToHash("0xbad").Hex())
			envelope["result"], _ = json.Marshal(block)
			data, _ = json.Marshal(envelope)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(response.StatusCode)
		_, _ = w.Write(data)
	}))
	defer proxy.Close()
	options := CommittedOptions{Endpoints: map[string]string{"healthy": endpoint, "adversarial": proxy.URL}, GenesisHash: genesis.Hash(), ManifestDigest: "genuine-recovery", Epochs: []AddressEpoch{{Address: "0x0000000000000000000000000000000000000001", Role: "emitter", ABIRevision: "1", ManifestDigest: "genuine-recovery"}}}
	s, err := NewCommittedSource(ctx, options)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	result, err := s.FetchCommitted(ctx, FetchOptions{FromBlock: 1, ToBlock: 128}, true, nil)
	if err != nil || !result.Complete {
		t.Fatalf("accepted range %v", err)
	}
	head, err := s.ObserveHead(ctx, 0)
	if err != nil || head.Height != 128 {
		t.Fatalf("head %+v %v", head, err)
	}
	if err = s.Ready(); err != nil {
		t.Fatal(err)
	}
	if err = s.ReportDurableHeight(time.Now().UTC(), 128); err != nil {
		t.Fatal(err)
	}
	paused.Store(true)
	started := time.Now()
	head, err = s.ObserveHead(ctx, 0)
	if err != nil || head.Height != 128 || time.Since(started) >= 500*time.Millisecond {
		t.Fatalf("healthy latency %s head %+v err %v", time.Since(started), head, err)
	}
	paused.Store(false)
	stale.Store(true)
	staleOnly, err := NewCommittedSource(ctx, options)
	if err != nil {
		t.Fatal(err)
	}
	defer staleOnly.Close()
	staleOnly.authoritative = head
	staleOnly.endpoints = staleOnly.endpoints[:1]
	staleHead, err := staleOnly.ObserveHead(ctx, 0)
	if err != nil || staleHead.Height != 128 {
		t.Fatalf("stale node lowered head %+v %v", staleHead, err)
	}
	staleRange, err := staleOnly.FetchCommitted(ctx, FetchOptions{FromBlock: 1, ToBlock: 2}, true, nil)
	if err != nil || !staleRange.Complete {
		t.Fatalf("stale historical range rejected %v", err)
	}
	stale.Store(false)
	conflict.Store(true)
	if _, err = s.ObserveHead(ctx, 0); err == nil {
		t.Fatal("same-height disagreement advanced head")
	}
	blocked, err := s.FetchCommitted(ctx, FetchOptions{FromBlock: 128, ToBlock: 128}, false, nil)
	if err == nil || blocked.Complete {
		t.Fatal("disputed boundary returned complete")
	}
	incidents := s.IntegrityIncidents()
	if len(incidents) != 1 || incidents[0].Height != 128 || incidents[0].AcceptedHash != result.Blocks[128].Hash {
		t.Fatalf("integrity incident %+v", incidents)
	}
	conflict.Store(false)
	incorrectSample.Store(true)
	if err = s.RejoinEndpoint(ctx, "adversarial"); err == nil {
		t.Fatal("incorrect sampled history admitted rejoin")
	}
	if err = s.Ready(); err == nil {
		t.Fatal("unresolved source was ready")
	}
	incorrectSample.Store(false)
	if err = s.RejoinEndpoint(ctx, "adversarial"); err != nil {
		t.Fatal(err)
	}
	for _, health := range s.EndpointHealth() {
		if health.SourceID == "adversarial" && health.RecoveryChecks != 128 {
			t.Fatalf("recovery checks %+v", health)
		}
	}
	if _, err = s.ObserveHead(ctx, 0); err == nil {
		t.Fatal("rejoin silently resolved operator-owned incident")
	}
	if s.accepted[128] != result.Blocks[128].Hash || s.authoritative.Height != 128 {
		t.Fatal("accepted history or authoritative head rolled back")
	}
}

func TestHistoricalSaturationPreservesLiveAndShutdownGenuineChain(t *testing.T) {
	ctx, client, endpoint := genuineHistoricalNode(t)
	var ignored any
	if err := client.Client().CallContext(ctx, &ignored, "anvil_mine", "0x2"); err != nil {
		t.Fatal(err)
	}
	genesis, err := client.HeaderByNumber(ctx, big.NewInt(0))
	if err != nil {
		t.Fatal(err)
	}
	arrived := make(chan struct{}, 16)
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		var request struct{ Method string }
		if err = json.Unmarshal(body, &request); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		if request.Method == "eth_getLogs" {
			arrived <- struct{}{}
			<-r.Context().Done()
			return
		}
		upstream, err := http.NewRequestWithContext(r.Context(), http.MethodPost, endpoint, bytes.NewReader(body))
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		upstream.Header.Set("Content-Type", "application/json")
		response, err := http.DefaultClient.Do(upstream)
		if err != nil {
			http.Error(w, err.Error(), 502)
			return
		}
		defer response.Body.Close()
		w.WriteHeader(response.StatusCode)
		_, _ = io.Copy(w, response.Body)
	}))
	defer proxy.Close()
	options := CommittedOptions{Endpoints: map[string]string{"historical": proxy.URL}, GenesisHash: genesis.Hash(), ManifestDigest: "genuine-saturation", Epochs: []AddressEpoch{{Address: "0x0000000000000000000000000000000000000001", Role: "emitter", ABIRevision: "1", ManifestDigest: "genuine-saturation"}}}
	sources := make([]*CommittedSource, 16)
	finished := make(chan error, 16)
	for i := range sources {
		sources[i], err = NewCommittedSource(ctx, options)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = sources[i].Close() })
	}
	for _, historical := range sources {
		go func(s *CommittedSource) {
			result, err := s.FetchCommitted(ctx, FetchOptions{FromBlock: 1, ToBlock: 2}, true, nil)
			if result.Complete {
				finished <- fmt.Errorf("interrupted range consumed")
				return
			}
			finished <- err
		}(historical)
	}
	for i := 0; i < 16; i++ {
		select {
		case <-arrived:
		case <-time.After(3 * time.Second):
			t.Fatal("historical permits did not saturate")
		}
	}
	if cap(processPermits)-cap(historicalPermits) != 48 {
		t.Fatal("default live reservation changed")
	}
	options.Endpoints = map[string]string{"live": endpoint}
	live, err := NewCommittedSource(ctx, options)
	if err != nil {
		t.Fatal(err)
	}
	defer live.Close()
	started := time.Now()
	result, err := live.FetchCommitted(ctx, FetchOptions{FromBlock: 1, ToBlock: 2}, false, nil)
	if err != nil || !result.Complete || time.Since(started) >= 500*time.Millisecond {
		t.Fatalf("live under saturation elapsed=%s complete=%v err=%v", time.Since(started), result.Complete, err)
	}
	started = time.Now()
	for _, s := range sources {
		if err = s.Close(); err != nil {
			t.Fatal(err)
		}
	}
	if time.Since(started) >= 20*time.Second {
		t.Fatal("shutdown exceeded bound")
	}
	for i := 0; i < 16; i++ {
		select {
		case err := <-finished:
			if err == nil {
				t.Fatal("cancelled range returned success")
			}
		case <-time.After(time.Second):
			t.Fatal("range goroutine survived shutdown")
		}
	}
	for _, s := range sources {
		if s.TransportMetrics().PendingRanges != 0 || s.TransportMetrics().InFlightRequests != 0 {
			t.Fatal("source shutdown did not drain")
		}
	}
}
