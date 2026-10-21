package chainreader

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Sidiora-Technologies/KindleLaunch/core/indexer/internal/decode"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
)

func TestHistoricalUint256ArgumentBounds(t *testing.T) {
	for _, v := range []*big.Int{nil, big.NewInt(-1), new(big.Int).Lsh(big.NewInt(1), 256)} {
		if _, err := uint256Argument(v); err == nil {
			t.Fatal("invalid uint256 accepted")
		}
	}
	max := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1))
	b, err := uint256Argument(max)
	if err != nil || len(b) != 32 || new(big.Int).SetBytes(b).Cmp(max) != 0 {
		t.Fatal("uint256 precision lost")
	}
}

func TestHistoricalMissingAuthorityHasPinnedUnavailableResult(t *testing.T) {
	var reader *HistoricalReader
	hash := common.HexToHash("0x1234")
	d := HistoricalDeployment{Role: "SidioraERC20", Address: common.HexToAddress("0xabcd"), DeploymentEpoch: "deployment-1", CodeDigest: common.HexToHash("0x4567")}
	out, err := reader.read(context.Background(), d, hash, "decimals()", nil)
	if err == nil || out.Status != "unavailable" || out.Value != nil || out.BlockHash != hash.Hex() || out.Method != "decimals()" || out.AttemptedAt.IsZero() || out.CodeDigest != d.CodeDigest.Hex() {
		t.Fatalf("missing authority represented as success: %#v %v", out, err)
	}
	if _, err := NewHistoricalReader(context.Background(), nil); err == nil {
		t.Fatal("missing RPC accepted")
	}
	native := NativePAXUnit()
	if native.Decimals != 18 || native.Kind != "native" || native.Address != nil {
		t.Fatal("native PAX conflated with ERC20")
	}
}

func TestHistoricalRoleGuardsDoNotDispatchByTransferTopic(t *testing.T) {
	var reader *HistoricalReader
	hash := common.HexToHash("0x1234")
	token := HistoricalDeployment{Role: "SidioraERC20", Address: common.HexToAddress("0xabcd"), DeploymentEpoch: "token-epoch", CodeDigest: common.HexToHash("0x123"), Kind: "direct"}
	result, err := reader.OwnerOf(context.Background(), token, hash, big.NewInt(1))
	if err == nil || result.Status != "unavailable" || result.BlockHash != hash.Hex() || result.Method != "ownerOf(uint256)" || result.AttemptedAt.IsZero() {
		t.Fatal("ERC20 treated as ERC721", result, err)
	}
	nft := token
	nft.Role = "SidioraNFT"
	result, err = reader.Decimals(context.Background(), nft, hash)
	if err == nil || result.Status != "unavailable" || result.BlockHash != hash.Hex() || result.Method != "decimals()" || result.AttemptedAt.IsZero() {
		t.Fatal("ERC721 treated as ERC20", result, err)
	}
}

type adversarialChain struct {
	ctx      context.Context
	client   *ethclient.Client
	endpoint string
	auth     *bind.TransactOpts
	owner    common.Address
}

func actualAdversarialChain(t *testing.T) *adversarialChain {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	t.Cleanup(cancel)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()
	output, err := os.Create(filepath.Join(t.TempDir(), "anvil.log"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = output.Close() })
	command := exec.CommandContext(ctx, "anvil", "--host", "127.0.0.1", "--port", strconv.Itoa(port), "--threads", "2", "--chain-id", "125", "--accounts", "1", "--silent")
	command.Stdout, command.Stderr = output, output
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
	timer := time.NewTicker(20 * time.Millisecond)
	defer timer.Stop()
	for {
		id, err := client.ChainID(ctx)
		if err == nil && id.Cmp(big.NewInt(125)) == 0 {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("actual chain125 unavailable", ctx.Err())
		case <-timer.C:
		}
	}
	key, err := crypto.HexToECDSA("ac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80")
	if err != nil {
		t.Fatal(err)
	}
	auth, err := bind.NewKeyedTransactorWithChainID(key, big.NewInt(125))
	if err != nil {
		t.Fatal(err)
	}
	auth.Context = ctx
	return &adversarialChain{ctx, client, endpoint, auth, crypto.PubkeyToAddress(key.PublicKey)}
}

