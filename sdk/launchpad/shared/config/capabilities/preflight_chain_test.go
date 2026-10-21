package capabilities

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Sidiora-Technologies/KindleLaunch/shared/config/deployment"
	interfaces "github.com/Sidiora-Technologies/KindleLaunch/shared/interfaces/v2"
	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/rpc"
)

// lockedBuffer collects a child process's stderr while the test reads it.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// blockClock hands out strictly increasing block timestamps that never exceed
// wall time. Anvil rejects a block whose timestamp is not greater than its
// parent's, and the preflight policy rejects future or stale blocks, so each
// timestamp is max(last+1, now-slack); if that would still be in the future
// (more blocks than the slack window absorbs), it waits for the wall clock.
type blockClock struct {
	last  int64
	slack int64
}

func (c *blockClock) next(ctx context.Context) int64 {
	next := max(c.last+1, time.Now().Unix()-c.slack)
	for time.Now().Unix() < next && ctx.Err() == nil {
		time.Sleep(10 * time.Millisecond)
	}
	c.last = next
	return next
}

type quoteArtifact struct {
	Runtime        string            `json:"runtime"`
	ImmutableNames map[string]string `json:"immutableNames"`
	Immutables     map[string][]struct {
		Start  int `json:"start"`
		Length int `json:"length"`
	} `json:"immutableReferences"`
}

