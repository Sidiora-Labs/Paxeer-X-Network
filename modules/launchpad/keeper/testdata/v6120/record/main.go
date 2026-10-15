// Command record reads every getter of every launchpad pool, pool token,
// fee-right collection and role address of both contract generations with
// eth_call pinned at one height, records the refusal entries and the swap
// traces the parity tests replay, and writes golden.json.
//
// Run from the repository root:
//
//	go run ./modules/launchpad/keeper/testdata/v6120/record -rpc $RPC_URL -height $H -trader $TRADER
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"math/big"
	"os"
	"os/signal"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/rpc"
)

const (
	roleSidioraPool    = "SidioraPool"
	roleSidioraERC20   = "SidioraERC20"
	roleSidioraFactory = "SidioraFactory"
	roleSidioraNFT     = "SidioraNFT"
	roleRouter         = "Router"
	roleQuoter         = "Quoter"
	roleFeesRouter     = "FeesRouter"
	roleFeeAccumulator = "FeeAccumulator"
	rolePoolRegistry   = "PoolRegistry"
	roleProtocolConfig = "ProtocolConfig"
	roleTreasury       = "Treasury"

	expectedGeneration1Pools = 45
	expectedGeneration2Pools = 4
	snapshotSlots            = 8
	pastBlockOffset          = 10
	traceDeadlineSeconds     = 3600
	batchSize                = 100
	maxAirdropEpochs         = 1024
	revertErrorCode          = 3
)

var (
	errSupplyMismatch = errors.New("pool-token holder sum differs from totalSupply")

	goldenGeneration1Pool = common.HexToAddress("0xEeA16aC55AaE47c484EBef02eb1186aD606e1BF9")
	goldenGeneration2Pool = common.HexToAddress("0xCE20Da50F92D21d8BeE497910c782E9Aa25b6263")
	usdlAddress           = common.HexToAddress("0x85FcD13735F4309833A503EE804ea32395851479")
	launchpadPrecompile   = common.HexToAddress("0x0000000000000000000000000000000000001017")

	roles = []string{
		roleSidioraPool, roleSidioraERC20, roleSidioraFactory, roleSidioraNFT, roleRouter, roleQuoter,
		roleFeesRouter, roleFeeAccumulator, rolePoolRegistry, roleProtocolConfig, roleTreasury,
	}

	interfaceIDs = [][4]byte{
		{0x01, 0xff, 0xc9, 0xa7},
		{0x80, 0xac, 0x58, 0xcd},
		{0x78, 0x0e, 0x9d, 0x63},
		{0x5b, 0x5e, 0x13, 0x9f},
		{0x79, 0x65, 0xdb, 0x0b},
		{0xff, 0xff, 0xff, 0xff},
	}

	roleArgs   = []string{"getRoleAdmin", "hasRole"}
	argPlanned = map[string][]string{
		roleSidioraPool:    {"priceSnapshots"},
		roleSidioraERC20:   {"allowance", "balanceOf", "getPastBalance", "getPastTotalSupply", "getPastVotes", "nonces"},
		roleSidioraFactory: append([]string{"getNonce", "predictPoolAddress"}, roleArgs...),
		roleSidioraNFT: append([]string{"balanceOf", "getApproved", "getFeeStrategy", "getPoolAddress", "isApprovedForAll",
			"ownerOf", "poolTokenId", "supportsInterface", "tokenByIndex", "tokenOfOwnerByIndex"}, roleArgs...),
		roleRouter: roleArgs,
		roleQuoter: append([]string{"getAllPools", "getMarketCap", "getPoolPrice", "getPoolStats", "getPoolsByCreator",
			"quoteExactInput", "quoteMultihop"}, roleArgs...),
		roleFeesRouter: roleArgs,
		roleFeeAccumulator: append([]string{"airdropClaimedSupply", "airdropEligibleSupply", "airdropEpochRemaining",
			"airdropSnapshotBlock", "getAccumulatedFees", "getAirdropBalance", "getAirdropEpoch", "getLpRewardsBalance",
			"getOpticalSurplus", "hasClaimedAirdrop", "legacyAirdropRemaining", "legacyAirdropRoot",
			"legacyAirdropUnallocated", "supportsAirdrop"}, roleArgs...),
		rolePoolRegistry: append([]string{"getAllPools", "getNftIdByPool", "getPoolByToken", "getPoolCountByCreator",
			"getPoolMetadata", "getPoolsByCreator", "getPoolsByCreatorPage", "isRegisteredPool"}, roleArgs...),
		roleProtocolConfig: roleArgs,
		roleTreasury:       append([]string{"getBalance"}, roleArgs...),
	}

	refusalEntries = []struct{ role, method string }{
		{roleSidioraPool, "initialize"},
		{roleSidioraFactory, "initialize"},
		{roleSidioraFactory, "proxiableUUID"},
		{roleRouter, "initialize"},
		{roleQuoter, "initialize"},
		{roleFeesRouter, "initialize"},
		{roleFeeAccumulator, "allocateLegacyAirdrop"},
		{roleFeeAccumulator, "initialize"},
		{roleFeeAccumulator, "migrateAccounting"},
		{roleFeeAccumulator, "migrateAdditionalAccounting"},
		{rolePoolRegistry, "initialize"},
		{roleProtocolConfig, "initialize"},
		{roleProtocolConfig, "setBaseFeeBps"},
		{roleProtocolConfig, "setCreationFee"},
		{roleProtocolConfig, "setFeeWeights"},
		{roleProtocolConfig, "setProtocolFeeBps"},
		{roleProtocolConfig, "setVirtualDefaults"},
		{roleSidioraNFT, "initialize"},
		{roleSidioraNFT, "migratePoolIndex"},
	}
)

type generation struct {
	number         int
	factory        common.Address
	router         common.Address
	quoter         common.Address
	feesRouter     common.Address
	feeAccumulator common.Address
	poolRegistry   common.Address
	protocolConfig common.Address
	collection     common.Address
	treasury       common.Address
	expectedPools  int
	goldenPool     common.Address
	pools          []common.Address
}