func actualArtifact(t *testing.T, name string) (abi.ABI, []byte) {
	t.Helper()
	root := filepath.Join("..", "..", "..", "..", "..", "contracts", "launchpad", "artifacts", "contracts")
	var files []string
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() && entry.Name() == name+".json" {
			files = append(files, path)
		}
		return nil
	})
	if err != nil || len(files) != 1 {
		t.Fatalf("actual unique artifact %s: %v %v", name, files, err)
	}
	data, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatal(err)
	}
	var artifact struct {
		ABI      json.RawMessage
		Bytecode string
	}
	if err = json.Unmarshal(data, &artifact); err != nil {
		t.Fatal(err)
	}
	parsed, err := abi.JSON(bytes.NewReader(artifact.ABI))
	if err != nil {
		t.Fatal(err)
	}
	code := common.FromHex(artifact.Bytecode)
	if len(code) == 0 {
		t.Fatal("actual deployment bytecode unavailable", name)
	}
	return parsed, code
}
func (c *adversarialChain) deploy(t *testing.T, name string, arguments ...any) (common.Address, *bind.BoundContract, abi.ABI, *ethtypes.Receipt) {
	t.Helper()
	parsed, code := actualArtifact(t, name)
	address, tx, contract, err := bind.DeployContract(c.auth, parsed, code, c.client, arguments...)
	if err != nil {
		t.Fatal(name, err)
	}
	receipt := c.mined(t, tx)
	return address, contract, parsed, receipt
}
func (c *adversarialChain) mined(t *testing.T, tx *ethtypes.Transaction) *ethtypes.Receipt {
	t.Helper()
	receipt, err := bind.WaitMined(c.ctx, c.client, tx)
	if err != nil || receipt == nil || receipt.Status != 1 {
		t.Fatalf("actual mined transaction %s: %v", tx.Hash(), err)
	}
	return receipt
}
func (c *adversarialChain) transact(t *testing.T, contract *bind.BoundContract, method string, arguments ...any) *ethtypes.Receipt {
	t.Helper()
	tx, err := contract.Transact(c.auth, method, arguments...)
	if err != nil {
		t.Fatal(method, err)
	}
	return c.mined(t, tx)
}
func (c *adversarialChain) proxy(t *testing.T, name string, arguments ...any) (common.Address, *bind.BoundContract, abi.ABI, HistoricalDeployment) {
	t.Helper()
	implementation, _, parsed, _ := c.deploy(t, name)
	input, err := parsed.Pack("initialize", arguments...)
	if err != nil {
		t.Fatal(err)
	}
	address, _, _, receipt := c.deploy(t, "ERC1967Proxy", implementation, input)
	contract := bind.NewBoundContract(address, parsed, c.client, c.client, c.client)
	code, err := c.client.CodeAtHash(c.ctx, address, receipt.BlockHash)
	if err != nil {
		t.Fatal(err)
	}
	implCode, err := c.client.CodeAtHash(c.ctx, implementation, receipt.BlockHash)
	if err != nil {
		t.Fatal(err)
	}
	authority := HistoricalDeployment{Address: address, Role: name, DeploymentEpoch: receipt.TxHash.Hex(), Kind: "erc1967", CodeDigest: crypto.Keccak256Hash(code), Implementation: implementation, ImplementationDigest: crypto.Keccak256Hash(implCode)}
	return address, contract, parsed, authority
}

