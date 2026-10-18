package receipt

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"sort"
	"strings"

	"github.com/Sidiora-Technologies/KindleLaunch/core/indexer/internal/decode"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/config/deployment"
	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
)

// Block identity modes. Ethereum-style chains name a block by
// keccak(RLP(header)), so every header, transaction root and receipt root can
// be recomputed locally. Chain 125 (Paxeer) is a CometBFT-backed EVM: the node
// reports a consensus block hash that is not the go-ethereum header hash, its
// transactionsRoot is the CometBFT data hash and its receiptsRoot is a constant
// placeholder (sha256 of the empty string). On such a chain the block is named
// by the node-reported hash; the authority then binds every read to that hash
// (EIP-1898), requires the canonical number lookup to report the same hash,
// recomputes every transaction hash from its signed payload, binds every
// receipt to its transaction and block, and checks the per-receipt and merged
// log blooms. Only the two trie roots, which the chain does not define in
// Ethereum form, are not recomputed.
const (
	BlockIdentityKeccak   = "keccak"
	BlockIdentityReported = "reported"
)

// ErrAnchorUnavailable reports that no configured source can still serve the
// chain-identity anchor block (pruned node without a historical source).
var ErrAnchorUnavailable = errors.New("receipt chain anchor unavailable from configured sources")

// BlockAnchor pins chain identity to an operator-issued (height, hash) inside
// the source retention window. It replaces the genesis pin on pruned chains,
// where block 0 cannot be read.
type BlockAnchor struct {
	Height uint64
	Hash   common.Hash
}

// CodeRange is one immutable-variable span of a runtime bytecode template.
type CodeRange struct {
	Start  int
	Length int
}

// AnchorAttestation proves chain identity from durable state when the anchor
// block itself has been pruned from every configured source. It must only
// succeed when previously persisted, anchor-descended ingestion still matches
// the live chain.
type AnchorAttestation func(context.Context, BlockAnchor) error

// DiscoveredMarket is a factory market recorded by a previously qualified
// canonical creation; it re-admits the market's token and pool code epochs
// after a restart.
type DiscoveredMarket struct {
	Token, Pool      common.Address
	Height           uint64
	TransactionIndex uint
}

type pinnedBlock struct {
	Header       *types.Header
	Hash         common.Hash
	Transactions types.Transactions
}

func validBlockIdentity(mode string) bool {
	return mode == "" || mode == BlockIdentityKeccak || mode == BlockIdentityReported
}

func (a *RPCQualification) reportedIdentity() bool {
	return a.options.BlockIdentity == BlockIdentityReported
}

// IsPruned reports whether err is a node's refusal to serve state or blocks
// below its retention floor.
func IsPruned(err error) bool {
	return err != nil && strings.Contains(strings.ToLower(err.Error()), "pruned")
}

// bundleBlockHash is the identity of the block a bundle claims membership in:
// the authority-issued block identity, or the Ethereum header hash for bundles
// built without one.
func bundleBlockHash(b Bundle) common.Hash {
	if b.BlockHash != (common.Hash{}) {
		return b.BlockHash
	}
	if b.Header == nil {
		return common.Hash{}
	}
	return b.Header.Hash()
}

func (a *RPCQualification) rawBlock(ctx context.Context, method string, selector any, full bool) (json.RawMessage, error) {
	var raw json.RawMessage
	if err := a.client.Client().CallContext(ctx, &raw, method, selector, full); err != nil {
		return nil, err
	}
	if len(raw) == 0 || string(raw) == "null" {
		return nil, ethereum.NotFound
	}
	return raw, nil
}

// identifiedHeader decodes a header and returns it with the identity this
// authority names it by. In keccak mode a reported hash that disagrees with the
// recomputed header hash is rejected.
func (a *RPCQualification) identifiedHeader(raw json.RawMessage) (*types.Header, common.Hash, error) {
	var header types.Header
	if err := json.Unmarshal(raw, &header); err != nil {
		return nil, common.Hash{}, ErrQualification
	}
	var reported struct {
		Hash *common.Hash `json:"hash"`
	}
	if err := json.Unmarshal(raw, &reported); err != nil {
		return nil, common.Hash{}, ErrQualification
	}
	if header.Number == nil || !header.Number.IsUint64() {
		return nil, common.Hash{}, ErrQualification
	}
	if a.reportedIdentity() {
		if reported.Hash == nil || *reported.Hash == (common.Hash{}) {
			return nil, common.Hash{}, ErrQualification
		}
		return &header, *reported.Hash, nil
	}
	computed := header.Hash()
	if reported.Hash != nil && *reported.Hash != computed {
		return nil, common.Hash{}, ErrQualification
	}
	return &header, computed, nil
}