func generations() []*generation {
	return []*generation{
		{
			number:         1,
			factory:        common.HexToAddress("0x8a1A09CEe72c1D39dF33B8284E38baeF8371f465"),
			router:         common.HexToAddress("0xCC7298801112682e10ee14b8a520309caD80336d"),
			quoter:         common.HexToAddress("0xB768e183b6EfDeDf8b2AA7af732039D1C3c452d0"),
			feesRouter:     common.HexToAddress("0x02Df12a44F2658080E76fbcF7D6B34Baa97843b6"),
			feeAccumulator: common.HexToAddress("0x50C69dF6637b3DCE6a7407C5A4b4F99E68514A76"),
			poolRegistry:   common.HexToAddress("0x7684382c89f79104574D8EF9b31eFf2eD2C2BA0b"),
			protocolConfig: common.HexToAddress("0xEeDF5409cFD30bd14D0399318c7d2150265575e5"),
			collection:     common.HexToAddress("0xDF73b354ed9dcB473cc9D01541c46f507591e190"),
			treasury:       common.HexToAddress("0x15405D535ce533BfFb98c83e42f4DD242AA5e079"),
			expectedPools:  expectedGeneration1Pools,
			goldenPool:     goldenGeneration1Pool,
		},
		{
			number:         2,
			factory:        common.HexToAddress("0x3993D004166738d1069B5C4e0a923f7B9D5D8830"),
			router:         common.HexToAddress("0x78FB06f525cD964388f2305A489035263E31C6e4"),
			quoter:         common.HexToAddress("0x793006f4F321b8863F26DBD75b11bAa01DA181dc"),
			feesRouter:     common.HexToAddress("0x05681dA6f611C2dfFD3E92611F6dE2d247647927"),
			feeAccumulator: common.HexToAddress("0x7b14808b72f61664cDb79D25E6d1587655571a66"),
			poolRegistry:   common.HexToAddress("0x1dC357d2B7B850332a91F06A9E66634F46fC4Ba7"),
			protocolConfig: common.HexToAddress("0xF3669eDc475Cb6E1F0d7635240ABD8e8FB7E20C6"),
			collection:     common.HexToAddress("0xe8DcC844a8006dbf64f5Bb851bCad90292a6409B"),
			expectedPools:  expectedGeneration2Pools,
			goldenPool:     goldenGeneration2Pool,
		},
	}
}

func (g *generation) roleAddresses() []struct {
	role string
	addr common.Address
} {
	return []struct {
		role string
		addr common.Address
	}{
		{roleSidioraFactory, g.factory},
		{roleRouter, g.router},
		{roleQuoter, g.quoter},
		{roleFeesRouter, g.feesRouter},
		{roleFeeAccumulator, g.feeAccumulator},
		{rolePoolRegistry, g.poolRegistry},
		{roleProtocolConfig, g.protocolConfig},
		{roleSidioraNFT, g.collection},
		{roleTreasury, g.treasury},
	}
}

type golden struct {
	Height      uint64              `json:"height"`
	BlockHash   common.Hash         `json:"blockHash"`
	Timestamp   uint64              `json:"timestamp"`
	Generations []generationRecord  `json:"generations"`
	Contracts   []*contractRecord   `json:"contracts"`
	Refusals    []*callRecord       `json:"refusals"`
	Traces      []*traceRecord      `json:"traces"`
	Supplies    []tokenSupplyRecord `json:"supplies"`
}

type generationRecord struct {
	Generation int                       `json:"generation"`
	Roles      map[string]common.Address `json:"roles"`
	Pools      []common.Address          `json:"pools"`
}

type contractRecord struct {
	Address    common.Address `json:"address"`
	Role       string         `json:"role"`
	Generation int            `json:"generation"`
	CodeHash   common.Hash    `json:"codeHash"`
	CodeSize   int            `json:"codeSize"`
	Calls      []*callRecord  `json:"calls"`
}

type callRecord struct {
	Key       string          `json:"key"`
	Signature string          `json:"signature"`
	Selector  hexutil.Bytes   `json:"selector"`
	To        common.Address  `json:"to"`
	From      *common.Address `json:"from,omitempty"`
	Input     hexutil.Bytes   `json:"input"`
	Reverted  bool            `json:"reverted"`
	Output    hexutil.Bytes   `json:"output"`
}

type traceRecord struct {
	Key       string          `json:"key"`
	Signature string          `json:"signature"`
	From      common.Address  `json:"from"`
	To        common.Address  `json:"to"`
	Input     hexutil.Bytes   `json:"input"`
	CallTrace json.RawMessage `json:"callTrace"`
	StateDiff json.RawMessage `json:"stateDiff"`
}

type tokenSupplyRecord struct {
	Token       common.Address `json:"token"`
	Pool        common.Address `json:"pool"`
	TotalSupply *hexutil.Big   `json:"totalSupply"`
	HolderSum   *hexutil.Big   `json:"holderSum"`
	Holders     int            `json:"holders"`
}

type holderList struct {
	Token   common.Address   `json:"token"`
	Holders []common.Address `json:"holders"`
}

type holderBalance struct {
	Holder  common.Address
	Balance *big.Int
}

type callReq struct {
	method *abi.Method
	rec    *callRecord
}