func deployRuntime(t *testing.T, c *adversarialChain, runtime []byte) (common.Address, *ethtypes.Receipt) {
	t.Helper()
	if len(runtime) > 255 {
		t.Fatal("fixture runtime too large")
	}
	init := []byte{0x60, byte(len(runtime)), 0x60, 0x0c, 0x60, 0x00, 0x39, 0x60, byte(len(runtime)), 0x60, 0x00, 0xf3}
	parsed, err := abi.JSON(strings.NewReader("[]"))
	if err != nil {
		t.Fatal(err)
	}
	address, tx, _, err := bind.DeployContract(c.auth, parsed, append(init, runtime...), c.client)
	if err != nil {
		t.Fatal(err)
	}
	return address, c.mined(t, tx)
}

func TestActualHostileMetadataIsBoundedAndRecoverable(t *testing.T) {
	c := actualAdversarialChain(t)
	hostile, receipt := deployRuntime(t, c, []byte{0x62, 0x02, 0x00, 0x00, 0x60, 0x00, 0xf3})
	code, err := c.client.CodeAtHash(c.ctx, hostile, receipt.BlockHash)
	if err != nil {
		t.Fatal(err)
	}
	d := HistoricalDeployment{Address: hostile, Role: "SidioraERC20", DeploymentEpoch: receipt.TxHash.Hex(), Kind: "direct", CodeDigest: crypto.Keccak256Hash(code)}
	reader, err := NewBoundedHistoricalReader(c.ctx, c.endpoint)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	first, err := reader.Decimals(c.ctx, d, receipt.BlockHash)
	if err == nil || first.Status != "unavailable" || first.Value != nil || !strings.Contains(first.Failure, "byte cap") {
		t.Fatal("hostile metadata did not reach bounded real transport", first, err)
	}
	started := time.Now()
	var workers sync.WaitGroup
	for i := 0; i < 32; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			out, err := reader.Decimals(c.ctx, d, receipt.BlockHash)
			if err == nil || out.Status != "unavailable" || out.Value != nil || out.BlockHash != receipt.BlockHash.Hex() {
				t.Errorf("hostile enrichment promoted: %#v %v", out, err)
			}
		}()
	}
	workers.Wait()
	if time.Since(started) > 3*time.Second {
		t.Fatal("hostile readers exhausted bounded admission")
	}
	token, _, _, creation := c.deploy(t, "SidioraERC20", "Recoverable", "RCV", big.NewInt(1000000), c.owner)
	code, err = c.client.CodeAtHash(c.ctx, token, creation.BlockHash)
	if err != nil {
		t.Fatal(err)
	}
	normal := HistoricalDeployment{Address: token, Role: "SidioraERC20", DeploymentEpoch: creation.TxHash.Hex(), Kind: "direct", CodeDigest: crypto.Keccak256Hash(code)}
	out, err := reader.Decimals(c.ctx, normal, creation.BlockHash)
	if err != nil || out.Status != "available" || out.Value == nil || *out.Value != "6" {
		t.Fatal("healthy metadata cannot recover", out, err)
	}
	out, err = reader.Decimals(c.ctx, normal, common.HexToHash("0xdead"))
	if err == nil || out.Status != "unavailable" || out.Value != nil || out.BlockHash != common.HexToHash("0xdead").Hex() {
		t.Fatal("missing historical block substituted cached or latest decimals", out, err)
	}
}