func (a *RPCQualification) headerByNumber(ctx context.Context, number *big.Int) (*types.Header, common.Hash, error) {
	tag := "latest"
	if number != nil {
		tag = hexutil.EncodeBig(number)
	}
	raw, err := a.rawBlock(ctx, "eth_getBlockByNumber", tag, false)
	if err != nil {
		return nil, common.Hash{}, err
	}
	header, identity, err := a.identifiedHeader(raw)
	if err != nil {
		return nil, common.Hash{}, err
	}
	if number != nil && header.Number.Cmp(number) != 0 {
		return nil, common.Hash{}, ErrQualification
	}
	return header, identity, nil
}

func (a *RPCQualification) headerByHash(ctx context.Context, hash common.Hash) (*types.Header, error) {
	raw, err := a.rawBlock(ctx, "eth_getBlockByHash", hash, false)
	if err != nil {
		return nil, err
	}
	header, identity, err := a.identifiedHeader(raw)
	if err != nil {
		return nil, err
	}
	if identity != hash {
		return nil, ErrQualification
	}
	return header, nil
}

// blockByHash reads a full block by identity and authenticates every
// transaction by recomputing its hash from the signed payload.
func (a *RPCQualification) blockByHash(ctx context.Context, hash common.Hash) (pinnedBlock, error) {
	raw, err := a.rawBlock(ctx, "eth_getBlockByHash", hash, true)
	if err != nil {
		return pinnedBlock{}, err
	}
	block, err := a.parseBlock(raw)
	if err != nil {
		return pinnedBlock{}, err
	}
	if block.Hash != hash {
		return pinnedBlock{}, ErrQualification
	}
	return block, nil
}

// parseBlock decodes a full block and authenticates every transaction by
// recomputing its hash from the signed payload.
func (a *RPCQualification) parseBlock(raw json.RawMessage) (pinnedBlock, error) {
	header, hash, err := a.identifiedHeader(raw)
	if err != nil {
		return pinnedBlock{}, err
	}
	var body struct {
		Transactions []json.RawMessage `json:"transactions"`
	}
	if err = json.Unmarshal(raw, &body); err != nil || len(body.Transactions) > 4096 {
		return pinnedBlock{}, ErrQualification
	}
	txs := make(types.Transactions, len(body.Transactions))
	for i, encoded := range body.Transactions {
		var tx types.Transaction
		if err = tx.UnmarshalJSON(encoded); err != nil {
			return pinnedBlock{}, ErrQualification
		}
		var meta struct {
			Hash             *common.Hash    `json:"hash"`
			BlockHash        *common.Hash    `json:"blockHash"`
			TransactionIndex *hexutil.Uint64 `json:"transactionIndex"`
		}
		if err = json.Unmarshal(encoded, &meta); err != nil || meta.Hash == nil || *meta.Hash != tx.Hash() {
			return pinnedBlock{}, ErrQualification
		}
		if meta.BlockHash != nil && *meta.BlockHash != hash || meta.TransactionIndex != nil && uint64(*meta.TransactionIndex) != uint64(i) {
			return pinnedBlock{}, ErrQualification
		}
		txs[i] = &tx
	}
	return pinnedBlock{Header: header, Hash: hash, Transactions: txs}, nil
}

// verifyChainIdentity checks the configured genesis or anchor pin. A pin the
// sources can no longer serve is accepted only through attest, which must prove
// identity from durable anchor-descended state.
func (a *RPCQualification) verifyChainIdentity(ctx context.Context, attest AnchorAttestation) error {
	if a.options.GenesisHash != (common.Hash{}) {
		_, identity, err := a.headerByNumber(ctx, big.NewInt(0))
		if err != nil || identity != a.options.GenesisHash {
			return ErrQualification
		}
		return nil
	}
	anchor := a.options.Anchor
	if anchor == nil || anchor.Hash == (common.Hash{}) {
		return ErrQualification
	}
	_, identity, err := a.headerByNumber(ctx, new(big.Int).SetUint64(anchor.Height))
	if err == nil {
		if identity != anchor.Hash {
			return ErrQualification
		}
		return nil
	}
	if !IsPruned(err) {
		return fmt.Errorf("%w: anchor read: %v", ErrQualification, err)
	}
	if attest == nil {
		return ErrAnchorUnavailable
	}
	if err = attest(ctx, *anchor); err != nil {
		return errors.Join(ErrAnchorUnavailable, err)
	}
	return nil
}

