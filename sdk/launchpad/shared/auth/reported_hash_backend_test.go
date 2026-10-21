package auth

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/accounts"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/eth/ethconfig"
	"github.com/ethereum/go-ethereum/ethclient/simulated"
	"github.com/ethereum/go-ethereum/node"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/rpc"
)

func chain125Node(t *testing.T) string {
	t.Helper()
	probe, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := probe.Addr().(*net.TCPAddr).Port
	_ = probe.Close()
	chainConfig := *params.AllDevChainProtocolChanges
	chainConfig.ChainID = big.NewInt(125)
	backend := simulated.NewBackend(types.GenesisAlloc{}, func(nodeConf *node.Config, ethConf *ethconfig.Config) {
		nodeConf.HTTPHost = "127.0.0.1"
		nodeConf.HTTPPort = port
		nodeConf.HTTPModules = []string{"eth"}
		ethConf.Genesis.Config = &chainConfig
		ethConf.NetworkId = 125
	})
	t.Cleanup(func() { _ = backend.Close() })
	backend.Commit()
	return fmt.Sprintf("http://127.0.0.1:%d", port)
}

func signEOA(t *testing.T, message string) (string, string, string) {
	t.Helper()
	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	sig, err := crypto.Sign(accounts.TextHash([]byte(message)), key)
	if err != nil {
		t.Fatal(err)
	}
	sig[64] += 27
	other, _ := crypto.GenerateKey()
	bad, _ := crypto.Sign(accounts.TextHash([]byte(message)), other)
	bad[64] += 27
	return strings.ToLower(crypto.PubkeyToAddress(key.PublicKey).Hex()), "0x" + hex.EncodeToString(sig), "0x" + hex.EncodeToString(bad)
}

func verifyWithReportedHash(t *testing.T, endpoint string) {
	t.Helper()
	client, err := rpc.Dial(endpoint)
	if err != nil {
		t.Fatal(err)
	}
	backend := NewReportedHashBackend(client)
	defer backend.Close()
	message := "kindle.test wants you to sign in"
	wallet, good, bad := signEOA(t, message)
	if _, err = VerifyProof(context.Background(), backend, WalletProof{Kind: "eoa_eip191", Wallet: wallet, Signature: good}, message, nil); err != nil {
		t.Fatalf("eoa proof through reported-hash backend: %#v", err)
	}
	if _, err = VerifyProof(context.Background(), backend, WalletProof{Kind: "eoa_eip191", Wallet: wallet, Signature: bad}, message, nil); err == nil {
		t.Fatal("foreign signature accepted")
	}
}