func TestActualNFTOwnerRemainsPinnedAfterTwoTransfers(t *testing.T) {
	c := actualAdversarialChain(t)
	_, nft, _, d := c.proxy(t, "SidioraNFT", "Historical NFT", "HNFT", common.Address{}, c.owner)
	c.transact(t, nft, "grantRole", crypto.Keccak256Hash([]byte("MINTER_ROLE")), c.owner)
	pool := common.HexToAddress("0x0000000000000000000000000000000000000123")
	mint := c.transact(t, nft, "mint", c.owner, pool, uint8(0))
	second := common.HexToAddress("0x0000000000000000000000000000000000000222")
	third := common.HexToAddress("0x0000000000000000000000000000000000000333")
	c.transact(t, nft, "approve", c.owner, big.NewInt(1))
	c.transact(t, nft, "transferFrom", c.owner, second, big.NewInt(1))
	if err := c.client.Client().CallContext(c.ctx, nil, "anvil_impersonateAccount", second); err != nil {
		t.Fatal(err)
	}
	if err := c.client.Client().CallContext(c.ctx, nil, "anvil_setBalance", second, "0xde0b6b3a7640000"); err != nil {
		t.Fatal(err)
	}
	data, err := actualNFTTransferData(t, second, third)
	if err != nil {
		t.Fatal(err)
	}
	var hash common.Hash
	if err := c.client.Client().CallContext(c.ctx, &hash, "eth_sendTransaction", map[string]any{"from": second.Hex(), "to": d.Address.Hex(), "data": "0x" + common.Bytes2Hex(data), "gas": "0x30d40"}); err != nil {
		t.Fatal(err)
	}
	tx, _, err := c.client.TransactionByHash(c.ctx, hash)
	if err != nil {
		t.Fatal(err)
	}
	last := c.mined(t, tx)
	reader, err := NewBoundedHistoricalReader(c.ctx, c.endpoint)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	old, err := reader.OwnerOf(c.ctx, d, mint.BlockHash, big.NewInt(1))
	if err != nil || old.Value == nil || *old.Value != strings.ToLower(c.owner.Hex()) {
		t.Fatal("mint owner lost", old, err)
	}
	latest, err := reader.OwnerOf(c.ctx, d, last.BlockHash, big.NewInt(1))
	if err != nil || latest.Value == nil || *latest.Value != strings.ToLower(third.Hex()) {
		t.Fatal("last owner incorrect", latest, err)
	}
}
func actualNFTTransferData(t *testing.T, from, to common.Address) ([]byte, error) {
	parsed, _ := actualArtifact(t, "SidioraNFT")
	return parsed.Pack("transferFrom", from, to, big.NewInt(1))
}