func validCodeRanges(role string, ranges []CodeRange) bool {
	if len(ranges) == 0 {
		return true
	}
	if role != "SidioraERC20" || len(ranges) > 64 {
		return false
	}
	sorted := append([]CodeRange(nil), ranges...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Start < sorted[j].Start })
	end := 0
	for _, r := range sorted {
		if r.Start < end || r.Length < 1 || r.Length > 32 || r.Start > 128<<10 {
			return false
		}
		end = r.Start + r.Length
	}
	return true
}

// immutableRanges returns the masked spans for a factory-discovered token. Each
// SidioraERC20 instance embeds its own immutables (name hash, cached domain
// separator over address(this), ...), so the template identity is the hash of
// the runtime code with exactly those spans zeroed. Manifest roles are never
// masked.
func (a *RPCQualification) immutableRanges(e RPCCodeEpoch) []CodeRange {
	if e.Identity.Kind != "direct" || e.Deployment.Role != "SidioraERC20" {
		return nil
	}
	for _, id := range a.options.Manifest.Roles {
		if common.HexToAddress(id.Address) == e.Deployment.Address {
			return nil
		}
	}
	for _, template := range a.options.Discovery {
		if template.Role == "SidioraERC20" && template.ABIRevision == e.Deployment.ABIRevision && common.HexToHash(template.RuntimeHash) == common.HexToHash(e.Identity.RuntimeHash) {
			return template.Immutables
		}
	}
	return nil
}

func (a *RPCQualification) runtimeHash(e RPCCodeEpoch, code []byte) common.Hash {
	ranges := a.immutableRanges(e)
	if len(ranges) == 0 {
		return crypto.Keccak256Hash(code)
	}
	masked := append([]byte(nil), code...)
	for _, r := range ranges {
		if r.Start < 0 || r.Length < 0 || r.Start+r.Length > len(masked) {
			return common.Hash{}
		}
		for i := r.Start; i < r.Start+r.Length; i++ {
			masked[i] = 0
		}
	}
	return crypto.Keccak256Hash(masked)
}

func (a *RPCQualification) manifestAddress(address common.Address) bool {
	for _, id := range a.options.Manifest.Roles {
		if common.HexToAddress(id.Address) == address {
			return true
		}
	}
	return false
}

func (a *RPCQualification) current() *RPCQualification {
	a.sourceMu.Lock()
	defer a.sourceMu.Unlock()
	if a.discovered != nil {
		return a.discovered
	}
	return a
}

// DiscoveredEpochs returns the factory-discovered token and pool code epochs
// the authority currently admits (not the configured manifest epochs).
func (a *RPCQualification) DiscoveredEpochs() []RPCCodeEpoch {
	if a == nil {
		return nil
	}
	authority := a.current()
	out := []RPCCodeEpoch{}
	for _, e := range authority.options.Epochs {
		if (e.Deployment.Role == "SidioraPool" || e.Deployment.Role == "SidioraERC20") && !authority.manifestAddress(e.Deployment.Address) {
			out = append(out, e)
		}
	}
	return out
}

func (a *RPCQualification) discoveryEpoch(owned RPCOptions, address common.Address, role string, from decode.Order) (RPCCodeEpoch, error) {
	var template *RPCDiscovery
	for i := range owned.Discovery {
		if owned.Discovery[i].Role == role {
			if template != nil {
				return RPCCodeEpoch{}, ErrQualification
			}
			template = &owned.Discovery[i]
		}
	}
	if template == nil {
		return RPCCodeEpoch{}, ErrQualification
	}
	identity := deployment.Role{Address: address.Hex(), Kind: "direct", RuntimeHash: template.RuntimeHash}
	if role == "SidioraPool" {
		beacon := owned.Manifest.Roles["PoolBeacon"]
		identity.Kind = "beacon_proxy"
		identity.BeaconAddress = beacon.Address
		identity.ImplementationAddress = beacon.ImplementationAddress
		identity.ImplementationHash = beacon.ImplementationHash
	}
	return RPCCodeEpoch{Deployment: decode.Deployment{Address: address, Role: role, ABIRevision: template.ABIRevision, From: from}, Identity: identity}, nil
}