// On a Keccak-hash chain (geth) the reported hash is Header.Hash(); the
// backend must keep working there unchanged.
func TestReportedHashBackendOnGethNode(t *testing.T) {
	endpoint := chain125Node(t)
	verifyWithReportedHash(t, endpoint)
	client, err := rpc.Dial(endpoint)
	if err != nil {
		t.Fatal(err)
	}
	backend := NewReportedHashBackend(client)
	defer backend.Close()
	block, err := backend.ReportedBlockByNumber(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if block.Hash != block.Header.Hash() {
		t.Fatalf("geth reports keccak hashes; got %s for header %s", block.Hash, block.Header.Hash())
	}
}

// consensusHash is the block hash the fake node reports for h. Like Paxeer's
// CometBFT-derived hash it differs from go-ethereum's recomputed h.Hash() but
// changes whenever the header content changes.
func consensusHash(h *types.Header) common.Hash {
	return crypto.Keccak256Hash([]byte("cometbft"), h.Hash().Bytes())
}

// cometNode is a JSON-RPC chain-125 node that reports consensusHash block
// hashes and, like Paxeer, answers any EIP-1898 {blockHash} read with
// "block not found by hash" (the recomputed hash names no block there).
type cometNode struct {
	mu       sync.Mutex
	header   *types.Header
	code     []byte
	callOut  []byte
	onRead   func()
	reads    atomic.Int64
	heights  []string
	byHashes atomic.Int64
}

func (n *cometNode) current() *types.Header {
	n.mu.Lock()
	defer n.mu.Unlock()
	return types.CopyHeader(n.header)
}

func (n *cometNode) replace(h *types.Header) {
	n.mu.Lock()
	n.header = h
	n.mu.Unlock()
}

func (n *cometNode) serve(t *testing.T) *rpc.Client {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID     json.RawMessage
			Method string
			Params []json.RawMessage
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
			return
		}
		reply := func(result any) {
			json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": result})
		}
		header := n.current()
		switch req.Method {
		case "eth_chainId":
			reply("0x7d")
		case "eth_getBlockByNumber":
			var tag string
			json.Unmarshal(req.Params[0], &tag)
			if tag != "latest" && tag != hexutil.EncodeBig(header.Number) {
				reply(nil)
				return
			}
			encoded, _ := json.Marshal(header)
			var out map[string]any
			json.Unmarshal(encoded, &out)
			out["hash"] = consensusHash(header).Hex()
			reply(out)
		case "eth_getCode", "eth_call":
			block := req.Params[len(req.Params)-1]
			if strings.Contains(string(block), "blockHash") {
				n.byHashes.Add(1)
				json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "error": map[string]any{"code": -32000, "message": "block not found by hash"}})
				return
			}
			var height string
			json.Unmarshal(block, &height)
			n.mu.Lock()
			n.heights = append(n.heights, height)
			n.mu.Unlock()
			n.reads.Add(1)
			if n.onRead != nil {
				n.onRead()
			}
			if req.Method == "eth_getCode" {
				reply(hexutil.Bytes(n.code))
			} else {
				reply(hexutil.Bytes(n.callOut))
			}
		default:
			t.Error("unexpected method", req.Method)
		}
	}))
	t.Cleanup(srv.Close)
	client, err := rpc.Dial(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(client.Close)
	return client
}

func paxeerLikeHeader(n int64) *types.Header {
	return &types.Header{Number: big.NewInt(n), Difficulty: big.NewInt(0), GasLimit: 100000000, Time: uint64(time.Now().Unix()), Extra: []byte{1}}
}

func erc1271Magic() []byte { return common.RightPadBytes([]byte{0x16, 0x26, 0xba, 0x7e}, 32) }

func TestVerifyContractPinsReportedHashAndReadsByHeight(t *testing.T) {
	header := paxeerLikeHeader(33963716)
	node := &cometNode{header: header, code: []byte{0x60, 0x80}, callOut: erc1271Magic()}
	backend := NewReportedHashBackend(node.serve(t))
	if consensusHash(header) == header.Hash() {
		t.Fatal("fixture must report a hash different from Header.Hash()")
	}
	wallet := "0x" + strings.Repeat("ab", 20)
	proof, err := VerifyContract(context.Background(), backend, wallet, "exact", "0x"+strings.Repeat("11", 65), nil)
	if err != nil {
		t.Fatalf("contract proof under reported hashes: %#v", err)
	}
	if proof.BlockHash != consensusHash(header).Hex() || proof.BlockHeight != 33963716 {
		t.Fatalf("proof must name the node-reported block: %+v", proof)
	}
	if node.byHashes.Load() != 0 || node.reads.Load() != 2 {
		t.Fatalf("reads must go by height: byHash=%d reads=%d", node.byHashes.Load(), node.reads.Load())
	}
	for _, height := range node.heights {
		if height != hexutil.EncodeBig(header.Number) {
			t.Fatalf("read addressed %s, want pinned height %s", height, hexutil.EncodeBig(header.Number))
		}
	}
	// The same wallet presented as an EOA is refused because it has code at
	// the pinned block.
	if _, err := VerifyProof(context.Background(), backend, WalletProof{Kind: "eoa_eip191", Wallet: wallet, Signature: "0x" + strings.Repeat("11", 65)}, "exact", nil); err == nil || err.(*VerificationError).Category != "proof_kind" {
		t.Fatalf("eoa with code: %#v", err)
	}
}