func TestPinnedQuoteReadsActualQuoterPoolAndProtocolBytecode(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 25*time.Second)
	defer cancel()
	dir, err := os.MkdirTemp("", "kindle-preflight-chain-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	ipc := filepath.Join(dir, "rpc.ipc")
	genesis := time.Now().Unix() - 45
	command := exec.Command("anvil", "--host", "127.0.0.1", "--port", "0", "--ipc", ipc, "--chain-id", "125", "--timestamp", strconv.FormatInt(genesis, 10), "--accounts", "0", "--threads", "2", "--silent")
	command.Env = append(os.Environ(), "RAYON_NUM_THREADS=2")
	command.Stdout = io.Discard
	var stderr lockedBuffer
	command.Stderr = &stderr
	if err = command.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	exited := false
	t.Cleanup(func() {
		if !exited {
			_ = command.Process.Kill()
			<-done
		}
	})
	var client *rpc.Client
	for {
		client, err = rpc.DialIPC(ctx, ipc)
		if err == nil {
			break
		}
		select {
		case waitErr := <-done:
			exited = true
			t.Fatalf("anvil exited before serving IPC: %v\nstderr:\n%s", waitErr, stderr.String())
		default:
		}
		if ctx.Err() != nil {
			t.Fatalf("anvil IPC unavailable: %v\nstderr:\n%s", ctx.Err(), stderr.String())
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Cleanup(client.Close)
	// Anvil requires strictly increasing block timestamps. Every block this
	// fixture produces (about 40) takes its timestamp from one monotonic clock
	// that never runs ahead of wall time and stays within 40s behind it, well
	// inside the one-minute freshness policy the engine enforces.
	clock := &blockClock{last: genesis, slack: 40}
	reader, err := NewPinnedRPCReader(client)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := os.ReadFile("../deployment/testdata/evm-artifacts.json")
	if err != nil {
		t.Fatal(err)
	}
	var artifacts struct {
		Contracts map[string]quoteArtifact `json:"contracts"`
	}
	if err = json.Unmarshal(encoded, &artifacts); err != nil {
		t.Fatal(err)
	}
	quoter := common.HexToAddress("0x1000000000000000000000000000000000000001")
	pool := common.HexToAddress("0x1000000000000000000000000000000000000002")
	registry := common.HexToAddress("0x1000000000000000000000000000000000000003")
	protocol := common.HexToAddress("0x1000000000000000000000000000000000000004")
	trader := common.HexToAddress("0x1000000000000000000000000000000000000005")
	for name, address := range map[string]common.Address{"Quoter": quoter, "SidioraPool": pool, "PoolRegistry": registry, "ProtocolConfig": protocol} {
		artifact, ok := artifacts.Contracts[name]
		if !ok {
			t.Fatalf("actual %s artifact missing", name)
		}
		code := common.FromHex(artifact.Runtime)
		for _, references := range artifact.Immutables {
			for _, reference := range references {
				if reference.Length != 32 || reference.Start < 0 || reference.Start+32 > len(code) {
					t.Fatal("invalid artifact immutable range")
				}
				copy(code[reference.Start:reference.Start+32], common.LeftPadBytes(address.Bytes(), 32))
			}
		}
		if len(code) == 0 {
			t.Fatal("empty actual contract runtime")
		}
		if err = client.CallContext(ctx, nil, "anvil_setCode", address, hexutil.Bytes(code)); err != nil {
			t.Fatal(err)
		}
	}
	set := func(address common.Address, key, value common.Hash) {
		t.Helper()
		if err := client.CallContext(ctx, nil, "anvil_setStorageAt", address, key, value); err != nil {
			t.Fatal(err)
		}
	}
	word := func(n int64) common.Hash { return common.BigToHash(big.NewInt(n)) }
	set(quoter, word(1), common.BytesToHash(registry.Bytes()))
	set(quoter, word(2), common.BytesToHash(protocol.Bytes()))
	registered := crypto.Keccak256Hash(common.LeftPadBytes(pool.Bytes(), 32), common.LeftPadBytes(big.NewInt(5).Bytes(), 32))
	set(registry, registered, word(1))
	set(pool, word(5), common.BytesToHash(protocol.Bytes()))
	virtual := big.NewInt(10000000000)
	real := big.NewInt(100000000)
	tokens, _ := new(big.Int).SetString("1000000000000000000000000", 10)
	set(pool, word(7), common.BigToHash(virtual))
	set(pool, word(8), common.BigToHash(real))
	set(pool, word(9), common.BigToHash(tokens))
	for _, slot := range []int64{4, 5, 6} {
		set(protocol, word(slot), word(100))
	}
	mineOne := func() common.Hash {
		t.Helper()
		if err := client.CallContext(ctx, nil, "evm_setNextBlockTimestamp", clock.next(ctx)); err != nil {
			t.Fatal(err)
		}
		if err := client.CallContext(ctx, nil, "evm_mine"); err != nil {
			t.Fatal(err)
		}
		header, err := reader.HeaderByNumber(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		return header.Hash()
	}
	// Anvil applies anvil_setStorageAt to the state recorded for the current
	// head block, so a pinned block that is still the head would silently
	// absorb later fixture mutations. Mining one more block seals the pinned
	// block's state the way a real chain does: later mutations land on top.
	mine := func() common.Hash {
		t.Helper()
		pinned := mineOne()
		mineOne()
		return pinned
	}
	block := mine()
	quote, err := ReadPinnedQuote(ctx, reader, quoter, pool, trader, "1000000", true, block)
	if err != nil {
		t.Fatal(err)
	}
	effective := new(big.Int).Add(virtual, real)
	paidAfterFee := big.NewInt(990000)
	output := new(big.Int).Div(new(big.Int).Mul(tokens, paidAfterFee), new(big.Int).Add(effective, paidAfterFee))
	scale := new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil)
	beforePrice := new(big.Int).Div(new(big.Int).Mul(effective, scale), tokens)
	afterPrice := new(big.Int).Div(new(big.Int).Mul(new(big.Int).Add(effective, paidAfterFee), scale), new(big.Int).Sub(tokens, output))
	impact := new(big.Int).Div(new(big.Int).Mul(new(big.Int).Abs(new(big.Int).Sub(afterPrice, beforePrice)), big.NewInt(10000)), beforePrice)
	if quote.AmountOut != output.String() || quote.FeeAmount != "10000" || quote.PriceImpactBPS != impact.String() {
		t.Fatalf("actual tuple differs from independent integer oracle: %+v", quote)
	}
	set(pool, word(2), common.BytesToHash(trader.Bytes()))
	opticalBlock := mine()
	_, err = ReadPinnedQuote(ctx, reader, quoter, pool, trader, "1000000", true, opticalBlock)
	var unavailable Unavailable
	if !errors.As(err, &unavailable) || unavailable.ReasonCode != OpticalQuoteUnsupported {
		t.Fatalf("actual optical revert lost type: %v", err)
	}
	old, err := ReadPinnedQuote(ctx, reader, quoter, pool, trader, "1000000", true, block)
	if err != nil || old != quote {
		t.Fatalf("new optical state contaminated pinned historical quote: %+v %v", old, err)
	}
	set(pool, word(2), common.Hash{})
	set(pool, word(8), common.Hash{})
	zeroRealBlock := mine()
	_, err = ReadPinnedQuote(ctx, reader, quoter, pool, trader, "1000000000000000000", false, zeroRealBlock)
	if !errors.As(err, &unavailable) || unavailable.ReasonCode != UnexecutableQuote {
		t.Fatalf("virtual reserves authorized actual sell: %v", err)
	}
	requestData, err := encodeCall("quoteExactInput(address,uint256,bool)", pool, big.NewInt(1000000), true)
	if err != nil {
		t.Fatal(err)
	}
	actual, err := reader.CallContractAtHash(ctx, ethereum.CallMsg{From: trader, To: &quoter, Data: requestData}, block)
	if err != nil || len(actual) != 96 || new(big.Int).SetBytes(actual[:32]).String() != quote.AmountOut {
		t.Fatal("preflight differs from exact actual Quoter return")
	}
	manifest := deployment.Current()
	// Exercise the pre-handoff epoch explicitly; the shipped manifest is active.
	manifest.Status = "pending-beacon-acceptance"
	manifest.ActivationHeight = 1
	poolCode, err := reader.CodeAtHash(ctx, pool, block)
	if err != nil {
		t.Fatal(err)
	}
	engine, err := NewPreflightEngine(reader, manifest, crypto.Keccak256Hash(poolCode).Hex(), strings.Repeat("a", 64), time.Minute, 10*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	request := PreflightRequest{Family: Creation, ExpectedRevision: engine.Revision().RevisionSHA256, BlockHash: block, Trader: trader, Recipient: trader, CreationPath: "Router", Name: "Real Market", Symbol: "REAL"}
	blocked, err := engine.Preflight(ctx, request)
	if err != nil || blocked.Supported || blocked.ExecutionPermitted || blocked.Revision != engine.Revision() {
		t.Fatalf("recorded deployment incorrectly activated: %+v %v", blocked, err)
	}
	governanceIssue := false
	for _, issue := range blocked.Issues {
		if issue.Code == "manifest_governance_pending" && issue.Role == "PoolBeacon" {
			governanceIssue = true
		}
	}
	if !governanceIssue {
		t.Fatal("pending beacon lacks actionable target role")
	}
	assertActualPreflightDrift(t, ctx, client, reader, clock, artifacts.Contracts, pool, trader, mine, set)
	request.Name = "Changed Market"
	if _, err = engine.Revalidate(ctx, blocked, request); err == nil {
		t.Fatal("changed creation request reused old disclosure")
	}
}

func assertActualPreflightDrift(t *testing.T, ctx context.Context, client *rpc.Client, reader *PinnedRPCReader, clock *blockClock, artifacts map[string]quoteArtifact, pool, trader common.Address, mine func() common.Hash, set func(common.Address, common.Hash, common.Hash)) {
	t.Helper()
	manifest := deployment.Current()
	manifest.Status = "eligible"
	manifest.ActivationHeight = 1
	install := func(name string, address common.Address) []byte {
		t.Helper()
		artifact, ok := artifacts[name]
		if !ok {
			t.Fatalf("actual artifact missing: %s", name)
		}
		code := common.FromHex(artifact.Runtime)
		for id, references := range artifact.Immutables {
			value := common.LeftPadBytes(address.Bytes(), 32)
			if name == "SidioraERC20" {
				switch artifact.ImmutableNames[id] {
				case "decimals":
					value = common.LeftPadBytes(big.NewInt(6).Bytes(), 32)
				case "_cachedChainId":
					value = common.LeftPadBytes(big.NewInt(125).Bytes(), 32)
				case "_nameHash":
					value = crypto.Keccak256([]byte("USD Ledger"))
				case "_cachedDomainSeparator":
					typ, err := abi.NewType("bytes32", "", nil)
					if err != nil {
						t.Fatal(err)
					}
					uintType, err := abi.NewType("uint256", "", nil)
					if err != nil {
						t.Fatal(err)
					}
					addressType, err := abi.NewType("address", "", nil)
					if err != nil {
						t.Fatal(err)
					}
					arguments := abi.Arguments{{Type: typ}, {Type: typ}, {Type: typ}, {Type: uintType}, {Type: addressType}}
					encoded, err := arguments.Pack(crypto.Keccak256Hash([]byte("EIP712Domain(string name,string version,uint256 chainId,address verifyingContract)")), crypto.Keccak256Hash([]byte("USD Ledger")), crypto.Keccak256Hash([]byte("1")), big.NewInt(125), address)
					if err != nil {
						t.Fatal(err)
					}
					value = crypto.Keccak256(encoded)
				case "deploymentFactory":
					value = common.LeftPadBytes(trader.Bytes(), 32)
				default:
					t.Fatalf("unreviewed token immutable: %s", artifact.ImmutableNames[id])
				}
			}
			for _, reference := range references {
				if reference.Length != 32 || reference.Start < 0 || reference.Start+32 > len(code) {
					t.Fatal("immutable range malformed")
				}
				copy(code[reference.Start:reference.Start+32], value)
			}
		}
		if err := client.CallContext(ctx, nil, "anvil_setCode", address, hexutil.Bytes(code)); err != nil {
			t.Fatal(err)
		}
		return code
	}
	for name, role := range manifest.Roles {
		address := common.HexToAddress(role.Address)
		switch role.Kind {
		case "erc1967":
			proxy := install("UUPSProxy", address)
			implementation := install(name, common.HexToAddress(role.ImplementationAddress))
			if crypto.Keccak256Hash(proxy).Hex() != role.RuntimeHash || crypto.Keccak256Hash(implementation).Hex() != role.ImplementationHash {
				t.Fatalf("artifact identity mismatch: %s", name)
			}
			set(address, deployment.ImplementationSlot, common.BytesToHash(common.HexToAddress(role.ImplementationAddress).Bytes()))
		case "beacon":
			install("PoolBeacon", address)
			install("SidioraPool", common.HexToAddress(role.ImplementationAddress))
			set(address, common.Hash{}, common.BytesToHash(common.HexToAddress(role.ImplementationAddress).Bytes()))
			set(address, common.BigToHash(big.NewInt(1)), common.BytesToHash(common.HexToAddress(manifest.Governance.ExpectedBeaconOwner).Bytes()))
		case "direct":
			code := install("SidioraERC20", address)
			role.RuntimeHash = crypto.Keccak256Hash(code).Hex()
			manifest.Roles[name] = role
		default:
			t.Fatal("unexpected fixture role")
		}
	}
	word := func(n int64) common.Hash { return common.BigToHash(big.NewInt(n)) }
	address := func(name string) common.Address { return common.HexToAddress(manifest.Roles[name].Address) }
	set(address("ProtocolConfig"), word(1), common.BytesToHash(address("USDL").Bytes()))
	set(address("ProtocolConfig"), word(11), word(1000000))
	for _, slot := range []int64{4, 5, 6} {
		set(address("ProtocolConfig"), word(slot), word(100))
	}
	set(address("Quoter"), word(1), common.BytesToHash(address("PoolRegistry").Bytes()))
	set(address("Quoter"), word(2), common.BytesToHash(address("ProtocolConfig").Bytes()))
	registered := crypto.Keccak256Hash(common.LeftPadBytes(pool.Bytes(), 32), common.LeftPadBytes(big.NewInt(5).Bytes(), 32))
	set(address("PoolRegistry"), registered, word(1))
	poolCode := install("BeaconProxy", pool)
	set(pool, deployment.BeaconSlot, common.BytesToHash(address("PoolBeacon").Bytes()))
	for slot, role := range map[int64]string{1: "USDL", 24: "SidioraFactory", 5: "ProtocolConfig", 4: "EventEmitter"} {
		set(pool, word(slot), common.BytesToHash(address(role).Bytes()))
	}
	set(pool, word(2), common.Hash{})
	set(pool, word(7), word(10000000000))
	set(pool, word(8), word(100000000))
	set(pool, word(9), common.BigToHash(new(big.Int).Exp(big.NewInt(10), big.NewInt(24), nil)))
	engine, err := NewPreflightEngine(reader, manifest, crypto.Keccak256Hash(poolCode).Hex(), strings.Repeat("a", 64), time.Minute, 10*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	assertActualCreationFees(t, ctx, client, reader, clock, engine, manifest, install, set)
	block := mine()
	header, err := reader.HeaderByHash(ctx, block)
	if err != nil {
		t.Fatal(err)
	}
	assertMeasuredActualReads(t, ctx, engine, block, header.Number.Uint64(), pool, trader)
	publication, err := engine.Capabilities(ctx, block)
	if err != nil {
		t.Fatal(err)
	}
	for _, published := range publication.Families {
		if (published.Capability.Family == Creation || published.Capability.Family == PlainQuote) && (!published.Capability.Supported || published.Capability.OracleRequired) {
			t.Fatalf("core requires optional provider: %+v", published.Capability)
		}
		if published.Capability.Family == DisplayConversion && (published.Capability.Supported || published.Capability.ReasonCode != DisplayUnavailable) {
			t.Fatal("display outage fabricated price")
		}
	}
	request := PreflightRequest{Family: PlainQuote, ExpectedRevision: engine.Revision().RevisionSHA256, BlockHash: block, Trader: trader, Recipient: trader, Pool: pool, AmountIn: "1000000", IsBuy: true}
	previous, err := engine.Preflight(ctx, request)
	if err != nil || !previous.Supported || previous.Quote == nil {
		t.Fatalf("actual pinned plain preflight unavailable: %+v %v", previous, err)
	}
	duplicate, err := engine.Revalidate(ctx, previous, request)
	if err != nil || duplicate.ObservedRevisionSHA256 != previous.ObservedRevisionSHA256 {
		t.Fatalf("identical disclosure not idempotent: %v", err)
	}
	assertUnknownPoolAndOutage(t, ctx, client, engine, manifest, request, address("Quoter"))

	set(pool, word(8), common.Hash{})
	zeroSell := request
	zeroSell.BlockHash = mine()
	zeroSell.IsBuy = false
	zeroSell.AmountIn = "1000000000000000000"
	noLiquidity, err := engine.Preflight(ctx, zeroSell)
	if err != nil || noLiquidity.Supported || noLiquidity.ExecutionPermitted || noLiquidity.ReasonCode != UnexecutableQuote || noLiquidity.Pool == nil || noLiquidity.Pool.RealUSDLBalance != "0" || noLiquidity.Pool.RedeemableLiquidityRaw != "0" || noLiquidity.Pool.VirtualUSDLReserve == "0" {
		t.Fatalf("virtual funds misrepresented as spendable: %+v %v", noLiquidity, err)
	}
	set(pool, word(8), word(100000000))
	set(address("ProtocolConfig"), word(11), word(2000000))
	request.BlockHash = mine()
	changed, err := engine.RevalidateAt(ctx, previous, request)
	if err == nil || changed.ObservedRevisionSHA256 == previous.ObservedRevisionSHA256 {
		t.Fatal("fee drift reused preflight revision")
	}
	feePublication, err := engine.Capabilities(ctx, request.BlockHash)
	if err != nil || feePublication.ObservedRevisionSHA256 == publication.ObservedRevisionSHA256 {
		t.Fatal("fee drift reused capability publication")
	}
	for _, published := range feePublication.Families {
		if published.Capability.Family == Creation && published.Capability.Supported {
			t.Fatal("wrong fee activated creation")
		}
	}
	set(address("ProtocolConfig"), word(11), word(1000000))
	set(pool, word(2), common.BytesToHash(trader.Bytes()))
	request.BlockHash = mine()
	changed, err = engine.RevalidateAt(ctx, previous, request)
	if err == nil || changed.Supported || changed.ReasonCode != OpticalQuoteUnsupported || changed.ObservedRevisionSHA256 == previous.ObservedRevisionSHA256 {
		t.Fatalf("optical drift retained executable disclosure: %+v %v", changed, err)
	}
	set(pool, word(2), common.Hash{})
	set(address("Quoter"), deployment.ImplementationSlot, common.BytesToHash(common.HexToAddress(manifest.Roles["Router"].ImplementationAddress).Bytes()))
	request.BlockHash = mine()
	revoked, err := engine.Capabilities(ctx, request.BlockHash)
	if err != nil {
		t.Fatal(err)
	}
	for _, published := range revoked.Families {
		if published.Capability.Family == PlainQuote && published.Capability.Supported {
			t.Fatal("unknown Quoter implementation activated quotes")
		}
		if published.Capability.Family == Creation && !published.Capability.Supported {
			t.Fatal("Quoter revocation disabled independent creation")
		}
	}
	ctxCanceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err = engine.Capabilities(ctxCanceled, request.BlockHash); err == nil {
		t.Fatal("interrupted inspection published cached capabilities")
	}
}

func assertActualCreationFees(t *testing.T, ctx context.Context, client *rpc.Client, reader *PinnedRPCReader, clock *blockClock, engine *PreflightEngine, manifest deployment.Manifest, install func(string, common.Address) []byte, set func(common.Address, common.Hash, common.Hash)) {
	t.Helper()
	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	payer := crypto.PubkeyToAddress(key.PublicKey)
	address := func(name string) common.Address { return common.HexToAddress(manifest.Roles[name].Address) }
	if err := client.CallContext(ctx, nil, "anvil_setBalance", payer, hexutil.EncodeBig(new(big.Int).Exp(big.NewInt(10), big.NewInt(22), nil))); err != nil {
		t.Fatal(err)
	}
	balanceSlot := crypto.Keccak256Hash(common.LeftPadBytes(payer.Bytes(), 32), common.LeftPadBytes(big.NewInt(3).Bytes(), 32))
	set(address("USDL"), balanceSlot, common.BigToHash(big.NewInt(20000000)))
	set(address("USDL"), common.BigToHash(big.NewInt(2)), common.BigToHash(big.NewInt(20000000)))
	treasury := common.HexToAddress("0x2000000000000000000000000000000000000001")
	implementation := common.HexToAddress("0x2000000000000000000000000000000000000002")
	proxyCode := install("UUPSProxy", treasury)
	implementationCode := install("Treasury", implementation)
	set(treasury, deployment.ImplementationSlot, common.BytesToHash(implementation.Bytes()))
	treasuryIdentity := deployment.Role{Address: treasury.Hex(), Kind: "erc1967", RuntimeHash: crypto.Keccak256Hash(proxyCode).Hex(), ImplementationAddress: implementation.Hex(), ImplementationHash: crypto.Keccak256Hash(implementationCode).Hex()}
	send := func(to common.Address, signature string, args ...any) common.Hash {
		t.Helper()
		data, err := encodeCall(signature, args...)
		if err != nil {
			t.Fatal(err)
		}
		nonce, err := reader.PendingNonceAt(ctx, payer)
		if err != nil {
			t.Fatal(err)
		}
		price, err := reader.SuggestGasPrice(ctx)
		if err != nil {
			t.Fatal(err)
		}
		transaction, err := types.SignTx(types.NewTransaction(nonce, to, big.NewInt(0), 15000000, price, data), types.LatestSignerForChainID(big.NewInt(125)), key)
		if err != nil {
			t.Fatal(err)
		}
		if err := client.CallContext(ctx, nil, "evm_setNextBlockTimestamp", clock.next(ctx)); err != nil {
			t.Fatal(err)
		}
		if err := reader.SendTransaction(ctx, transaction); err != nil {
			t.Fatalf("actual fixture transaction %s: %v", signature, err)
		}
		for {
			receipt, err := reader.TransactionReceipt(ctx, transaction.Hash())
			if err == nil && receipt != nil {
				if receipt.Status != types.ReceiptStatusSuccessful {
					t.Fatalf("actual fixture reverted: %s tx=%s", signature, transaction.Hash())
				}
				return transaction.Hash()
			}
			if ctx.Err() != nil {
				t.Fatal(ctx.Err())
			}
			time.Sleep(time.Millisecond)
		}
	}
	send(address("EventEmitter"), "initialize(address)", payer)
	send(address("EventEmitter"), "reinitializeV2(address)", payer)
	send(address("ProtocolConfig"), "initialize(address,address,address)", address("USDL"), address("EventEmitter"), payer)
	send(treasury, "initialize(address,address)", address("EventEmitter"), payer)
	send(address("PoolRegistry"), "initialize(address,address)", address("EventEmitter"), payer)
	send(address("FeeAccumulator"), "initialize(address,address,address,address,address,address)", address("ProtocolConfig"), treasury, address("PoolRegistry"), address("EventEmitter"), address("USDL"), payer)
	send(address("SidioraNFT"), "initialize(string,string,address,address)", "Sidiora Pool NFT", "SIDNFT", address("EventEmitter"), payer)
	send(address("SidioraFactory"), "initialize(address,address,address,address,address,address,address,address,address)", address("PoolBeacon"), address("SidioraNFT"), address("PoolRegistry"), address("EventEmitter"), address("ProtocolConfig"), treasury, address("FeeAccumulator"), address("USDL"), payer)
	send(address("Router"), "initialize(address,address,address,address,address)", address("SidioraFactory"), address("PoolRegistry"), address("ProtocolConfig"), address("USDL"), payer)
	send(address("FeesRouter"), "initialize(address,address,address,address)", address("SidioraNFT"), address("FeeAccumulator"), address("PoolRegistry"), payer)
	for _, grant := range []struct {
		target    common.Address
		role      string
		recipient common.Address
	}{
		{address("PoolRegistry"), "FACTORY_ROLE", address("SidioraFactory")},
		{address("FeeAccumulator"), "FACTORY_ROLE", address("SidioraFactory")},
		{address("SidioraNFT"), "MINTER_ROLE", address("SidioraFactory")},
		{address("SidioraFactory"), "ROUTER_ROLE", address("Router")},
		{treasury, "DEPOSITOR_ROLE", address("Router")},
		{treasury, "DEPOSITOR_ROLE", address("SidioraFactory")},
	} {
		send(grant.target, "grantRole(bytes32,address)", crypto.Keccak256Hash([]byte(grant.role)), grant.recipient)
	}
	send(address("EventEmitter"), "setPoolRegistry(address)", address("PoolRegistry"))
	send(address("EventEmitter"), "setSidioraFactory(address)", address("SidioraFactory"))
	send(address("EventEmitter"), "setAuthorizedEmitter(address,bool)", address("SidioraNFT"), true)
	var nftSelector [4]byte
	copy(nftSelector[:], crypto.Keccak256([]byte("emitNftMint(uint256,address,address,uint8)"))[:4])
	send(address("EventEmitter"), "setTypedEmitter(address,bytes4,bool)", address("SidioraNFT"), nftSelector, true)
	for _, path := range []string{"Router", "SidioraFactory"} {
		send(address("USDL"), "approve(address,uint256)", address(path), big.NewInt(1000000))
		hash := send(address(path), "createMarket(string,string,uint8,address)", "Actual "+path+" Market", "REAL", uint8(0), common.Address{})
		reconciled, err := engine.ReconcileCreationFee(ctx, hash, treasuryIdentity)
		if err != nil || reconciled.Path != path || reconciled.FeeRaw != "1000000" || reconciled.PayerDebitRaw != "1000000" || reconciled.TreasuryCreditRaw != "1000000" || reconciled.TreasuryLedgerCreditRaw != "1000000" {
			t.Fatalf("actual %s fee reconciliation: %+v %v", path, reconciled, err)
		}
		again, err := engine.ReconcileCreationFee(ctx, hash, treasuryIdentity)
		if err != nil || again != reconciled {
			t.Fatal("duplicate receipt reconciliation changed fee")
		}
		wrong := treasuryIdentity
		wrong.ImplementationHash = manifest.Roles["Router"].ImplementationHash
		if _, err := engine.ReconcileCreationFee(ctx, hash, wrong); err == nil {
			t.Fatal("unauthorized treasury implementation admitted")
		}
	}
}

// outageRPC is a JSON-RPC HTTP endpoint in front of the real anvil node. While
// a needle is set, any request whose body names it (the pool address appears
// in eth_getCode and in the isRegisteredPool calldata) is answered with HTTP
// 503, the way a failing upstream RPC answers; every other request is relayed
// to the chain unchanged.
type outageRPC struct {
	client *rpc.Client
	needle atomic.Value
}

func (o *outageRPC) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "read", http.StatusBadRequest)
		return
	}
	if needle, _ := o.needle.Load().(string); needle != "" && bytes.Contains(bytes.ToLower(body), []byte(needle)) {
		http.Error(w, "upstream unavailable", http.StatusServiceUnavailable)
		return
	}
	var request struct {
		ID     json.RawMessage   `json:"id"`
		Method string            `json:"method"`
		Params []json.RawMessage `json:"params"`
	}
	if err := json.Unmarshal(body, &request); err != nil {
		http.Error(w, "batch or malformed request", http.StatusBadRequest)
		return
	}
	params := make([]any, len(request.Params))
	for i, param := range request.Params {
		params[i] = param
	}
	var result json.RawMessage
	response := map[string]any{"jsonrpc": "2.0", "id": request.ID}
	if err := o.client.CallContext(r.Context(), &result, request.Method, params...); err != nil {
		failure := map[string]any{"code": -32000, "message": err.Error()}
		var data rpc.DataError
		if errors.As(err, &data) {
			failure["data"] = data.ErrorData()
		}
		response["error"] = failure
	} else if len(result) == 0 {
		response["result"] = nil
	} else {
		response["result"] = result
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(response)
}

// servePreflight runs the engine's HTTP handler and, on 200, holds the body to
// the frozen v2 contract the way core/api does (projection onto
// ConnectedCapabilitiesPreflight, then ValidateJSON).
func servePreflight(t *testing.T, engine *PreflightEngine, request PreflightRequest) (int, []byte) {
	t.Helper()
	body, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	engine.Handler(request.Family).ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body)))
	if recorder.Code != http.StatusOK {
		return recorder.Code, recorder.Body.Bytes()
	}
	var declared interfaces.ConnectedCapabilitiesPreflight
	if err := json.Unmarshal(recorder.Body.Bytes(), &declared); err != nil {
		t.Fatal(err)
	}
	projected, err := json.Marshal(declared)
	if err != nil {
		t.Fatal(err)
	}
	if err := interfaces.ValidateJSON("ConnectedCapabilitiesPreflight", projected); err != nil {
		t.Fatalf("preflight body violates v2 contract: %v %s", err, projected)
	}
	return recorder.Code, projected
}