func TestActualRoutedBuyKeepsThreeActorsAndUnknownRevisions(t *testing.T) {
	c := actualAdversarialChain(t)
	supply := big.NewInt(1000000000000000)
	token, tokenContract, _, _ := c.deploy(t, "SidioraERC20", "Routed Asset", "ROUTE", supply, c.owner)
	usdl, usdlContract, _, _ := c.deploy(t, "SidioraERC20", "USD Ledger", "USDL", supply, c.owner)
	emitter, emitterContract, _, _ := c.proxy(t, "EventEmitter", c.owner)
	c.transact(t, emitterContract, "reinitializeV2", c.owner)
	registryAddress, poolRegistry, _, _ := c.proxy(t, "PoolRegistry", common.Address{}, c.owner)
	configAddress, _, _, _ := c.proxy(t, "ProtocolConfig", usdl, common.Address{}, c.owner)
	treasuryAddress, treasury, _, _ := c.proxy(t, "Treasury", common.Address{}, c.owner)
	accumulatorAddress, accumulator, _, _ := c.proxy(t, "FeeAccumulator", configAddress, treasuryAddress, registryAddress, common.Address{}, usdl, c.owner)
	pool, _, poolABI, _ := c.proxy(t, "SidioraPool", token, usdl, common.Address{}, accumulatorAddress, emitter, configAddress, c.owner, big.NewInt(10000000000), big.NewInt(1000000000000))
	c.transact(t, poolRegistry, "grantRole", crypto.Keccak256Hash([]byte("FACTORY_ROLE")), c.owner)
	c.transact(t, poolRegistry, "register", pool, token, c.owner, common.Address{}, big.NewInt(1))
	c.transact(t, emitterContract, "setPoolRegistry", registryAddress)
	c.transact(t, treasury, "grantRole", crypto.Keccak256Hash([]byte("DEPOSITOR_ROLE")), accumulatorAddress)
	c.transact(t, accumulator, "grantRole", crypto.Keccak256Hash([]byte("POOL_ROLE")), pool)
	c.transact(t, tokenContract, "transfer", pool, big.NewInt(1000000000000))
	c.transact(t, usdlContract, "transfer", pool, big.NewInt(1000000))
	runtime := []byte{0x36, 0x60, 0x00, 0x60, 0x00, 0x37, 0x60, 0x00, 0x60, 0x00, 0x36, 0x60, 0x00, 0x60, 0x00, 0x73}
	runtime = append(runtime, pool[:]...)
	runtime = append(runtime, 0x5a, 0xf1, 0x15, 0x60, 0x2f, 0x57, 0x60, 0x00, 0x60, 0x00, 0xf3, 0x5b, 0x60, 0x00, 0x60, 0x00, 0xfd)
	router, _ := deployRuntime(t, c, runtime)
	beneficiary := common.HexToAddress("0x000000000000000000000000000000000000beef")
	header, err := c.client.HeaderByNumber(c.ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	input, err := poolABI.Pack("swap", big.NewInt(1000000), big.NewInt(0), true, beneficiary, new(big.Int).SetUint64(header.Time+3600))
	if err != nil {
		t.Fatal(err)
	}
	forwardABI, err := abi.JSON(strings.NewReader("[]"))
	if err != nil {
		t.Fatal(err)
	}
	forwarder := bind.NewBoundContract(router, forwardABI, c.client, c.client, c.client)
	tx, err := forwarder.RawTransact(c.auth, input)
	if err != nil {
		t.Fatal("actual routed swap", err)
	}
	receipt := c.mined(t, tx)
	catalogBytes, err := os.ReadFile("../../../../../launchpad/spec/production-system/integration/contracts/v1/chain/abi-catalog.json")
	if err != nil {
		t.Fatal(err)
	}
	var catalog decode.Catalog
	if err = json.Unmarshal(catalogBytes, &catalog); err != nil {
		t.Fatal(err)
	}
	var deployments []decode.Deployment
	for address, role := range map[common.Address]string{emitter: "EventEmitter", pool: "SidioraPool", token: "SidioraERC20", usdl: "SidioraERC20"} {
		for _, contract := range catalog.Contracts {
			if contract.Name == role {
				deployments = append(deployments, decode.Deployment{Address: address, Role: role, ABIRevision: contract.ABIRevision})
			}
		}
	}
	decoder, err := decode.NewRegistry(catalogBytes, deployments)
	if err != nil {
		t.Fatal(err)
	}
	markets := []decode.ActorMarketBinding{{Pool: pool, Token: token, Settlement: usdl}}
	decoded, err := decoder.DecodeTransactionReceipt(tx, receipt, markets)
	if err != nil {
		t.Fatal(err)
	}
	var swap *decode.DecodedEnvelope
	for _, event := range decoded {
		if event.EventName == "Swap" {
			swap = event
		}
	}
	if swap == nil || swap.Actors == nil || swap.Actors.TransactionOrigin == nil || *swap.Actors.TransactionOrigin != strings.ToLower(c.owner.Hex()) || swap.Actors.EventCaller == nil || *swap.Actors.EventCaller != strings.ToLower(router.Hex()) || swap.Actors.Beneficiary == nil || *swap.Actors.Beneficiary != strings.ToLower(beneficiary.Hex()) || swap.Event.Args["sender"] != strings.ToLower(router.Hex()) {
		t.Fatalf("three actors collapsed: %#v", swap)
	}
	if swap.Actors.AttributionMethod != "unique_pool_output_transfer" || len(swap.Actors.EvidenceIdentity) != 2 {
		t.Fatal("receipt attribution evidence absent", swap.Actors)
	}
	withoutOutput := *receipt
	withoutOutput.Logs = nil
	for _, l := range receipt.Logs {
		if l.Address != token {
			withoutOutput.Logs = append(withoutOutput.Logs, l)
		}
	}
	unknown, err := decoder.DecodeTransactionReceipt(tx, &withoutOutput, markets)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range unknown {
		if e.EventName == "Swap" && (e.Actors.Beneficiary != nil || e.Actors.Recipient != nil || e.Actors.AttributionConfidence != "unknown" || e.Raw.Identity != swap.Raw.Identity) {
			t.Fatal("missing recipient became origin or destroyed raw identity", e)
		}
	}
	duplicate := *receipt
	duplicate.Logs = append(append([]*ethtypes.Log{}, receipt.Logs...), receipt.Logs...)
	replay, err := decoder.DecodeTransactionReceipt(tx, &duplicate, markets)
	if err != nil || len(replay) != len(decoded) {
		t.Fatal("duplicate delivery changed receipt", err)
	}
	t.Log(fmt.Sprintf("actual actors: origin=%s caller=%s beneficiary=%s receipt=%s", c.owner, router, beneficiary, tx.Hash()))
}

func TestActualSameTransactionUpgradeSelectsNewABIEpoch(t *testing.T) {
	c := actualAdversarialChain(t)
	proxy, emitter, _, _ := c.proxy(t, "EventEmitter", c.owner)
	token, tokenContract, _, _ := c.deploy(t, "SidioraERC20", "Upgrade Asset", "UPG", big.NewInt(1000000), c.owner)
	c.transact(t, tokenContract, "transfer", proxy, big.NewInt(100))
	implementation, _, nextABI, _ := c.deploy(t, "Treasury")
	input, err := nextABI.Pack("recoverSurplus", token, c.owner, big.NewInt(1))
	if err != nil {
		t.Fatal(err)
	}
	receipt := c.transact(t, emitter, "upgradeToAndCall", implementation, input)
	upgradeTopic := crypto.Keccak256Hash([]byte("Upgraded(address)"))
	var upgrade *ethtypes.Log
	for _, l := range receipt.Logs {
		if l.Address == proxy && len(l.Topics) > 0 && l.Topics[0] == upgradeTopic {
			upgrade = l
		}
	}
	if upgrade == nil {
		t.Fatal("actual upgrade event absent")
	}
	boundary := decode.Order{Block: receipt.BlockNumber.Uint64(), Transaction: receipt.TransactionIndex, Log: upgrade.Index + 1}
	catalogBytes, err := os.ReadFile("../../../../../launchpad/spec/production-system/integration/contracts/v1/chain/abi-catalog.json")
	if err != nil {
		t.Fatal(err)
	}
	var catalog decode.Catalog
	if err = json.Unmarshal(catalogBytes, &catalog); err != nil {
		t.Fatal(err)
	}
	var deployments []decode.Deployment
	for _, contract := range catalog.Contracts {
		switch contract.Name {
		case "EventEmitter":
			deployments = append(deployments, decode.Deployment{Address: proxy, Role: contract.Name, ABIRevision: contract.ABIRevision, Until: &boundary})
		case "Treasury":
			deployments = append(deployments, decode.Deployment{Address: proxy, Role: contract.Name, ABIRevision: contract.ABIRevision, From: boundary})
		case "SidioraERC20":
			deployments = append(deployments, decode.Deployment{Address: token, Role: contract.Name, ABIRevision: contract.ABIRevision})
		}
	}
	decoder, err := decode.NewRegistry(catalogBytes, deployments)
	if err != nil {
		t.Fatal(err)
	}
	seenUpgrade, seenNewABI := false, false
	for _, l := range receipt.Logs {
		event, err := decoder.Decode(*l)
		if err != nil {
			t.Fatal(err)
		}
		if event.EventName == "Upgraded" {
			seenUpgrade = event.EmitterRole == "EventEmitter"
		}
		if event.EventName == "SurplusRecovered" {
			seenNewABI = event.EmitterRole == "Treasury" && event.Event.Args["amount"] == "1" && event.Raw.Order.Log >= boundary.Log
		}
	}
	if !seenUpgrade || !seenNewABI {
		t.Fatal("same-transaction ABI epoch transition lost", seenUpgrade, seenNewABI)
	}
}