func TestVerifyEOAUnderReportedHashes(t *testing.T) {
	node := &cometNode{header: paxeerLikeHeader(500)}
	backend := NewReportedHashBackend(node.serve(t))
	message := "kindle.test wants you to sign in"
	wallet, good, bad := signEOA(t, message)
	if _, err := VerifyProof(context.Background(), backend, WalletProof{Kind: "eoa_eip191", Wallet: wallet, Signature: good}, message, nil); err != nil {
		t.Fatalf("eoa proof: %#v", err)
	}
	if _, err := VerifyProof(context.Background(), backend, WalletProof{Kind: "eoa_eip191", Wallet: wallet, Signature: bad}, message, nil); err == nil {
		t.Fatal("foreign signature accepted")
	}
	op := WalletOperationVerifier{Backend: backend}
	if err := op.Verify(context.Background(), wallet, message, good); err != nil {
		t.Fatalf("wallet operation: %#v", err)
	}
	if node.byHashes.Load() != 0 {
		t.Fatal("EIP-1898 by hash used")
	}
}

// A block identity change while a pinned read is in flight must fail the
// proof as a retryable dependency failure, never publish a result.
func TestVerifyContractRechecksReportedHashAfterReads(t *testing.T) {
	header := paxeerLikeHeader(700)
	node := &cometNode{header: header, code: []byte{0x60, 0x80}, callOut: erc1271Magic()}
	node.onRead = func() {
		changed := types.CopyHeader(header)
		changed.Extra = []byte{9}
		node.replace(changed)
	}
	backend := NewReportedHashBackend(node.serve(t))
	_, err := VerifyContract(context.Background(), backend, "0x"+strings.Repeat("cd", 20), "exact", "0x"+strings.Repeat("11", 65), nil)
	var failure *VerificationError
	if !errors.As(err, &failure) || failure.Category != "upstream" || !failure.Retryable {
		t.Fatalf("identity change during read: %#v", err)
	}
}

// committedBackend serves exactly the committed block by its reported hash:
// latest resolves to it, other heights are refused, and a node that stops
// reporting the committed hash fails the proof.
func TestCommittedBackendServesOnlyTheReportedCommittedBlock(t *testing.T) {
	header := paxeerLikeHeader(900)
	node := &cometNode{header: header, code: []byte{0x60, 0x80}, callOut: erc1271Magic()}
	upstream := NewReportedHashBackend(node.serve(t))
	pin, err := upstream.ReportedBlockByNumber(context.Background(), big.NewInt(900))
	if err != nil || pin.Hash != consensusHash(header) {
		t.Fatal(pin.Hash, err)
	}
	committed := committedBackend{ContractBackend: upstream, pin: pin}
	latest, err := committed.ReportedBlockByNumber(context.Background(), nil)
	if err != nil || latest.Hash != pin.Hash {
		t.Fatal("latest must resolve to the committed block", err)
	}
	if _, err := committed.ReportedBlockByNumber(context.Background(), big.NewInt(901)); !errors.Is(err, ErrScopeDenied) {
		t.Fatal("other height served", err)
	}
	if _, err := committed.CodeAt(context.Background(), common.Address{1}, big.NewInt(899)); !errors.Is(err, ErrScopeDenied) {
		t.Fatal("read at other height served", err)
	}
	proof, err := VerifyContract(context.Background(), committed, "0x"+strings.Repeat("ef", 20), "exact", "0x"+strings.Repeat("11", 65), nil)
	if err != nil || proof.BlockHash != pin.Hash.Hex() || proof.BlockHeight != 900 {
		t.Fatalf("committed contract proof: %+v %#v", proof, err)
	}
	changed := types.CopyHeader(header)
	changed.Extra = []byte{3}
	node.replace(changed)
	if _, err := committed.ReportedBlockByNumber(context.Background(), nil); !errors.Is(err, errPinnedBlockChanged) {
		t.Fatal("committed identity change not detected", err)
	}
	if _, err := VerifyContract(context.Background(), committed, "0x"+strings.Repeat("ef", 20), "exact", "0x"+strings.Repeat("11", 65), nil); err == nil {
		t.Fatal("proof accepted after committed identity changed")
	}
}

