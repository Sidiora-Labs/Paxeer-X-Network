package keeper

import (
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/big"
	"sort"

	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/launchpad/types"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

var (
	// ErrCodeHashNotInLayout refuses a pool whose code hash is not the pool
	// proxy code hash layout.json records for its generation.
	ErrCodeHashNotInLayout = errors.New("code hash not in layout")
	// ErrMigrationStorage refuses a storage word that does not decode as the
	// type layout.json declares for its slot.
	ErrMigrationStorage = errors.New("migration storage")
	// ErrNoKeeperStore refuses nonzero contract state the keeper has no store
	// to carry.
	ErrNoKeeperStore = errors.New("no keeper store")
	// ErrHolderSupplyMismatch refuses a pool token whose holders.json
	// balances do not sum to its totalSupply.
	ErrHolderSupplyMismatch = errors.New("holder balances differ from total supply")
)

//go:embed testdata/v6120/layout.json testdata/v6120/pools.json testdata/v6120/usdl.json testdata/v6120/holders.json
var v6120Fixtures embed.FS

type layoutSlot struct {
	Slot  uint64 `json:"slot"`
	Field string `json:"field"`
	Words uint64 `json:"words,omitempty"`
}

type layoutGeneration struct {
	Generation        types.Generation `json:"generation"`
	SidioraFactory    common.Address   `json:"sidioraFactory"`
	PoolRegistry      common.Address   `json:"poolRegistry"`
	Treasury          common.Address   `json:"treasury"`
	PoolProxyCodeHash common.Hash      `json:"poolProxyCodeHash"`
	PoolSlotsAbsent   []uint64         `json:"poolSlotsAbsent"`
}

type migrationLayout struct {
	Treasury     common.Address          `json:"treasury"`
	PausableSlot common.Hash             `json:"pausableSlot"`
	Generations  []layoutGeneration      `json:"generations"`
	Storage      map[string][]layoutSlot `json:"storage"`
}

type migrationDelta struct {
	Pool         string  `json:"pool"`
	AssetAddress string  `json:"assetAddress"`
	Book         sdk.Int `json:"book"`
	Held         sdk.Int `json:"held"`
	Delta        sdk.Int `json:"delta"`
}

type migrationPools struct {
	Tolerance sdk.Int          `json:"tolerance"`
	Deltas    []migrationDelta `json:"deltas"`
}

type migrationHolders struct {
	Token   common.Address   `json:"token"`
	Holders []common.Address `json:"holders"`
}

// migrationFixture is the halt-height record MigrateV6120 checks the chain
// against: layout.json, pools.json, usdl.json and holders.json.
type migrationFixture struct {
	layout  migrationLayout
	pools   migrationPools
	usdl    USDLGolden
	holders map[common.Address][]common.Address
}

func loadMigrationFixture() (migrationFixture, error) {
	var fx migrationFixture
	var holders []migrationHolders
	for name, out := range map[string]interface{}{"layout.json": &fx.layout, "pools.json": &fx.pools,
		"usdl.json": &fx.usdl, "holders.json": &holders} {
		bz, err := v6120Fixtures.ReadFile("testdata/v6120/" + name)
		if err != nil {
			return migrationFixture{}, err
		}
		if err := json.Unmarshal(bz, out); err != nil {
			return migrationFixture{}, fmt.Errorf("%w: %s: %v", types.ErrInvalidConfiguration, name, err)
		}
	}
	if len(fx.layout.Generations) != 2 || fx.layout.Generations[0].Generation != types.Generation1 ||
		fx.layout.Generations[1].Generation != types.Generation2 {
		return migrationFixture{}, fmt.Errorf("%w: layout.json generations", types.ErrInvalidConfiguration)
	}
	fx.holders = make(map[common.Address][]common.Address, len(holders))
	for _, entry := range holders {
		if _, dup := fx.holders[entry.Token]; dup {
			return migrationFixture{}, fmt.Errorf("%w: holders.json token %s repeated", types.ErrInvalidConfiguration,
				entry.Token.Hex())
		}
		fx.holders[entry.Token] = entry.Holders
	}
	return fx, nil
}

// slot is the declared slot of field in the storage of role in layout.json.
func (l migrationLayout) slot(role, field string) (common.Hash, error) {
	for _, entry := range l.Storage[role] {
		if entry.Field == field {
			return uintWord(entry.Slot), nil
		}
	}
	return common.Hash{}, fmt.Errorf("%w: layout.json has no %s.%s", types.ErrInvalidConfiguration, role, field)
}

// slotReader is the EVM storage read of the migration.
type slotReader interface {
	GetState(ctx sdk.Context, addr common.Address, hash common.Hash) common.Hash
}

func uintWord(n uint64) common.Hash { return common.BigToHash(new(big.Int).SetUint64(n)) }

func addressWordHash(address common.Address) common.Hash { return common.BytesToHash(address.Bytes()) }

// mappingSlot is the slot of mapping[key] declared at slot.
func mappingSlot(key, slot common.Hash) common.Hash { return crypto.Keccak256Hash(key[:], slot[:]) }

// offsetSlot is slot+i modulo 2**256.
func offsetSlot(slot common.Hash, i uint64) common.Hash {
	return common.BigToHash(new(big.Int).Add(slot.Big(), new(big.Int).SetUint64(i)))
}

// arraySlot is the slot of element i of the dynamic array whose length is at
// slot.
func arraySlot(slot common.Hash, i uint64) common.Hash {
	return offsetSlot(crypto.Keccak256Hash(slot[:]), i)
}

// roleMemberSlot is _roles[role].hasRole[account] of AccessControl with
// _roles at slot.
func roleMemberSlot(slot common.Hash, role [32]byte, account common.Address) common.Hash {
	return mappingSlot(addressWordHash(account), mappingSlot(common.Hash(role), slot))
}

// contractStorage reads typed words of contract storage.
type contractStorage struct {
	ctx sdk.Context
	evm slotReader
}

func (s contractStorage) word(contract common.Address, slot common.Hash) common.Hash {
	return s.evm.GetState(s.ctx, contract, slot)
}

func (s contractStorage) address(contract common.Address, slot common.Hash) (common.Address, error) {
	w := s.word(contract, slot)
	if new(big.Int).Rsh(w.Big(), 160).Sign() != 0 {
		return common.Address{}, fmt.Errorf("%w: %s slot %s is not an address", ErrMigrationStorage, contract.Hex(),
			slot.Hex())
	}
	return common.BytesToAddress(w[12:]), nil
}

func (s contractStorage) amount(contract common.Address, slot common.Hash) sdk.Int {
	return sdk.NewIntFromBigInt(s.word(contract, slot).Big())
}

func (s contractStorage) uint64(contract common.Address, slot common.Hash) (uint64, error) {
	v := s.word(contract, slot).Big()
	if !v.IsUint64() {
		return 0, fmt.Errorf("%w: %s slot %s exceeds uint64", ErrMigrationStorage, contract.Hex(), slot.Hex())
	}
	return v.Uint64(), nil
}

func (s contractStorage) int64(contract common.Address, slot common.Hash) (int64, error) {
	v, err := s.uint64(contract, slot)
	if err != nil {
		return 0, err
	}
	if v > math.MaxInt64 {
		return 0, fmt.Errorf("%w: %s slot %s exceeds int64", ErrMigrationStorage, contract.Hex(), slot.Hex())
	}
	return int64(v), nil
}

func (s contractStorage) flag(contract common.Address, slot common.Hash) (bool, error) {
	v := s.word(contract, slot).Big()
	if v.Cmp(big.NewInt(1)) > 0 {
		return false, fmt.Errorf("%w: %s slot %s is not a bool", ErrMigrationStorage, contract.Hex(), slot.Hex())
	}
	return v.Sign() == 1, nil
}

// field reads field of role at contract by its layout.json slot.
func (s contractStorage) field(l migrationLayout, role, field string, contract common.Address) (common.Hash, error) {
	slot, err := l.slot(role, field)
	if err != nil {
		return common.Hash{}, err
	}
	return s.word(contract, slot), nil
}

// v6120Pool is one pool of a generation 1 or 2 PoolRegistry as step 2 reads
// it: the market record before the generation 2 role addresses and the fee
// right are written, the registry metadata and the book and held amounts.
type v6120Pool struct {
	Address    common.Address
	Generation types.Generation
	Registry   common.Address
	Market     types.Market
	Metadata   PoolMetadata
	Deltas     []types.BookHeldDelta
}

// listPools refuses when native markets exist, then lists the pools of the
// _allPools array of each PoolRegistry, generation 1 first, and refuses a pool
// whose code hash is not the pool proxy code hash of its generation.
func listPools(ctx sdk.Context, k *Keeper, evm EVMReader, fx migrationFixture) ([]v6120Pool, error) {
	if err := k.CheckNoNativeMarkets(ctx); err != nil {
		return nil, err
	}
	s := contractStorage{ctx: ctx, evm: evm}
	allPools, err := fx.layout.slot(types.RolePoolRegistry, "_allPools")
	if err != nil {
		return nil, err
	}
	seen := map[common.Address]bool{}
	var pools []v6120Pool
	for _, generation := range fx.layout.Generations {
		count, err := s.uint64(generation.PoolRegistry, allPools)
		if err != nil {
			return nil, err
		}
		for i := uint64(0); i < count; i++ {
			pool, err := s.address(generation.PoolRegistry, arraySlot(allPools, i))
			if err != nil {
				return nil, err
			}
			if pool == (common.Address{}) || seen[pool] {
				return nil, fmt.Errorf("%w: pool %s zero or listed twice", ErrMigrationStorage, pool.Hex())
			}
			seen[pool] = true
			if evm.GetCodeHash(ctx, pool) != generation.PoolProxyCodeHash {
				return nil, fmt.Errorf("pool %s %w", pool.Hex(), ErrCodeHashNotInLayout)
			}
			pools = append(pools, v6120Pool{Address: pool, Generation: generation.Generation,
				Registry: generation.PoolRegistry})
		}
	}
	return pools, nil
}

var (
	erc20Name        = crypto.Keccak256([]byte("name()"))[:4]
	erc20Symbol      = crypto.Keccak256([]byte("symbol()"))[:4]
	erc20TotalSupply = crypto.Keccak256([]byte("totalSupply()"))[:4]
)

func tokenString(ctx sdk.Context, caller NestedCaller, token common.Address, selector []byte) (string, error) {
	ret, err := caller.StaticCall(ctx, launchpadAddress, token, selector)
	if err != nil {
		return "", fmt.Errorf("%w: token %s: %w", ErrMigrationStorage, token.Hex(), err)
	}
	stringType, err := abi.NewType("string", "", nil)
	if err != nil {
		return "", err
	}
	out, err := abi.Arguments{{Type: stringType}}.Unpack(ret)
	if err != nil || len(out) != 1 {
		return "", fmt.Errorf("%w: token %s string return: %v", ErrMigrationStorage, token.Hex(), err)
	}
	return out[0].(string), nil
}

func tokenSupply(ctx sdk.Context, caller NestedCaller, token common.Address) (sdk.Int, error) {
	ret, err := caller.StaticCall(ctx, launchpadAddress, token, erc20TotalSupply)
	if err != nil || len(ret) != 32 {
		return sdk.Int{}, fmt.Errorf("%w: token %s totalSupply: %v", ErrMigrationStorage, token.Hex(), err)
	}
	return sdk.NewIntFromBigInt(new(big.Int).SetBytes(ret)), nil
}

// readPool reads every pool slot layout.json declares for the pool's
// generation, the Pausable slot, the guardians, the registry metadata, the
// pool-token name, symbol and supply, and checks holders.json against
// balanceOf. Reserves are min(book, held): the book is realUsdlBalance and
// tokenReserve, the held amounts the USDL step 1 moved into HeldQuote(pool)
// and token.balanceOf(pool). A book-held difference becomes a delta record;
// nothing is minted or topped up.
func readPool(ctx sdk.Context, k *Keeper, evm EVMReader, caller NestedCaller, fx migrationFixture,
	pool *v6120Pool) error {
	s := contractStorage{ctx: ctx, evm: evm}
	absent := map[uint64]bool{}
	for _, n := range fx.layout.Generations[pool.Generation-1].PoolSlotsAbsent {
		absent[n] = true
	}
	words := map[string]common.Hash{}
	var snapshots []sdk.Int
	for _, entry := range fx.layout.Storage[types.RolePool] {
		if absent[entry.Slot] {
			continue
		}
		if entry.Words > 0 {
			for i := uint64(0); i < entry.Words; i++ {
				snapshots = append(snapshots, s.amount(pool.Address, uintWord(entry.Slot+i)))
			}
			continue
		}
		words[entry.Field] = s.word(pool.Address, uintWord(entry.Slot))
	}
	if len(snapshots) != types.SnapshotSlots {
		return fmt.Errorf("%w: pool %s has %d price snapshots", ErrMigrationStorage, pool.Address.Hex(), len(snapshots))
	}
	address := func(field string) (common.Address, error) {
		w := words[field]
		if new(big.Int).Rsh(w.Big(), 160).Sign() != 0 {
			return common.Address{}, fmt.Errorf("%w: pool %s %s", ErrMigrationStorage, pool.Address.Hex(), field)
		}
		return common.BytesToAddress(w[12:]), nil
	}
	amount := func(field string) sdk.Int { return sdk.NewIntFromBigInt(words[field].Big()) }
	small := func(field string) (int64, error) {
		v := words[field].Big()
		if !v.IsInt64() {
			return 0, fmt.Errorf("%w: pool %s %s", ErrMigrationStorage, pool.Address.Hex(), field)
		}
		return v.Int64(), nil
	}

	token, err := address("tokenAddress")
	if err != nil {
		return err
	}
	quote, err := address("usdlAddress")
	if err != nil {
		return err
	}
	if quote != k.quoteToken(ctx) {
		return fmt.Errorf("%w: pool %s quotes %s", types.ErrInvalidConfiguration, pool.Address.Hex(), quote.Hex())
	}
	optical, err := address("opticalAddress")
	if err != nil {
		return err
	}
	guardian, err := address("guardian")
	if err != nil {
		return err
	}
	pendingGuardian, err := address("pendingGuardian")
	if err != nil {
		return err
	}
	created, err := small("creationTimestamp")
	if err != nil {
		return err
	}
	lastSnapshot, err := small("lastSnapshotTimestamp")
	if err != nil {
		return err
	}
	snapshotIndex, err := small("snapshotIndex")
	if err != nil {
		return err
	}
	snapshotCount, err := small("snapshotCount")
	if err != nil {
		return err
	}
	paused, err := s.flag(pool.Address, fx.layout.PausableSlot)
	if err != nil {
		return err
	}

	metadataSlot, err := fx.layout.slot(types.RolePoolRegistry, "_poolMetadata")
	if err != nil {
		return err
	}
	base := mappingSlot(addressWordHash(pool.Address), metadataSlot)
	creator, err := s.address(pool.Registry, base)
	if err != nil {
		return err
	}
	metaToken, err := s.address(pool.Registry, offsetSlot(base, 1))
	if err != nil {
		return err
	}
	metaOptical, err := s.address(pool.Registry, offsetSlot(base, 2))
	if err != nil {
		return err
	}
	createdAt, err := s.int64(pool.Registry, offsetSlot(base, 4))
	if err != nil {
		return err
	}
	createdBlock, err := s.int64(pool.Registry, offsetSlot(base, 5))
	if err != nil {
		return err
	}
	if metaToken != token || metaOptical != optical || creator == (common.Address{}) {
		return fmt.Errorf("%w: registry metadata of pool %s", ErrMigrationStorage, pool.Address.Hex())
	}
	pool.Metadata = PoolMetadata{Creator: creator, Token: token, Optical: optical, CreatedAt: createdAt,
		CreatedBlock: createdBlock}

	name, err := tokenString(ctx, caller, token, erc20Name)
	if err != nil {
		return err
	}
	symbol, err := tokenString(ctx, caller, token, erc20Symbol)
	if err != nil {
		return err
	}
	supply, err := tokenSupply(ctx, caller, token)
	if err != nil {
		return err
	}
	holders, ok := fx.holders[token]
	if !ok {
		return fmt.Errorf("%w: holders.json lists no holders of %s", types.ErrInvalidConfiguration, token.Hex())
	}
	sum := sdk.ZeroInt()
	for _, holder := range holders {
		balance, err := k.quoteBalance(ctx, caller, token, holder)
		if err != nil {
			return err
		}
		sum = sum.Add(balance)
	}
	if !sum.Equal(supply) {
		return fmt.Errorf("%w: token %s holders sum %s, totalSupply %s", ErrHolderSupplyMismatch, token.Hex(), sum,
			supply)
	}
	heldToken, err := k.quoteBalance(ctx, caller, token, pool.Address)
	if err != nil {
		return err
	}
	heldQuote, err := k.GetQuoteLedger(ctx, HeldQuoteLedger(pool.Address))
	if err != nil {
		return err
	}
	bookQuote, bookToken := amount("realUsdlBalance"), amount("tokenReserve")
	pool.Deltas = nil
	for _, pair := range []struct {
		asset      common.Address
		book, held sdk.Int
	}{{quote, bookQuote, heldQuote}, {token, bookToken, heldToken}} {
		if !pair.book.Equal(pair.held) {
			pool.Deltas = append(pool.Deltas, types.BookHeldDelta{Pool: pool.Address.Hex(), Asset: pair.asset.Hex(),
				Book: pair.book, Held: pair.held, Delta: pair.held.Sub(pair.book)})
		}
	}

	account := func(a common.Address) string { return evm.GetPaxAddressOrDefault(ctx, a).String() }
	pending := ""
	if pendingGuardian != (common.Address{}) {
		pending = account(pendingGuardian)
	}
	pool.Market = types.Market{
		Denom: types.ModuleName + "/" + pool.Address.Hex(), Name: name, Symbol: symbol,
		Creator: account(creator), Guardian: account(guardian), Paused: paused, TotalSupply: supply,
		VirtualQuoteReserve: amount("virtualUsdlReserve"), RealQuoteBalance: sdk.MinInt(bookQuote, heldQuote),
		TokenReserve: sdk.MinInt(bookToken, heldToken), CreationTime: created,
		CumulativeVolume: amount("cumulativeVolume"), PriceSnapshots: snapshots,
		SnapshotHeights: make([]uint64, types.SnapshotSlots), SnapshotIndex: uint64(snapshotIndex),
		SnapshotCount: uint64(snapshotCount), AccumulatedQuoteFees: amount("accumulatedUsdlFees"),
		AccumulatedTokenFees: amount("accumulatedTokenFees"), PoolAddress: pool.Address.Hex(),
		TokenAddress: token.Hex(), Optical: optical.Hex(), PendingGuardian: pending,
		LastSnapshotTimestamp: lastSnapshot, CumulativeBuyQuoteVolume: amount("cumulativeBuyUsdlVolume"),
		CumulativeSellQuoteVolume: amount("cumulativeSellUsdlVolume"),
	}
	return nil
}

// v6120FeeRight is the generation 2 fee right of one pool after step 3.
type v6120FeeRight struct {
	ID       uint64
	Owner    common.Address
	Strategy types.FeeStrategy
}

type collectionRight struct {
	id       uint64
	owner    common.Address
	approved common.Address
	strategy types.FeeStrategy
	pool     common.Address
}

func readCollectionRight(s contractStorage, l migrationLayout, collection common.Address,
	id uint64) (collectionRight, error) {
	right := collectionRight{id: id}
	key := uintWord(id)
	for field, out := range map[string]*common.Address{"_owners": &right.owner, "_tokenApprovals": &right.approved,
		"_poolAddress": &right.pool} {
		slot, err := l.slot(types.RoleFeeRights, field)
		if err != nil {
			return collectionRight{}, err
		}
		if *out, err = s.address(collection, mappingSlot(key, slot)); err != nil {
			return collectionRight{}, err
		}
	}
	slot, err := l.slot(types.RoleFeeRights, "_feeStrategy")
	if err != nil {
		return collectionRight{}, err
	}
	strategy, err := s.uint64(collection, mappingSlot(key, slot))
	if err != nil {
		return collectionRight{}, err
	}
	right.strategy = types.FeeStrategy(strategy)
	if strategy > uint64(types.FeeStrategyLpRewards) || right.owner == (common.Address{}) ||
		right.pool == (common.Address{}) {
		return collectionRight{}, fmt.Errorf("%w: fee right %s/%d", ErrMigrationStorage, collection.Hex(), id)
	}
	return right, nil
}

// readAllTokens is _allTokens of collection.
func readAllTokens(s contractStorage, l migrationLayout, collection common.Address) ([]uint64, error) {
	slot, err := l.slot(types.RoleFeeRights, "_allTokens")
	if err != nil {
		return nil, err
	}
	count, err := s.uint64(collection, slot)
	if err != nil {
		return nil, err
	}
	ids := make([]uint64, count)
	for i := range ids {
		if ids[i], err = s.uint64(collection, arraySlot(slot, uint64(i))); err != nil {
			return nil, err
		}
	}
	return ids, nil
}

// readFeeRights writes the generation 2 fee-right collection: the generation
// 2 tokens keep their ids, _allTokens order, per-holder _ownedTokens order and
// balances; the generation 1 tokens are minted again at nextTokenId in
// generation 1 id order, each mapping recorded with RecordFeeRightID, without
// a receiver hook. Operator approvals of every holder for the candidate
// operators carry to the generation 2 collection, poolTokenId is set for every
// token and the pool index is left ready. It returns the fee right of every
// pool.
func readFeeRights(ctx sdk.Context, k *Keeper, evm EVMReader, fx migrationFixture, pools []v6120Pool,
	candidates []common.Address) (map[common.Address]v6120FeeRight, error) {
	s := contractStorage{ctx: ctx, evm: evm}
	l := fx.layout
	collection := k.generation2.SidioraNFT
	state, err := k.GetFeeRightCollection(ctx, collection)
	if err != nil {
		return nil, err
	}
	if state.TotalSupply != 0 || state.NextTokenID != 1 {
		return nil, fmt.Errorf("%w: generation 2 fee-right collection already holds tokens", ErrInvalidMigrationRecord)
	}
	nextSlot, err := l.slot(types.RoleFeeRights, "nextTokenId")
	if err != nil {
		return nil, err
	}
	next, err := s.uint64(collection, nextSlot)
	if err != nil {
		return nil, err
	}
	balanceSlot, err := l.slot(types.RoleFeeRights, "_balances")
	if err != nil {
		return nil, err
	}
	ownedSlot, err := l.slot(types.RoleFeeRights, "_ownedTokens")
	if err != nil {
		return nil, err
	}
	poolTokenSlot, err := l.slot(types.RoleFeeRights, "poolTokenId")
	if err != nil {
		return nil, err
	}
	operatorSlot, err := l.slot(types.RoleFeeRights, "_operatorApprovals")
	if err != nil {
		return nil, err
	}

	rights := map[common.Address]v6120FeeRight{}
	owners := map[common.Address]bool{}
	var ownerOrder []common.Address
	write := func(index uint64, right collectionRight, id uint64) error {
		if _, dup := rights[right.pool]; dup {
			return fmt.Errorf("%w: pool %s holds two fee rights", ErrMigrationStorage, right.pool.Hex())
		}
		approved := ""
		if right.approved != (common.Address{}) {
			approved = right.approved.Hex()
		}
		if err := k.setFeeRight(ctx, collection, types.FeeRight{ID: id, Owner: right.owner.Hex(), Approved: approved,
			Strategy: right.strategy, Pool: right.pool.Hex()}); err != nil {
			return err
		}
		k.setUint64(ctx, feeRightKey(feeRightAllTokensPrefix, collection, idBytes(index)), id)
		k.setUint64(ctx, feeRightKey(feeRightAllIndexPrefix, collection, idBytes(id)), index)
		k.setUint64(ctx, feeRightKey(feeRightPoolPrefix, collection, right.pool.Bytes()), id)
		rights[right.pool] = v6120FeeRight{ID: id, Owner: right.owner, Strategy: right.strategy}
		if !owners[right.owner] {
			owners[right.owner] = true
			ownerOrder = append(ownerOrder, right.owner)
		}
		return nil
	}

	ids, err := readAllTokens(s, l, collection)
	if err != nil {
		return nil, err
	}
	byID := map[uint64]common.Address{}
	for index, id := range ids {
		right, err := readCollectionRight(s, l, collection, id)
		if err != nil {
			return nil, err
		}
		if id == 0 || id >= next || byID[id] != (common.Address{}) {
			return nil, fmt.Errorf("%w: generation 2 fee right id %d", ErrMigrationStorage, id)
		}
		byID[id] = right.owner
		if poolID, err := s.uint64(collection, mappingSlot(addressWordHash(right.pool), poolTokenSlot)); err != nil {
			return nil, err
		} else if poolID != 0 && poolID != id {
			return nil, fmt.Errorf("%w: poolTokenId of %s is %d, fee right %d", ErrMigrationStorage, right.pool.Hex(),
				poolID, id)
		}
		if err := write(uint64(index), right, id); err != nil {
			return nil, err
		}
	}
	// _ownedTokens of the generation 2 holders keep their order.
	for _, owner := range ownerOrder {
		base := mappingSlot(addressWordHash(owner), ownedSlot)
		count, err := s.uint64(collection, base)
		if err != nil {
			return nil, err
		}
		balance, err := s.uint64(collection, mappingSlot(addressWordHash(owner), balanceSlot))
		if err != nil {
			return nil, err
		}
		if count != balance {
			return nil, fmt.Errorf("%w: %s owns %d tokens, balance %d", ErrMigrationStorage, owner.Hex(), count, balance)
		}
		for i := uint64(0); i < count; i++ {
			id, err := s.uint64(collection, arraySlot(base, i))
			if err != nil {
				return nil, err
			}
			if byID[id] != owner {
				return nil, fmt.Errorf("%w: %s lists fee right %d it does not own", ErrMigrationStorage, owner.Hex(), id)
			}
			k.setUint64(ctx, feeRightKey(feeRightOwnedPrefix, collection, owner.Bytes(), idBytes(i)), id)
			k.setUint64(ctx, feeRightKey(feeRightOwnedIndexPrefix, collection, idBytes(id)), i)
		}
		k.setUint64(ctx, feeRightKey(feeRightBalancePrefix, collection, owner.Bytes()), count)
	}
	generation2Owners := len(ownerOrder)

	legacy := generation1Roles.SidioraNFT
	legacyIDs, err := readAllTokens(s, l, legacy)
	if err != nil {
		return nil, err
	}
	sort.Slice(legacyIDs, func(i, j int) bool { return legacyIDs[i] < legacyIDs[j] })
	index := uint64(len(ids))
	for n, legacyID := range legacyIDs {
		if n > 0 && legacyIDs[n-1] == legacyID {
			return nil, fmt.Errorf("%w: generation 1 fee right %d listed twice", ErrMigrationStorage, legacyID)
		}
		right, err := readCollectionRight(s, l, legacy, legacyID)
		if err != nil {
			return nil, err
		}
		if next == math.MaxUint64 {
			return nil, types.ErrOverflow
		}
		if err := write(index, right, next); err != nil {
			return nil, err
		}
		k.addOwnedFeeRight(ctx, collection, right.owner, next)
		if err := k.RecordFeeRightID(ctx, legacyID, next); err != nil {
			return nil, err
		}
		index++
		next++
	}

	for n, owner := range ownerOrder {
		source := collection
		if n >= generation2Owners {
			source = legacy
		}
		for _, operator := range candidates {
			approved, err := s.flag(source, mappingSlot(addressWordHash(operator),
				mappingSlot(addressWordHash(owner), operatorSlot)))
			if err != nil {
				return nil, err
			}
			if approved && operator != owner {
				k.SetFeeRightApprovalForAll(ctx, collection, owner, operator, true)
			}
		}
	}
	// A generation 1 holder that also holds generation 2 rights keeps the
	// generation 1 operator approvals too.
	for _, owner := range ownerOrder[:generation2Owners] {
		for _, operator := range candidates {
			approved, err := s.flag(legacy, mappingSlot(addressWordHash(operator),
				mappingSlot(addressWordHash(owner), operatorSlot)))
			if err != nil {
				return nil, err
			}
			if approved && operator != owner {
				k.SetFeeRightApprovalForAll(ctx, collection, owner, operator, true)
			}
		}
	}

	if err := k.setFeeRightCollection(ctx, collection, FeeRightCollectionState{NextTokenID: next,
		PoolIndexMigrationCursor: next, PoolIndexReady: true, TotalSupply: index}); err != nil {
		return nil, err
	}
	nftSlot, err := l.slot(types.RolePoolRegistry, "_poolToNftId")
	if err != nil {
		return nil, err
	}
	for _, pool := range pools {
		right, ok := rights[pool.Address]
		if !ok {
			return nil, fmt.Errorf("%w: pool %s has no fee right", ErrMigrationStorage, pool.Address.Hex())
		}
		registered, err := s.uint64(pool.Registry, mappingSlot(addressWordHash(pool.Address), nftSlot))
		if err != nil {
			return nil, err
		}
		if pool.Generation == types.Generation2 && registered != right.ID {
			return nil, fmt.Errorf("%w: registry fee right of %s is %d, collection %d", ErrMigrationStorage,
				pool.Address.Hex(), registered, right.ID)
		}
		if pool.Generation == types.Generation1 {
			mapped, found, err := k.GetFeeRightID(ctx, registered)
			if err != nil {
				return nil, err
			}
			if !found || mapped != right.ID {
				return nil, fmt.Errorf("%w: registry fee right %d of %s is not its collection token",
					ErrMigrationStorage, registered, pool.Address.Hex())
			}
		}
	}
	return rights, nil
}

// roleTable is one role contract of both generations and the roles it holds
// besides DEFAULT_ADMIN_ROLE.
type roleTable struct {
	role                     string
	generation1, generation2 common.Address
	roles                    [][32]byte
}

func hashRole(name string) [32]byte { return crypto.Keccak256Hash([]byte(name)) }

func (k *Keeper) roleTables() []roleTable {
	g1, g2 := generation1Roles, k.generation2
	return []roleTable{
		{types.RoleFactory, g1.SidioraFactory, g2.SidioraFactory, [][32]byte{hashRole("ROUTER_ROLE")}},
		{types.RoleFeeRights, g1.SidioraNFT, g2.SidioraNFT, [][32]byte{[32]byte(FeeRightMinterRole), [32]byte(FeeRightStrategySetterRole)}},
		{types.RoleFeeAccumulator, g1.FeeAccumulator, g2.FeeAccumulator, [][32]byte{[32]byte(PoolRole), FeesRouterRole,
			hashRole("FACTORY_ROLE"), hashRole("OPTICAL_CLAIM_ROLE"), hashRole("OPTICAL_GRANTER_ROLE")}},
		{types.RolePoolRegistry, g1.PoolRegistry, g2.PoolRegistry, [][32]byte{hashRole("FACTORY_ROLE")}},
		{types.RoleTreasury, g1.Treasury, k.TreasuryContract(), [][32]byte{[32]byte(TreasuryDepositorRole)}},
	}
}

// readRoles carries the role memberships of both generations other than
// DEFAULT_ADMIN_ROLE, verified by storage read for every candidate account,
// into the role table of the generation 2 address of the same role. The
// Treasury _balances of both generations were credited to TreasuryLedger by
// the step 1 drain.
func readRoles(ctx sdk.Context, k *Keeper, evm EVMReader, fx migrationFixture, candidates []common.Address) error {
	s := contractStorage{ctx: ctx, evm: evm}
	store := k.store(ctx)
	for _, table := range k.roleTables() {
		slot, err := fx.layout.slot(table.role, "_roles")
		if err != nil {
			return err
		}
		for _, contract := range []common.Address{table.generation1, table.generation2} {
			for _, role := range table.roles {
				for _, account := range candidates {
					member, err := s.flag(contract, roleMemberSlot(slot, role, account))
					if err != nil {
						return err
					}
					if member {
						store.Set(types.RoleKey(table.generation2, role, account), []byte{1})
					}
				}
			}
		}
	}
	return nil
}

// v6120Accounting is the merged FeeAccumulator accounting of one pool.
type v6120Accounting struct {
	AccumulatedFees sdk.Int
	AirdropBalance  sdk.Int
	OpticalSurplus  sdk.Int
	Epochs          []types.PoolAirdropEpoch
	Claims          []common.Address
	ClaimEpochs     []uint64
}

// readAccounting reads the FeeAccumulator accounting slots of both
// generations for every pool and merges them: _accumulatedFees,
// _airdropBalance and _opticalSurplus add up, the airdrop epochs with their
// snapshot fields (slots 15 to 18) and legacy fields (slots 23 and 24) and the
// claims of holders.json come from the accumulator that opened them. State the
// keeper has no store for (_protocolFeesPending, _lpRewardsBalance,
// legacyAirdropUnallocated, an uninitialized accounting) refuses. It returns
// the accounting of every pool and the merged totalLiabilities.
func readAccounting(ctx sdk.Context, k *Keeper, evm EVMReader, fx migrationFixture,
	pools []v6120Pool) (map[common.Address]v6120Accounting, sdk.Int, error) {
	s := contractStorage{ctx: ctx, evm: evm}
	slots := map[string]common.Hash{}
	for _, entry := range fx.layout.Storage[types.RoleFeeAccumulator] {
		slots[entry.Field] = uintWord(entry.Slot)
	}
	for _, field := range []string{"_accumulatedFees", "_protocolFeesPending", "_airdropBalance", "_airdropEpoch",
		"_airdropEpochAmount", "_airdropClaimed", "_lpRewardsBalance", "_opticalSurplus", "totalLiabilities",
		"airdropSnapshotBlock", "airdropEligibleSupply", "airdropEpochRemaining", "airdropClaimedSupply",
		"accountingInitialized", "legacyAirdropUnallocated", "legacyAirdropRoot", "legacyAirdropRemaining"} {
		if _, ok := slots[field]; !ok {
			return nil, sdk.Int{}, fmt.Errorf("%w: layout.json has no FeeAccumulator.%s",
				types.ErrInvalidConfiguration, field)
		}
	}
	accumulators := []common.Address{generation1Roles.FeeAccumulator, k.generation2.FeeAccumulator}
	liabilities := sdk.ZeroInt()
	for _, accumulator := range accumulators {
		initialized, err := s.flag(accumulator, slots["accountingInitialized"])
		if err != nil {
			return nil, sdk.Int{}, err
		}
		if !initialized {
			return nil, sdk.Int{}, fmt.Errorf("%w: FeeAccumulator %s accounting is not initialized", ErrNoKeeperStore,
				accumulator.Hex())
		}
		if pending := s.amount(accumulator, slots["_protocolFeesPending"]); !pending.IsZero() {
			return nil, sdk.Int{}, fmt.Errorf("%w: FeeAccumulator %s _protocolFeesPending %s", ErrNoKeeperStore,
				accumulator.Hex(), pending)
		}
		liabilities = liabilities.Add(s.amount(accumulator, slots["totalLiabilities"]))
	}

	out := map[common.Address]v6120Accounting{}
	for _, pool := range pools {
		key := addressWordHash(pool.Address)
		acc := v6120Accounting{AccumulatedFees: sdk.ZeroInt(), AirdropBalance: sdk.ZeroInt(),
			OpticalSurplus: sdk.ZeroInt()}
		var opened common.Address
		for _, accumulator := range accumulators {
			acc.AccumulatedFees = acc.AccumulatedFees.Add(s.amount(accumulator, mappingSlot(key, slots["_accumulatedFees"])))
			acc.AirdropBalance = acc.AirdropBalance.Add(s.amount(accumulator, mappingSlot(key, slots["_airdropBalance"])))
			acc.OpticalSurplus = acc.OpticalSurplus.Add(s.amount(accumulator, mappingSlot(key, slots["_opticalSurplus"])))
			for _, field := range []string{"_lpRewardsBalance", "legacyAirdropUnallocated"} {
				if v := s.amount(accumulator, mappingSlot(key, slots[field])); !v.IsZero() {
					return nil, sdk.Int{}, fmt.Errorf("%w: FeeAccumulator %s %s of %s is %s", ErrNoKeeperStore,
						accumulator.Hex(), field, pool.Address.Hex(), v)
				}
			}
			epoch, err := s.uint64(accumulator, mappingSlot(key, slots["_airdropEpoch"]))
			if err != nil {
				return nil, sdk.Int{}, err
			}
			if epoch == 0 {
				continue
			}
			if opened != (common.Address{}) {
				return nil, sdk.Int{}, fmt.Errorf("%w: pool %s has airdrop epochs at %s and %s", ErrMigrationStorage,
					pool.Address.Hex(), opened.Hex(), accumulator.Hex())
			}
			opened = accumulator
			for e := uint64(1); e <= epoch; e++ {
				epochKey := uintWord(e)
				at := func(field string) common.Hash { return mappingSlot(epochKey, mappingSlot(key, slots[field])) }
				snapshot, err := s.uint64(accumulator, at("airdropSnapshotBlock"))
				if err != nil {
					return nil, sdk.Int{}, err
				}
				record := types.PoolAirdropEpoch{Pool: pool.Address.Hex(), Epoch: e,
					Amount: s.amount(accumulator, at("_airdropEpochAmount")), SnapshotBlock: snapshot,
					EligibleSupply:  s.amount(accumulator, at("airdropEligibleSupply")),
					Remaining:       s.amount(accumulator, at("airdropEpochRemaining")),
					ClaimedSupply:   s.amount(accumulator, at("airdropClaimedSupply")),
					LegacyRemaining: s.amount(accumulator, at("legacyAirdropRemaining"))}
				if root := s.word(accumulator, at("legacyAirdropRoot")); root != (common.Hash{}) {
					record.LegacyRoot = root.Hex()
				}
				acc.Epochs = append(acc.Epochs, record)
				for _, holder := range fx.holders[common.HexToAddress(pool.Market.TokenAddress)] {
					claimed, err := s.flag(accumulator, mappingSlot(epochKey, mappingSlot(addressWordHash(holder),
						mappingSlot(key, slots["_airdropClaimed"]))))
					if err != nil {
						return nil, sdk.Int{}, err
					}
					if claimed {
						acc.Claims = append(acc.Claims, holder)
						acc.ClaimEpochs = append(acc.ClaimEpochs, e)
					}
				}
			}
		}
		out[pool.Address] = acc
	}
	return out, liabilities, nil
}

// ProtocolConfigDiff is one ProtocolConfig field whose generation 1 value
// differs from the generation 2 value MigrateV6120 applies.
type ProtocolConfigDiff struct {
	Field       string
	Generation1 string
	Generation2 string
}

type protocolConfigValues struct {
	quoteToken                                       common.Address
	virtualQuote, virtualToken, creationFee          sdk.Int
	minFee, maxFee, baseFee, protocolFee             sdk.Int
	decayRate, volatilityWeight, concentrationWeight sdk.Int
}

func readProtocolConfig(s contractStorage, config common.Address) protocolConfigValues {
	word := func(n uint64) sdk.Int { return s.amount(config, uintWord(n)) }
	return protocolConfigValues{quoteToken: common.BytesToAddress(s.word(config, uintWord(1)).Bytes()),
		virtualQuote: word(2), virtualToken: word(3), minFee: word(4), maxFee: word(5), baseFee: word(6),
		protocolFee: word(7), decayRate: word(8), volatilityWeight: word(9), concentrationWeight: word(10),
		creationFee: word(11)}
}

func (v protocolConfigValues) fields() []struct {
	name  string
	value sdk.Int
} {
	return []struct {
		name  string
		value sdk.Int
	}{{"virtualUsdlDefault", v.virtualQuote}, {"virtualTokenDefault", v.virtualToken}, {"minFeeBps", v.minFee},
		{"maxFeeBps", v.maxFee}, {"baseFeeBps", v.baseFee}, {"protocolFeeBps", v.protocolFee},
		{"feeDecayRate", v.decayRate}, {"volatilityWeight", v.volatilityWeight},
		{"concentrationWeight", v.concentrationWeight}, {"creationFee", v.creationFee}}
}

// ParamsFromProtocolConfig is the parameter set of the generation 2
// ProtocolConfig storage (slots 1 to 11), its USDL address the quote token,
// refused when it fails Validate, and the fields of the generation 1
// ProtocolConfig that differ from it.
func ParamsFromProtocolConfig(ctx sdk.Context, evm slotReader) (types.Params, []ProtocolConfigDiff, error) {
	s := contractStorage{ctx: ctx, evm: evm}
	g1 := readProtocolConfig(s, generation1Roles.ProtocolConfig)
	g2 := readProtocolConfig(s, generation2Roles.ProtocolConfig)
	bps := func(v sdk.Int) (uint64, error) {
		if !v.IsUint64() {
			return 0, fmt.Errorf("%w: ProtocolConfig value %s exceeds uint64", types.ErrInvalidParams, v)
		}
		return v.Uint64(), nil
	}
	small := make([]uint64, 7)
	for n, v := range []sdk.Int{g2.minFee, g2.maxFee, g2.baseFee, g2.protocolFee, g2.decayRate, g2.volatilityWeight,
		g2.concentrationWeight} {
		var err error
		if small[n], err = bps(v); err != nil {
			return types.Params{}, nil, err
		}
	}
	params := types.Params{VirtualQuoteDefault: g2.virtualQuote,
		VirtualTokenDefault: g2.virtualToken, MinFeeBps: small[0], MaxFeeBps: small[1], BaseFeeBps: small[2],
		ProtocolFeeBps: small[3], FeeDecayRate: small[4], VolatilityWeight: small[5], ConcentrationWeight: small[6],
		CreationFee: g2.creationFee, QuoteToken: g2.quoteToken.Hex()}
	if err := params.Validate(); err != nil {
		return types.Params{}, nil, err
	}
	var diffs []ProtocolConfigDiff
	if g1.quoteToken != g2.quoteToken {
		diffs = append(diffs, ProtocolConfigDiff{Field: "usdlAddress", Generation1: g1.quoteToken.Hex(),
			Generation2: g2.quoteToken.Hex()})
	}
	f1, f2 := g1.fields(), g2.fields()
	for n := range f2 {
		if !f1[n].value.Equal(f2[n].value) {
			diffs = append(diffs, ProtocolConfigDiff{Field: f2[n].name, Generation1: f1[n].value.String(),
				Generation2: f2[n].value.String()})
		}
	}
	return params, diffs, nil
}