// Rehydrate re-admits the token and pool epochs of markets recorded by
// previously qualified canonical creations, so a restarted authority keeps
// qualifying trades on markets created before its process started. Each market
// is re-proven at the current chain head: code identity against the discovery
// templates, the token's deploying factory, and the PoolRegistry binding of
// pool and token.
func (a *RPCQualification) Rehydrate(ctx context.Context, markets []DiscoveredMarket) error {
	if a == nil || len(markets) > 2048 {
		return ErrQualification
	}
	if len(markets) == 0 {
		return nil
	}
	read, cancel := context.WithTimeout(ctx, a.options.Timeout)
	defer cancel()
	_, head, err := a.headerByNumber(read, nil)
	if err != nil {
		return err
	}
	a.sourceMu.Lock()
	defer a.sourceMu.Unlock()
	base := a
	if a.discovered != nil {
		base = a.discovered
	}
	owned, err := clone(base.options)
	if err != nil {
		return err
	}
	known := map[common.Address]bool{}
	for _, e := range owned.Epochs {
		known[e.Deployment.Address] = true
	}
	factory := common.HexToAddress(owned.Manifest.Roles["SidioraFactory"].Address)
	registry := common.HexToAddress(owned.Manifest.Roles["PoolRegistry"].Address)
	changed := false
	for _, market := range markets {
		if market.Token == (common.Address{}) || market.Pool == (common.Address{}) || market.Token == market.Pool || market.Height < owned.Manifest.ActivationHeight {
			return ErrQualification
		}
		from := decode.Order{Block: market.Height, Transaction: market.TransactionIndex, Log: 0}
		for _, item := range []struct {
			Address common.Address
			Role    string
		}{{market.Token, "SidioraERC20"}, {market.Pool, "SidioraPool"}} {
			if known[item.Address] {
				continue
			}
			e, err := base.discoveryEpoch(owned, item.Address, item.Role, from)
			if err != nil {
				return err
			}
			if _, err = base.validateCode(read, e, head); err != nil {
				return fmt.Errorf("%w: rehydrate %s %s", err, item.Role, strings.ToLower(item.Address.Hex()))
			}
			if len(owned.Epochs) >= 4096 {
				return ErrQualification
			}
			owned.Epochs = append(owned.Epochs, e)
			known[item.Address] = true
			changed = true
		}
		source, err := base.address(read, market.Token, "deploymentFactory()", head)
		if err != nil || source != factory {
			return ErrQualification
		}
		registered, err := base.word(read, registry, "isRegisteredPool(address)", head, market.Pool)
		if err != nil || new(big.Int).SetBytes(registered).Cmp(big.NewInt(1)) != 0 {
			return ErrQualification
		}
		linked, err := base.address(read, registry, "getPoolByToken(address)", head, market.Token)
		if err != nil || linked != market.Pool {
			return ErrQualification
		}
	}
	if !changed {
		return nil
	}
	deployments := make([]decode.Deployment, len(owned.Epochs))
	for i, e := range owned.Epochs {
		deployments[i] = e.Deployment
	}
	registryDecoder, err := decode.NewRegistry(owned.CatalogJSON, deployments)
	if err != nil {
		return err
	}
	a.discovered = &RPCQualification{client: a.client, options: owned, registry: registryDecoder, manifestHash: a.manifestHash, privateFixture: a.privateFixture, reads: a.reads}
	return nil
}

// PoolReserves is a pool's reserve state read at a pinned block.
type PoolReserves struct {
	VirtualUSDL, RealUSDL, Token, Price *big.Int
}

// PoolReserves reads getReserves() and getPrice() of a qualified pool pinned to
// block (EIP-1898 by block identity), i.e. the pool state after that block.
// SidioraPool emits PoolStateUpdated only from syncReserves(), so swaps do not
// announce reserves; this read is the reserve source of truth per swap block.
func (a *RPCQualification) PoolReserves(ctx context.Context, pool common.Address, block common.Hash) (PoolReserves, error) {
	var out PoolReserves
	if a == nil || pool == (common.Address{}) || block == (common.Hash{}) {
		return out, ErrQualification
	}
	authority := a.current()
	header, err := authority.headerByHash(ctx, block)
	if err != nil || header == nil {
		return out, ErrQualification
	}
	e, err := authority.epoch(pool, decode.Order{Block: header.Number.Uint64(), Transaction: ^uint(0), Log: ^uint(0)})
	if err != nil || e.Deployment.Role != "SidioraPool" {
		return out, ErrQualification
	}
	read, release, err := authority.scalarRead(ctx)
	if err != nil {
		return out, err
	}
	raw, err := authority.client.CallContractAtHash(read, ethereum.CallMsg{To: &pool, Data: crypto.Keccak256([]byte("getReserves()"))[:4], Gas: rpcReadGas}, block)
	release()
	if err != nil {
		return out, errors.Join(ErrQualification, err)
	}
	if len(raw) != 96 {
		return out, ErrQualification
	}
	price, err := authority.word(ctx, pool, "getPrice()", block)
	if err != nil {
		return out, err
	}
	out = PoolReserves{VirtualUSDL: new(big.Int).SetBytes(raw[:32]), RealUSDL: new(big.Int).SetBytes(raw[32:64]), Token: new(big.Int).SetBytes(raw[64:96]), Price: new(big.Int).SetBytes(price)}
	return out, nil
}