// assertUnknownPoolAndOutage drives the real engine over the actual
// PoolRegistry bytecode. A pool the registry does not list (the zero address,
// an address without code, or a non-pool contract) is a deterministic blocked
// preflight with reasonCode pool_unknown; an RPC failure while reading the
// pool stays an error, which the handler reports as preflight_unavailable.
func assertUnknownPoolAndOutage(t *testing.T, ctx context.Context, client *rpc.Client, engine *PreflightEngine, manifest deployment.Manifest, registered PreflightRequest, nonPool common.Address) {
	t.Helper()
	for _, family := range []Family{Trading, PlainQuote} {
		for _, unknown := range []common.Address{{}, common.HexToAddress("0x00000000000000000000000000000000000dead1"), nonPool} {
			request := registered
			request.Family = family
			request.Pool = unknown
			result, err := engine.Preflight(ctx, request)
			if err != nil || result.Supported || result.ExecutionPermitted || result.ReasonCode != PoolUnknown || result.Pool != nil || result.Quote != nil || !result.Capability.Supported {
				t.Fatalf("%s unknown pool %s: %+v %v", family, unknown.Hex(), result, err)
			}
			again, err := engine.Preflight(ctx, request)
			if err != nil || again.ReasonCode != PoolUnknown || again.ObservedRevisionSHA256 != result.ObservedRevisionSHA256 || again.RequestSHA256 != result.RequestSHA256 {
				t.Fatalf("%s unknown pool outcome not deterministic: %+v %v", family, again, err)
			}
			status, body := servePreflight(t, engine, request)
			var decoded struct {
				Supported          bool   `json:"supported"`
				ExecutionPermitted bool   `json:"executionPermitted"`
				ReasonCode         string `json:"reasonCode"`
			}
			if status != http.StatusOK || json.Unmarshal(body, &decoded) != nil || decoded.Supported || decoded.ExecutionPermitted || decoded.ReasonCode != "pool_unknown" {
				t.Fatalf("%s unknown pool over HTTP: %d %s", family, status, body)
			}
		}
	}
	// Deterministic refusals of the request itself are blocked 200 results
	// with a stable reasonCode, never the retryable dependency error.
	zeroTrader := registered
	zeroTrader.Trader = common.Address{}
	badAmount := registered
	badAmount.AmountIn = "not-a-number"
	emptyName := registered
	emptyName.Family, emptyName.Pool, emptyName.AmountIn, emptyName.CreationPath = Creation, common.Address{}, "", "Router"
	missingNFT := registered
	missingNFT.Family, missingNFT.Pool, missingNFT.AmountIn, missingNFT.NFTID = NFTFees, common.Address{}, "", "999999"
	for _, refusal := range []struct {
		request PreflightRequest
		reason  string
	}{{zeroTrader, "preflight_context_invalid"}, {badAmount, "amount_invalid"}, {emptyName, "creation_metadata_invalid"}, {missingNFT, "claim_owner_mismatch"}} {
		if _, err := engine.Preflight(ctx, refusal.request); err == nil {
			t.Fatalf("%s: Preflight accepted a refused request", refusal.reason)
		}
		status, body := servePreflight(t, engine, refusal.request)
		var decoded struct {
			Supported          bool               `json:"supported"`
			ExecutionPermitted bool               `json:"executionPermitted"`
			ReasonCode         string             `json:"reasonCode"`
			Issues             []deployment.Issue `json:"issues"`
		}
		if status != http.StatusOK || json.Unmarshal(body, &decoded) != nil || decoded.Supported || decoded.ExecutionPermitted || decoded.ReasonCode != refusal.reason || len(decoded.Issues) == 0 || decoded.Issues[len(decoded.Issues)-1].Code != refusal.reason {
			t.Fatalf("%s refusal over HTTP: %d %s", refusal.reason, status, body)
		}
	}
	// The registered pool over a relay that fails every read naming it: the
	// failure must surface as the retryable error, never as pool_unknown.
	relay := &outageRPC{client: client}
	server := httptest.NewServer(relay)
	t.Cleanup(server.Close)
	relayed, err := rpc.DialContext(ctx, server.URL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(relayed.Close)
	reader, err := NewPinnedRPCReader(relayed)
	if err != nil {
		t.Fatal(err)
	}
	outageEngine, err := NewPreflightEngine(reader, manifest, engine.poolRuntime.Hex(), strings.Repeat("a", 64), time.Minute, 10*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	request := registered
	request.ExpectedRevision = outageEngine.Revision().RevisionSHA256
	if healthy, err := outageEngine.Preflight(ctx, request); err != nil || !healthy.Supported || healthy.Quote == nil {
		t.Fatalf("relay altered healthy preflight: %+v %v", healthy, err)
	}
	relay.needle.Store(strings.ToLower(registered.Pool.Hex()[2:]))
	for _, family := range []Family{Trading, PlainQuote} {
		request.Family = family
		result, err := outageEngine.Preflight(ctx, request)
		var issue deployment.Issue
		if err == nil || !errors.As(err, &issue) || issue.Code != "pool_registry_read_failed" || result.ReasonCode == PoolUnknown {
			t.Fatalf("%s RPC outage misreported: %+v %v", family, result, err)
		}
		status, body := servePreflight(t, outageEngine, request)
		var envelope deployment.Issue
		if status != http.StatusUnprocessableEntity || json.Unmarshal(body, &envelope) != nil || envelope.Code != "preflight_unavailable" {
			t.Fatalf("%s RPC outage over HTTP: %d %s", family, status, body)
		}
	}
	relay.needle.Store("")
}