// Live chain-125 facts: block 33963716 is the creation block of pool
// livePool; the node reports livePoolCreationHash for it while go-ethereum
// recomputes livePoolCreationRecomputed from the same header.
var (
	livePoolCreationHeight     = big.NewInt(33963716)
	livePoolCreationHash       = common.HexToHash("0x81a18d822e327fbcbfe2be91b7f666bfa043016cb0e3f094c5bbabe91f9d192b")
	livePoolCreationRecomputed = common.HexToHash("0xe83bd786ee2456fe683b1cc6a7c4b9755783d09071d189ec20dc5a565c6f6292")
	livePool                   = common.HexToAddress("0xCE20Da50F92D21d8BeE497910c782E9Aa25b6263")
)

// TestReportedHashBackendOnPaxeer runs against the live chain-125 RPC when
// PAXEER_RPC_URL is set (e.g. https://api5.mainnet-beta.paxeer.network), where
// header.Hash() differs from the reported block hash. With
// PAXEER_ARCHIVE_RPC_URL (e.g. https://api2.mainnet-beta.paxeer.network) it
// also pins the historical pool-creation block by its reported hash.
func TestReportedHashBackendOnPaxeer(t *testing.T) {
	endpoint := os.Getenv("PAXEER_RPC_URL")
	if endpoint == "" {
		t.Skip("PAXEER_RPC_URL not set")
	}
	verifyWithReportedHash(t, endpoint)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	client, err := rpc.DialContext(ctx, endpoint)
	if err != nil {
		t.Fatal(err)
	}
	backend := NewReportedHashBackend(client)
	defer backend.Close()
	latest, err := backend.ReportedBlockByNumber(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if latest.Hash == latest.Header.Hash() {
		t.Fatal("expected Header.Hash() to differ from the node-reported hash on chain 125")
	}
	// A contract presented as an EOA is refused only if its code was read,
	// i.e. the pinned code read at the latest height succeeded.
	if _, err := VerifyProof(ctx, backend, WalletProof{Kind: "eoa_eip191", Wallet: strings.ToLower(livePool.Hex()), Signature: "0x" + strings.Repeat("11", 65)}, "x", nil); err == nil || err.(*VerificationError).Category != "proof_kind" {
		t.Fatalf("pool presented as EOA: %#v", err)
	}
	archiveURL := os.Getenv("PAXEER_ARCHIVE_RPC_URL")
	if archiveURL == "" {
		return
	}
	archiveClient, err := rpc.DialContext(ctx, archiveURL)
	if err != nil {
		t.Fatal(err)
	}
	archive := NewReportedHashBackend(archiveClient)
	defer archive.Close()
	pin, err := archive.ReportedBlockByNumber(ctx, livePoolCreationHeight)
	if err != nil {
		t.Fatal(err)
	}
	if pin.Hash != livePoolCreationHash || pin.Header.Hash() != livePoolCreationRecomputed {
		t.Fatalf("creation block identity: reported %s recomputed %s", pin.Hash, pin.Header.Hash())
	}
	if _, err := archive.client.CodeAtHash(ctx, livePool, livePoolCreationRecomputed); err == nil {
		t.Fatal("node served EIP-1898 by the recomputed header hash")
	}
	committed := committedBackend{ContractBackend: archive, pin: pin}
	var code []byte
	if err := readPinned(ctx, committed, pin, func(height *big.Int) error {
		var e error
		code, e = committed.CodeAt(ctx, livePool, height)
		return e
	}); err != nil || len(code) == 0 {
		t.Fatalf("pinned code read at the committed creation block: %d bytes %v", len(code), err)
	}
	t.Logf("block %s reported %s recomputed %s; pool code %d bytes", livePoolCreationHeight, pin.Hash.Hex(), pin.Header.Hash().Hex(), len(code))
}