type recorder struct {
	client     *rpc.Client
	block      hexutil.Uint64
	abis       map[string]*abi.ABI
	out        *golden
	contracts  map[common.Address]*contractRecord
	getters    map[common.Address]map[string]*callReq
	queue      []*callReq
	gens       []*generation
	poolGen    map[common.Address]*generation
	poolToken  map[common.Address]common.Address
	tokenPool  map[common.Address]common.Address
	holders    map[common.Address][]common.Address
	accounts   map[common.Address]struct{}
	trader     common.Address
	buyAmount  *big.Int
	sellAmount *big.Int
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := run(ctx, os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "record:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("record", flag.ContinueOnError)
	rpcURL := fs.String("rpc", "", "JSON-RPC endpoint of a synced node that serves height H")
	height := fs.Uint64("height", 0, "height H every read is pinned to")
	trader := fs.String("trader", "", "account that sends the swap traces")
	abiDir := fs.String("abi", "precompiles/launchpad/abi", "directory of the role ABI files")
	holdersPath := fs.String("holders", "modules/launchpad/keeper/testdata/v6120/holders.json", "pool-token holder list")
	outPath := fs.String("out", "modules/launchpad/keeper/testdata/v6120/golden.json", "golden file to write")
	buy := fs.String("buy-usdl", "1000000", "USDL base units of the traced and quoted buys")
	sell := fs.String("sell-tokens", "1000000000000", "pool-token base units of the traced and quoted sells")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *rpcURL == "" {
		return errors.New("-rpc is required")
	}
	if *height <= pastBlockOffset {
		return fmt.Errorf("-height must be above %d", pastBlockOffset)
	}
	if !common.IsHexAddress(*trader) || common.HexToAddress(*trader) == (common.Address{}) {
		return errors.New("-trader must be a nonzero hex address")
	}
	buyAmount, ok := new(big.Int).SetString(*buy, 10)
	if !ok || buyAmount.Sign() <= 0 {
		return fmt.Errorf("-buy-usdl %q is not a positive integer", *buy)
	}
	sellAmount, ok := new(big.Int).SetString(*sell, 10)
	if !ok || sellAmount.Sign() <= 0 {
		return fmt.Errorf("-sell-tokens %q is not a positive integer", *sell)
	}
	abis, err := loadABIs(*abiDir)
	if err != nil {
		return err
	}
	holders, err := loadHolders(*holdersPath)
	if err != nil {
		return err
	}
	client, err := rpc.DialContext(ctx, *rpcURL)
	if err != nil {
		return fmt.Errorf("dial: %w", err)
	}
	defer client.Close()

	r := &recorder{
		client:     client,
		block:      hexutil.Uint64(*height),
		abis:       abis,
		out:        &golden{Height: *height},
		contracts:  map[common.Address]*contractRecord{},
		getters:    map[common.Address]map[string]*callReq{},
		gens:       generations(),
		poolGen:    map[common.Address]*generation{},
		poolToken:  map[common.Address]common.Address{},
		tokenPool:  map[common.Address]common.Address{},
		holders:    holders,
		accounts:   map[common.Address]struct{}{},
		trader:     common.HexToAddress(*trader),
		buyAmount:  buyAmount,
		sellAmount: sellAmount,
	}
	if err := r.record(ctx); err != nil {
		return err
	}
	return writeGolden(*outPath, r.out)
}

func loadABIs(dir string) (map[string]*abi.ABI, error) {
	abis := make(map[string]*abi.ABI, len(roles))
	for _, role := range roles {
		raw, err := os.ReadFile(filepath.Join(dir, role+".json"))
		if err != nil {
			return nil, fmt.Errorf("abi %s: %w", role, err)
		}
		parsed, err := abi.JSON(strings.NewReader(string(raw)))
		if err != nil {
			return nil, fmt.Errorf("abi %s: %w", role, err)
		}
		if err := checkArgPlan(role, &parsed); err != nil {
			return nil, err
		}
		abis[role] = &parsed
	}
	return abis, nil
}

func checkArgPlan(role string, parsed *abi.ABI) error {
	planned := map[string]bool{}
	for _, name := range argPlanned[role] {
		planned[name] = true
	}
	for _, name := range sortedMethods(parsed) {
		m := parsed.Methods[name]
		if !m.IsConstant() || len(m.Inputs) == 0 {
			continue
		}
		if !planned[m.Name] {
			return fmt.Errorf("abi %s: no argument plan for getter %s", role, m.Sig)
		}
	}
	return nil
}

func loadHolders(path string) (map[common.Address][]common.Address, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("holders: %w", err)
	}
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	var lists []holderList
	if err := dec.Decode(&lists); err != nil {
		return nil, fmt.Errorf("holders: %w", err)
	}
	out := make(map[common.Address][]common.Address, len(lists))
	for _, l := range lists {
		if _, dup := out[l.Token]; dup {
			return nil, fmt.Errorf("holders: token %s listed twice", l.Token.Hex())
		}
		if len(l.Holders) == 0 {
			return nil, fmt.Errorf("holders: token %s has no holders", l.Token.Hex())
		}
		out[l.Token] = l.Holders
	}
	return out, nil
}

func writeGolden(path string, g *golden) error {
	raw, err := json.MarshalIndent(g, "", "  ")
	if err != nil {
		return fmt.Errorf("encode golden: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(raw, '\n'), 0o644); err != nil {
		return fmt.Errorf("write golden: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("write golden: %w", err)
	}
	return nil
}

func sortedMethods(parsed *abi.ABI) []string {
	names := make([]string, 0, len(parsed.Methods))
	for name := range parsed.Methods {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func (r *recorder) record(ctx context.Context) error {
	if err := r.readHeader(ctx); err != nil {
		return err
	}
	if err := r.readRolesAndPools(ctx); err != nil {
		return err
	}
	if err := r.readPools(ctx); err != nil {
		return err
	}
	if err := r.readTokens(ctx); err != nil {
		return err
	}
	if err := r.readCollections(ctx); err != nil {
		return err
	}
	if err := r.readRegistriesAndQuoters(ctx); err != nil {
		return err
	}
	if err := r.readFeeAccumulators(ctx); err != nil {
		return err
	}
	if err := r.readTreasuries(ctx); err != nil {
		return err
	}
	if err := r.readRoleMembers(ctx); err != nil {
		return err
	}
	if err := r.readRefusals(ctx); err != nil {
		return err
	}
	if err := r.readTraces(ctx); err != nil {
		return err
	}
	return r.readCode(ctx)
}

func (r *recorder) readHeader(ctx context.Context) error {
	var header *struct {
		Hash      common.Hash    `json:"hash"`
		Timestamp hexutil.Uint64 `json:"timestamp"`
	}
	if err := r.client.CallContext(ctx, &header, "eth_getBlockByNumber", r.block, false); err != nil {
		return fmt.Errorf("block %d: %w", uint64(r.block), err)
	}
	if header == nil {
		return fmt.Errorf("block %d: not served", uint64(r.block))
	}
	r.out.BlockHash = header.Hash
	r.out.Timestamp = uint64(header.Timestamp)
	return nil
}

func (r *recorder) contract(addr common.Address, role string, gen int) (*contractRecord, error) {
	if c, ok := r.contracts[addr]; ok {
		if c.Role != role || c.Generation != gen {
			return nil, fmt.Errorf("address %s recorded as %s generation %d and as %s generation %d",
				addr.Hex(), c.Role, c.Generation, role, gen)
		}
		return c, nil
	}
	c := &contractRecord{Address: addr, Role: role, Generation: gen}
	r.contracts[addr] = c
	r.out.Contracts = append(r.out.Contracts, c)
	return c, nil
}

func (r *recorder) enqueue(c *contractRecord, name string, args ...interface{}) (*callReq, error) {
	q, err := r.newCall(c.Role, c.Address, name, nil, args...)
	if err != nil {
		return nil, err
	}
	c.Calls = append(c.Calls, q.rec)
	return q, nil
}

func (r *recorder) newCall(role string, to common.Address, name string, from *common.Address, args ...interface{}) (*callReq, error) {
	parsed := r.abis[role]
	m, ok := parsed.Methods[name]
	if !ok {
		return nil, fmt.Errorf("abi %s has no method %s", role, name)
	}
	packed, err := parsed.Pack(name, args...)
	if err != nil {
		return nil, fmt.Errorf("pack %s.%s: %w", role, name, err)
	}
	q := &callReq{
		method: &m,
		rec: &callRecord{
			Key:       role + "." + m.RawName,
			Signature: m.Sig,
			Selector:  hexutil.Bytes(m.ID),
			To:        to,
			From:      from,
			Input:     packed,
		},
	}
	r.queue = append(r.queue, q)
	return q, nil
}

func (r *recorder) enqueueGetters(c *contractRecord) (map[string]*callReq, error) {
	parsed := r.abis[c.Role]
	out := map[string]*callReq{}
	for _, name := range sortedMethods(parsed) {
		m := parsed.Methods[name]
		if !m.IsConstant() || len(m.Inputs) != 0 {
			continue
		}
		q, err := r.enqueue(c, name)
		if err != nil {
			return nil, err
		}
		out[m.RawName] = q
	}
	r.getters[c.Address] = out
	return out, nil
}

type callArgs struct {
	From *common.Address `json:"from,omitempty"`
	To   common.Address  `json:"to"`
	Data hexutil.Bytes   `json:"data"`
}

func (r *recorder) flush(ctx context.Context) error {
	for len(r.queue) > 0 {
		n := min(batchSize, len(r.queue))
		chunk := r.queue[:n]
		r.queue = r.queue[n:]
		elems := make([]rpc.BatchElem, n)
		results := make([]hexutil.Bytes, n)
		for i, q := range chunk {
			elems[i] = rpc.BatchElem{
				Method: "eth_call",
				Args:   []interface{}{callArgs{From: q.rec.From, To: q.rec.To, Data: q.rec.Input}, r.block},
				Result: &results[i],
			}
		}
		if err := r.client.BatchCallContext(ctx, elems); err != nil {
			return fmt.Errorf("eth_call batch: %w", err)
		}
		for i, q := range chunk {
			if elems[i].Error == nil {
				q.rec.Output = results[i]
				continue
			}
			data, err := revertData(elems[i].Error)
			if err != nil {
				return fmt.Errorf("%s at %s: %w", q.rec.Key, q.rec.To.Hex(), err)
			}
			q.rec.Reverted = true
			q.rec.Output = data
		}
	}
	return nil
}

func revertData(callErr error) (hexutil.Bytes, error) {
	var coded rpc.Error
	if !errors.As(callErr, &coded) || coded.ErrorCode() != revertErrorCode {
		return nil, callErr
	}
	var withData rpc.DataError
	if !errors.As(callErr, &withData) {
		return nil, fmt.Errorf("revert without data: %w", callErr)
	}
	encoded, ok := withData.ErrorData().(string)
	if !ok {
		return nil, fmt.Errorf("revert data is not a hex string: %w", callErr)
	}
	data, err := hexutil.Decode(encoded)
	if err != nil {
		return nil, fmt.Errorf("revert data %q: %w", encoded, err)
	}
	return data, nil
}

func (q *callReq) single() (interface{}, error) {
	if q.rec.Reverted {
		return nil, fmt.Errorf("%s at %s reverted", q.rec.Key, q.rec.To.Hex())
	}
	values, err := q.method.Outputs.Unpack(q.rec.Output)
	if err != nil {
		return nil, fmt.Errorf("decode %s at %s: %w", q.rec.Key, q.rec.To.Hex(), err)
	}
	if len(values) != 1 {
		return nil, fmt.Errorf("decode %s at %s: %d values", q.rec.Key, q.rec.To.Hex(), len(values))
	}
	return values[0], nil
}

func (q *callReq) address() (common.Address, error) {
	v, err := q.single()
	if err != nil {
		return common.Address{}, err
	}
	a, ok := v.(common.Address)
	if !ok {
		return common.Address{}, fmt.Errorf("decode %s: %T is not an address", q.rec.Key, v)
	}
	return a, nil
}

func (q *callReq) uint() (*big.Int, error) {
	v, err := q.single()
	if err != nil {
		return nil, err
	}
	n, ok := v.(*big.Int)
	if !ok {
		return nil, fmt.Errorf("decode %s: %T is not a uint256", q.rec.Key, v)
	}
	return n, nil
}

func (q *callReq) addresses() ([]common.Address, error) {
	v, err := q.single()
	if err != nil {
		return nil, err
	}
	a, ok := v.([]common.Address)
	if !ok {
		return nil, fmt.Errorf("decode %s: %T is not an address array", q.rec.Key, v)
	}
	return a, nil
}

func (q *callReq) bytes32() ([32]byte, error) {
	v, err := q.single()
	if err != nil {
		return [32]byte{}, err
	}
	b, ok := v.([32]byte)
	if !ok {
		return [32]byte{}, fmt.Errorf("decode %s: %T is not a bytes32", q.rec.Key, v)
	}
	return b, nil
}

func (q *callReq) bounded(limit int64) (int64, error) {
	n, err := q.uint()
	if err != nil {
		return 0, err
	}
	if !n.IsInt64() || n.Int64() > limit {
		return 0, fmt.Errorf("%s at %s is %s, above %d", q.rec.Key, q.rec.To.Hex(), n, limit)
	}
	return n.Int64(), nil
}

func (r *recorder) addAccount(a common.Address) {
	if a != (common.Address{}) {
		r.accounts[a] = struct{}{}
	}
}

func (r *recorder) readRolesAndPools(ctx context.Context) error {
	getters := map[*generation]map[common.Address]map[string]*callReq{}
	for _, g := range r.gens {
		getters[g] = map[common.Address]map[string]*callReq{}
		for _, ra := range g.roleAddresses() {
			if ra.addr == (common.Address{}) {
				continue
			}
			c, err := r.contract(ra.addr, ra.role, g.number)
			if err != nil {
				return err
			}
			if getters[g][ra.addr], err = r.enqueueGetters(c); err != nil {
				return err
			}
		}
	}
	if err := r.flush(ctx); err != nil {
		return err
	}
	counts := map[*generation]*big.Int{}
	for _, g := range r.gens {
		if g.treasury == (common.Address{}) {
			treasury, err := getters[g][g.factory]["treasury"].address()
			if err != nil {
				return err
			}
			if treasury == (common.Address{}) {
				return fmt.Errorf("generation %d factory names no treasury", g.number)
			}
			g.treasury = treasury
			c, err := r.contract(treasury, roleTreasury, g.number)
			if err != nil {
				return err
			}
			if _, err := r.enqueueGetters(c); err != nil {
				return err
			}
		}
		count, err := getters[g][g.poolRegistry]["getPoolCount"].bounded(1 << 20)
		if err != nil {
			return err
		}
		if count != int64(g.expectedPools) {
			return fmt.Errorf("generation %d registry lists %d pools, want %d", g.number, count, g.expectedPools)
		}
		counts[g] = big.NewInt(count)
	}
	lists := map[*generation]*callReq{}
	for _, g := range r.gens {
		q, err := r.enqueue(r.contracts[g.poolRegistry], "getAllPools", big.NewInt(0), counts[g])
		if err != nil {
			return err
		}
		lists[g] = q
		if _, err := r.enqueue(r.contracts[g.quoter], "getAllPools", big.NewInt(0), counts[g]); err != nil {
			return err
		}
	}
	if err := r.flush(ctx); err != nil {
		return err
	}
	for _, g := range r.gens {
		pools, err := lists[g].addresses()
		if err != nil {
			return err
		}
		if len(pools) != g.expectedPools {
			return fmt.Errorf("generation %d getAllPools returned %d pools, want %d", g.number, len(pools), g.expectedPools)
		}
		listed := false
		for _, p := range pools {
			if _, dup := r.poolGen[p]; dup {
				return fmt.Errorf("pool %s listed twice", p.Hex())
			}
			r.poolGen[p] = g
			listed = listed || p == g.goldenPool
		}
		if !listed {
			return fmt.Errorf("generation %d registry does not list golden pool %s", g.number, g.goldenPool.Hex())
		}
		g.pools = pools
		rolesOut := map[string]common.Address{}
		for _, ra := range g.roleAddresses() {
			rolesOut[ra.role] = ra.addr
			r.addAccount(ra.addr)
		}
		r.out.Generations = append(r.out.Generations, generationRecord{Generation: g.number, Roles: rolesOut, Pools: pools})
	}
	r.addAccount(launchpadPrecompile)
	return nil
}

func (r *recorder) readPools(ctx context.Context) error {
	getters := map[common.Address]map[string]*callReq{}
	for _, g := range r.gens {
		for _, p := range g.pools {
			c, err := r.contract(p, roleSidioraPool, g.number)
			if err != nil {
				return err
			}
			if getters[p], err = r.enqueueGetters(c); err != nil {
				return err
			}
			for i := int64(0); i < snapshotSlots; i++ {
				if _, err := r.enqueue(c, "priceSnapshots", big.NewInt(i)); err != nil {
					return err
				}
			}
			r.addAccount(p)
		}
	}
	if err := r.flush(ctx); err != nil {
		return err
	}
	for _, g := range r.gens {
		for _, p := range g.pools {
			token, err := getters[p]["tokenAddress"].address()
			if err != nil {
				return err
			}
			if token == (common.Address{}) {
				return fmt.Errorf("pool %s has no token", p.Hex())
			}
			if other, dup := r.tokenPool[token]; dup {
				return fmt.Errorf("token %s belongs to pools %s and %s", token.Hex(), other.Hex(), p.Hex())
			}
			r.poolToken[p] = token
			r.tokenPool[token] = p
			guardian, err := getters[p]["guardian"].address()
			if err != nil {
				return err
			}
			r.addAccount(guardian)
			if pending := getters[p]["pendingGuardian"]; !pending.rec.Reverted {
				a, err := pending.address()
				if err != nil {
					return err
				}
				r.addAccount(a)
			}
		}
	}
	return nil
}

func (r *recorder) spenders(pool common.Address) []common.Address {
	out := []common.Address{}
	for _, g := range r.gens {
		out = append(out, g.router)
	}
	return append(out, launchpadPrecompile, pool)
}

func (r *recorder) readTokens(ctx context.Context) error {
	for token := range r.holders {
		if _, ok := r.tokenPool[token]; !ok {
			return fmt.Errorf("holders: token %s is not a pool token of either registry", token.Hex())
		}
	}
	past := big.NewInt(int64(uint64(r.block)) - pastBlockOffset)
	supplies := map[common.Address]*callReq{}
	balances := map[common.Address][]*callReq{}
	for _, g := range r.gens {
		for _, p := range g.pools {
			token := r.poolToken[p]
			holders, ok := r.holders[token]
			if !ok {
				return fmt.Errorf("holders: no holder list for token %s of pool %s", token.Hex(), p.Hex())
			}
			c, err := r.contract(token, roleSidioraERC20, g.number)
			if err != nil {
				return err
			}
			getters, err := r.enqueueGetters(c)
			if err != nil {
				return err
			}
			supplies[token] = getters["totalSupply"]
			if _, err := r.enqueue(c, "getPastTotalSupply", past); err != nil {
				return err
			}
			if _, err := r.enqueue(c, "balanceOf", p); err != nil {
				return err
			}
			for _, h := range holders {
				q, err := r.enqueue(c, "balanceOf", h)
				if err != nil {
					return err
				}
				balances[token] = append(balances[token], q)
				if _, err := r.enqueue(c, "nonces", h); err != nil {
					return err
				}
				if _, err := r.enqueue(c, "getPastBalance", h, past); err != nil {
					return err
				}
				if _, err := r.enqueue(c, "getPastVotes", h, past); err != nil {
					return err
				}
				for _, s := range r.spenders(p) {
					if _, err := r.enqueue(c, "allowance", h, s); err != nil {
						return err
					}
				}
			}
		}
	}
	if err := r.flush(ctx); err != nil {
		return err
	}
	for _, g := range r.gens {
		for _, p := range g.pools {
			token := r.poolToken[p]
			rec, err := supplyRecord(token, p, r.holders[token], supplies[token], balances[token])
			if err != nil {
				return err
			}
			r.out.Supplies = append(r.out.Supplies, rec)
		}
	}
	return nil
}

func supplyRecord(token, pool common.Address, holders []common.Address, supply *callReq, balances []*callReq) (tokenSupplyRecord, error) {
	totalSupply, err := supply.uint()
	if err != nil {
		return tokenSupplyRecord{}, err
	}
	if len(holders) != len(balances) {
		return tokenSupplyRecord{}, fmt.Errorf("token %s: %d holders and %d balance reads", token.Hex(), len(holders), len(balances))
	}
	list := make([]holderBalance, len(holders))
	for i, q := range balances {
		b, err := q.uint()
		if err != nil {
			return tokenSupplyRecord{}, err
		}
		list[i] = holderBalance{Holder: holders[i], Balance: b}
	}
	sum, err := checkHolderSupply(token, totalSupply, list)
	if err != nil {
		return tokenSupplyRecord{}, err
	}
	return tokenSupplyRecord{
		Token:       token,
		Pool:        pool,
		TotalSupply: (*hexutil.Big)(totalSupply),
		HolderSum:   (*hexutil.Big)(sum),
		Holders:     len(list),
	}, nil
}

func checkHolderSupply(token common.Address, totalSupply *big.Int, balances []holderBalance) (*big.Int, error) {
	if totalSupply == nil || totalSupply.Sign() < 0 {
		return nil, fmt.Errorf("token %s: totalSupply is not a uint256", token.Hex())
	}
	seen := make(map[common.Address]struct{}, len(balances))
	sum := new(big.Int)
	for _, b := range balances {
		if _, dup := seen[b.Holder]; dup {
			return nil, fmt.Errorf("token %s: holder %s listed twice", token.Hex(), b.Holder.Hex())
		}
		seen[b.Holder] = struct{}{}
		if b.Balance == nil || b.Balance.Sign() < 0 {
			return nil, fmt.Errorf("token %s: balance of %s is not a uint256", token.Hex(), b.Holder.Hex())
		}
		sum.Add(sum, b.Balance)
	}
	if sum.Cmp(totalSupply) != 0 {
		return nil, fmt.Errorf("token %s: %w: holders %s, totalSupply %s", token.Hex(), errSupplyMismatch, sum, totalSupply)
	}
	return sum, nil
}

func (r *recorder) operators() []common.Address {
	out := []common.Address{}
	for _, g := range r.gens {
		out = append(out, g.feesRouter, g.router)
	}
	return append(out, launchpadPrecompile)
}

func (r *recorder) readCollections(ctx context.Context) error {
	type pending struct {
		c        *contractRecord
		owners   []*callReq
		ownerSet map[common.Address]struct{}
	}
	states := make([]*pending, 0, len(r.gens))
	for _, g := range r.gens {
		c := r.contracts[g.collection]
		next, err := r.getter(g.collection, "nextTokenId")
		if err != nil {
			return err
		}
		supply, err := r.getter(g.collection, "totalSupply")
		if err != nil {
			return err
		}
		nextID, err := next.bounded(1 << 20)
		if err != nil {
			return err
		}
		total, err := supply.bounded(1 << 20)
		if err != nil {
			return err
		}
		st := &pending{c: c, ownerSet: map[common.Address]struct{}{}}
		for id := int64(1); id < nextID; id++ {
			q, err := r.enqueue(c, "ownerOf", big.NewInt(id))
			if err != nil {
				return err
			}
			st.owners = append(st.owners, q)
			for _, name := range []string{"getApproved", "getFeeStrategy", "getPoolAddress"} {
				if _, err := r.enqueue(c, name, big.NewInt(id)); err != nil {
					return err
				}
			}
		}
		for i := int64(0); i < total; i++ {
			if _, err := r.enqueue(c, "tokenByIndex", big.NewInt(i)); err != nil {
				return err
			}
		}
		for _, p := range g.pools {
			if _, err := r.enqueue(c, "poolTokenId", p); err != nil {
				return err
			}
		}
		for _, id := range interfaceIDs {
			if _, err := r.enqueue(c, "supportsInterface", id); err != nil {
				return err
			}
		}
		states = append(states, st)
	}
	if err := r.flush(ctx); err != nil {
		return err
	}
	type balanceReq struct {
		c     *contractRecord
		owner common.Address
		q     *callReq
	}
	var owned []balanceReq
	for _, st := range states {
		owners := []common.Address{}
		for _, q := range st.owners {
			if q.rec.Reverted {
				continue
			}
			o, err := q.address()
			if err != nil {
				return err
			}
			if _, ok := st.ownerSet[o]; ok {
				continue
			}
			st.ownerSet[o] = struct{}{}
			owners = append(owners, o)
			r.addAccount(o)
		}
		for _, o := range owners {
			q, err := r.enqueue(st.c, "balanceOf", o)
			if err != nil {
				return err
			}
			owned = append(owned, balanceReq{c: st.c, owner: o, q: q})
			for _, op := range r.operators() {
				if _, err := r.enqueue(st.c, "isApprovedForAll", o, op); err != nil {
					return err
				}
			}
		}
	}
	if err := r.flush(ctx); err != nil {
		return err
	}
	for _, b := range owned {
		n, err := b.q.bounded(1 << 20)
		if err != nil {
			return err
		}
		for i := int64(0); i < n; i++ {
			if _, err := r.enqueue(b.c, "tokenOfOwnerByIndex", b.owner, big.NewInt(i)); err != nil {
				return err
			}
		}
	}
	return r.flush(ctx)
}

func (r *recorder) getter(addr common.Address, name string) (*callReq, error) {
	q, ok := r.getters[addr][name]
	if !ok {
		return nil, fmt.Errorf("getter %s at %s was not read", name, addr.Hex())
	}
	return q, nil
}

func (r *recorder) readRegistriesAndQuoters(ctx context.Context) error {
	metadata := map[common.Address]*callReq{}
	for _, g := range r.gens {
		registry := r.contracts[g.poolRegistry]
		quoter := r.contracts[g.quoter]
		for _, p := range g.pools {
			for _, name := range []string{"getNftIdByPool", "isRegisteredPool"} {
				if _, err := r.enqueue(registry, name, p); err != nil {
					return err
				}
			}
			q, err := r.enqueue(registry, "getPoolMetadata", p)
			if err != nil {
				return err
			}
			metadata[p] = q
			if _, err := r.enqueue(registry, "getPoolByToken", r.poolToken[p]); err != nil {
				return err
			}
			for _, name := range []string{"getMarketCap", "getPoolPrice", "getPoolStats"} {
				if _, err := r.enqueue(quoter, name, p); err != nil {
					return err
				}
			}
			if _, err := r.enqueue(quoter, "quoteExactInput", p, r.buyAmount, true); err != nil {
				return err
			}
			if _, err := r.enqueue(quoter, "quoteExactInput", p, r.sellAmount, false); err != nil {
				return err
			}
		}
		if len(g.pools) >= 2 {
			a, b := r.poolToken[g.pools[0]], r.poolToken[g.pools[1]]
			if _, err := r.enqueue(quoter, "quoteMultihop", a, b, r.sellAmount); err != nil {
				return err
			}
			if _, err := r.enqueue(quoter, "quoteMultihop", b, a, r.sellAmount); err != nil {
				return err
			}
		}
	}
	if err := r.flush(ctx); err != nil {
		return err
	}
	for _, g := range r.gens {
		registry := r.contracts[g.poolRegistry]
		quoter := r.contracts[g.quoter]
		factory := r.contracts[g.factory]
		creators := []common.Address{}
		seen := map[common.Address]struct{}{}
		for _, p := range g.pools {
			meta, err := decodeMetadata(metadata[p])
			if err != nil {
				return err
			}
			if meta.token != r.poolToken[p] {
				return fmt.Errorf("registry metadata of pool %s names token %s, pool names %s",
					p.Hex(), meta.token.Hex(), r.poolToken[p].Hex())
			}
			if _, err := r.enqueue(factory, "predictPoolAddress", meta.token, meta.creator, meta.optical); err != nil {
				return err
			}
			if _, ok := seen[meta.creator]; !ok {
				seen[meta.creator] = struct{}{}
				creators = append(creators, meta.creator)
				r.addAccount(meta.creator)
			}
		}
		for _, c := range creators {
			for _, name := range []string{"getPoolCountByCreator", "getPoolsByCreator"} {
				if _, err := r.enqueue(registry, name, c); err != nil {
					return err
				}
			}
			if _, err := r.enqueue(registry, "getPoolsByCreatorPage", c, big.NewInt(0), big.NewInt(int64(len(g.pools)))); err != nil {
				return err
			}
			if _, err := r.enqueue(quoter, "getPoolsByCreator", c); err != nil {
				return err
			}
			if _, err := r.enqueue(factory, "getNonce", c); err != nil {
				return err
			}
		}
	}
	return r.flush(ctx)
}

type poolMetadata struct {
	creator common.Address
	token   common.Address
	optical common.Address
}

func decodeMetadata(q *callReq) (poolMetadata, error) {
	if q.rec.Reverted {
		return poolMetadata{}, fmt.Errorf("%s at %s reverted", q.rec.Key, q.rec.To.Hex())
	}
	const words = 6
	out := q.rec.Output
	if len(out) != words*32 {
		return poolMetadata{}, fmt.Errorf("%s at %s returned %d bytes, want %d", q.rec.Key, q.rec.To.Hex(), len(out), words*32)
	}
	word := func(i int) (common.Address, error) {
		w := out[i*32 : (i+1)*32]
		for _, b := range w[:12] {
			if b != 0 {
				return common.Address{}, fmt.Errorf("%s at %s: word %d is not an address", q.rec.Key, q.rec.To.Hex(), i)
			}
		}
		return common.BytesToAddress(w[12:]), nil
	}
	var m poolMetadata
	var err error
	if m.creator, err = word(0); err != nil {
		return poolMetadata{}, err
	}
	if m.token, err = word(1); err != nil {
		return poolMetadata{}, err
	}
	if m.optical, err = word(2); err != nil {
		return poolMetadata{}, err
	}
	return m, nil
}

func (r *recorder) allPools() []common.Address {
	out := []common.Address{}
	for _, g := range r.gens {
		out = append(out, g.pools...)
	}
	return out
}

func (r *recorder) readFeeAccumulators(ctx context.Context) error {
	type epochReq struct {
		c    *contractRecord
		pool common.Address
		q    *callReq
	}
	var epochs []epochReq
	for _, g := range r.gens {
		c := r.contracts[g.feeAccumulator]
		for _, p := range r.allPools() {
			for _, name := range []string{"getAccumulatedFees", "getAirdropBalance", "getLpRewardsBalance",
				"getOpticalSurplus", "supportsAirdrop", "legacyAirdropUnallocated"} {
				if _, err := r.enqueue(c, name, p); err != nil {
					return err
				}
			}
			q, err := r.enqueue(c, "getAirdropEpoch", p)
			if err != nil {
				return err
			}
			epochs = append(epochs, epochReq{c: c, pool: p, q: q})
		}
	}
	if err := r.flush(ctx); err != nil {
		return err
	}
	for _, e := range epochs {
		last := int64(0)
		if !e.q.rec.Reverted {
			n, err := e.q.bounded(maxAirdropEpochs)
			if err != nil {
				return err
			}
			last = n
		}
		for epoch := int64(0); epoch <= last; epoch++ {
			id := big.NewInt(epoch)
			for _, name := range []string{"airdropClaimedSupply", "airdropEligibleSupply", "airdropEpochRemaining",
				"airdropSnapshotBlock", "legacyAirdropRemaining", "legacyAirdropRoot"} {
				if _, err := r.enqueue(e.c, name, e.pool, id); err != nil {
					return err
				}
			}
			for _, h := range r.holders[r.poolToken[e.pool]] {
				if _, err := r.enqueue(e.c, "hasClaimedAirdrop", e.pool, h, id); err != nil {
					return err
				}
			}
		}
	}
	return r.flush(ctx)
}

func (r *recorder) readTreasuries(ctx context.Context) error {
	for _, g := range r.gens {
		c := r.contracts[g.treasury]
		if _, err := r.enqueue(c, "getBalance", usdlAddress); err != nil {
			return err
		}
		for _, p := range r.allPools() {
			if _, err := r.enqueue(c, "getBalance", r.poolToken[p]); err != nil {
				return err
			}
		}
	}
	return r.flush(ctx)
}

func (r *recorder) sortedAccounts() []common.Address {
	out := make([]common.Address, 0, len(r.accounts))
	for a := range r.accounts {
		out = append(out, a)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Cmp(out[j]) < 0 })
	return out
}

func (r *recorder) readRoleMembers(ctx context.Context) error {
	accounts := r.sortedAccounts()
	for _, g := range r.gens {
		for _, ra := range g.roleAddresses() {
			if _, ok := r.abis[ra.role].Methods["hasRole"]; !ok {
				continue
			}
			c := r.contracts[ra.addr]
			names := make([]string, 0)
			for name := range r.getters[ra.addr] {
				if strings.HasSuffix(name, "_ROLE") {
					names = append(names, name)
				}
			}
			sort.Strings(names)
			for _, name := range names {
				q := r.getters[ra.addr][name]
				if q.rec.Reverted {
					continue
				}
				role, err := q.bytes32()
				if err != nil {
					return err
				}
				if _, err := r.enqueue(c, "getRoleAdmin", role); err != nil {
					return err
				}
				for _, a := range accounts {
					if _, err := r.enqueue(c, "hasRole", role, a); err != nil {
						return err
					}
				}
			}
		}
	}
	return r.flush(ctx)
}

func zeroArgs(m abi.Method) []interface{} {
	args := make([]interface{}, len(m.Inputs))
	for i, in := range m.Inputs {
		args[i] = reflect.New(in.Type.GetType()).Elem().Interface()
	}
	return args
}

func (r *recorder) generation(n int) *generation {
	for _, g := range r.gens {
		if g.number == n {
			return g
		}
	}
	return nil
}

func (r *recorder) readRefusals(ctx context.Context) error {
	g := r.generation(2)
	targets := map[string]common.Address{
		roleSidioraPool:    g.goldenPool,
		roleSidioraFactory: g.factory,
		roleRouter:         g.router,
		roleQuoter:         g.quoter,
		roleFeesRouter:     g.feesRouter,
		roleFeeAccumulator: g.feeAccumulator,
		rolePoolRegistry:   g.poolRegistry,
		roleProtocolConfig: g.protocolConfig,
		roleSidioraNFT:     g.collection,
	}
	from := r.trader
	queued := make([]*callReq, 0, len(refusalEntries))
	for _, e := range refusalEntries {
		m, ok := r.abis[e.role].Methods[e.method]
		if !ok {
			return fmt.Errorf("abi %s has no method %s", e.role, e.method)
		}
		q, err := r.newCall(e.role, targets[e.role], e.method, &from, zeroArgs(m)...)
		if err != nil {
			return err
		}
		queued = append(queued, q)
	}
	if err := r.flush(ctx); err != nil {
		return err
	}
	for _, q := range queued {
		if !q.rec.Reverted {
			return fmt.Errorf("refusal entry %s at %s did not revert", q.rec.Key, q.rec.To.Hex())
		}
		r.out.Refusals = append(r.out.Refusals, q.rec)
	}
	return nil
}

func (r *recorder) traceCall(ctx context.Context, role string, to common.Address, name string, args ...interface{}) error {
	parsed := r.abis[role]
	m, ok := parsed.Methods[name]
	if !ok {
		return fmt.Errorf("abi %s has no method %s", role, name)
	}
	input, err := parsed.Pack(name, args...)
	if err != nil {
		return fmt.Errorf("pack %s.%s: %w", role, name, err)
	}
	from := r.trader
	tx := callArgs{From: &from, To: to, Data: input}
	t := &traceRecord{Key: role + "." + m.RawName, Signature: m.Sig, From: from, To: to, Input: input}
	callCfg := map[string]interface{}{"tracer": "callTracer", "tracerConfig": map[string]interface{}{"withLog": true}}
	if err := r.client.CallContext(ctx, &t.CallTrace, "debug_traceCall", tx, r.block, callCfg); err != nil {
		return fmt.Errorf("trace %s at %s: %w", t.Key, to.Hex(), err)
	}
	diffCfg := map[string]interface{}{"tracer": "prestateTracer", "tracerConfig": map[string]interface{}{"diffMode": true}}
	if err := r.client.CallContext(ctx, &t.StateDiff, "debug_traceCall", tx, r.block, diffCfg); err != nil {
		return fmt.Errorf("state diff %s at %s: %w", t.Key, to.Hex(), err)
	}
	if len(t.CallTrace) == 0 || string(t.CallTrace) == "null" || len(t.StateDiff) == 0 || string(t.StateDiff) == "null" {
		return fmt.Errorf("trace %s at %s: empty result", t.Key, to.Hex())
	}
	r.out.Traces = append(r.out.Traces, t)
	return nil
}

func (r *recorder) readTraces(ctx context.Context) error {
	deadline := new(big.Int).SetUint64(r.out.Timestamp + traceDeadlineSeconds)
	zero := new(big.Int)
	for _, g := range r.gens {
		p := g.goldenPool
		token := r.poolToken[p]
		other := g.pools[0]
		if other == p {
			other = g.pools[1]
		}
		if err := r.traceCall(ctx, roleRouter, g.router, "buy", p, r.buyAmount, zero, deadline); err != nil {
			return err
		}
		if err := r.traceCall(ctx, roleRouter, g.router, "sell", p, r.sellAmount, zero, deadline); err != nil {
			return err
		}
		if err := r.traceCall(ctx, roleRouter, g.router, "swapTokenForToken", token, r.poolToken[other], r.sellAmount, zero, deadline); err != nil {
			return err
		}
		if err := r.traceCall(ctx, roleSidioraPool, p, "swap", r.buyAmount, zero, true, r.trader, deadline); err != nil {
			return err
		}
		if err := r.traceCall(ctx, roleSidioraPool, p, "swap", r.sellAmount, zero, false, r.trader, deadline); err != nil {
			return err
		}
	}
	return nil
}

func (r *recorder) readCode(ctx context.Context) error {
	for start := 0; start < len(r.out.Contracts); start += batchSize {
		chunk := r.out.Contracts[start:min(start+batchSize, len(r.out.Contracts))]
		elems := make([]rpc.BatchElem, len(chunk))
		codes := make([]hexutil.Bytes, len(chunk))
		for i, c := range chunk {
			elems[i] = rpc.BatchElem{Method: "eth_getCode", Args: []interface{}{c.Address, r.block}, Result: &codes[i]}
		}
		if err := r.client.BatchCallContext(ctx, elems); err != nil {
			return fmt.Errorf("eth_getCode batch: %w", err)
		}
		for i, c := range chunk {
			if elems[i].Error != nil {
				return fmt.Errorf("eth_getCode %s: %w", c.Address.Hex(), elems[i].Error)
			}
			if len(codes[i]) == 0 {
				return fmt.Errorf("%s %s has no code at %d", c.Role, c.Address.Hex(), uint64(r.block))
			}
			c.CodeHash = crypto.Keccak256Hash(codes[i])
			c.CodeSize = len(codes[i])
		}
	}
	return nil
}
