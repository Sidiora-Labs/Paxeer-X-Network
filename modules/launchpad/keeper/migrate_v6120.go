package keeper

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/launchpad/types"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	"github.com/ethereum/go-ethereum/common"
)

const (
	// EventTypeMigrationProtocolConfigDiff records a generation 1
	// ProtocolConfig field whose value differs from the generation 2 value
	// the migration applies.
	EventTypeMigrationProtocolConfigDiff = "launchpad_migration_protocol_config_diff"
	// EventTypeMigrationSettledProtocolFees records the generation 1
	// FeeAccumulator _protocolFeesPending counter, protocol fees already
	// deposited in the generation 1 Treasury and so not merged into the
	// generation 2 pending protocol fees.
	EventTypeMigrationSettledProtocolFees = "launchpad_migration_settled_protocol_fees"

	AttributeKeyTreasury    = "treasury"
	AttributeKeyField       = "field"
	AttributeKeyGeneration1 = "generation_1"
	AttributeKeyGeneration2 = "generation_2"
)

// generation1Roles are the generation 1 role addresses; their pools and fee
// rights move to the generation 2 addresses and the role addresses other
// than EventEmitter and PoolBeacon are retired.
var generation1Roles = RoleAddresses{
	SidioraFactory:  common.HexToAddress("0x8a1A09CEe72c1D39dF33B8284E38baeF8371f465"),
	Router:          common.HexToAddress("0xCC7298801112682e10ee14b8a520309caD80336d"),
	Quoter:          common.HexToAddress("0xB768e183b6EfDeDf8b2AA7af732039D1C3c452d0"),
	FeesRouter:      common.HexToAddress("0x02Df12a44F2658080E76fbcF7D6B34Baa97843b6"),
	FeeAccumulator:  common.HexToAddress("0x50C69dF6637b3DCE6a7407C5A4b4F99E68514A76"),
	PoolRegistry:    common.HexToAddress("0x7684382c89f79104574D8EF9b31eFf2eD2C2BA0b"),
	ProtocolConfig:  common.HexToAddress("0xEeDF5409cFD30bd14D0399318c7d2150265575e5"),
	SidioraNFT:      common.HexToAddress("0xDF73b354ed9dcB473cc9D01541c46f507591e190"),
	Treasury:        common.HexToAddress("0x15405D535ce533BfFb98c83e42f4DD242AA5e079"),
	OpticalRegistry: common.HexToAddress("0x4CdA6e48632d51Ee4Fa735D81BF09F7543f644a1"),
	EventEmitter:    common.HexToAddress("0x0E10286EE51F99c666CDcAb52451e58AbdA4048F"),
	PoolBeacon:      common.HexToAddress("0xf11f08afe33e020Cab22bCaffBbAfC471c75E9d4"),
}

// migrationOperators are the deployer and guardian, Timelock, Governance and
// beacon owner accounts, probed with the role addresses, pools, opticals and
// holders for role memberships, operator approvals and the Treasury admin.
var migrationOperators = []common.Address{
	common.HexToAddress("0x1255d84066f579E7B7A3df4296e960d59fc05b32"),
	common.HexToAddress("0x82e177ca309578dc5Ed7Fc583278D2C96b3c0F14"),
	common.HexToAddress("0x87d682Fe2eeB6e76648f38BF3a955E36F26cCdB1"),
	common.HexToAddress("0xE3e71911664062753b56F72965D906295625d6c0"),
}

// EVMReader is the EVM state MigrateV6120 reads and writes: the EVM keeper,
// with InstallRetired writing the retirement runtime at a generation 1 role
// address.
type EVMReader interface {
	types.EVMKeeper
	InstallRetired(ctx sdk.Context, retired, replacement common.Address) error
}

// MigrateV6120 is the launchpad step of the v6.12.0 upgrade. It runs in one
// cached context and writes nothing unless every step succeeds; the error of
// a failed step is returned unwrapped.
func MigrateV6120(ctx sdk.Context, k Keeper, evm EVMReader) error {
	cc, write := ctx.CacheContext()
	if err := runV6120(cc, &k, evm); err != nil {
		return err
	}
	write()
	ctx.EventManager().EmitEvents(cc.EventManager().Events())
	return nil
}

func runV6120(ctx sdk.Context, k *Keeper, evm EVMReader) error {
	fx, err := loadMigrationFixture()
	if err != nil {
		return err
	}
	pools, err := listPools(ctx, k, evm, fx)
	if err != nil {
		return err
	}
	if err := clearLegacyProtocolFees(ctx, k); err != nil {
		return err
	}
	if err := readParams(ctx, k, evm); err != nil {
		return err
	}
	s := contractStorage{ctx: ctx, evm: evm}
	factoryTreasury, err := s.field(fx.layout, types.RoleFactory, "treasury", k.generation2.SidioraFactory)
	if err != nil {
		return err
	}
	if common.BytesToAddress(factoryTreasury.Bytes()) != k.TreasuryContract() ||
		fx.layout.Treasury != k.TreasuryContract() {
		return fmt.Errorf("%w: generation 2 SidioraFactory treasury %s", types.ErrInvalidConfiguration,
			common.BytesToAddress(factoryTreasury.Bytes()).Hex())
	}
	candidates, tokens, err := migrationCandidates(ctx, k, evm, fx, pools)
	if err != nil {
		return err
	}
	caller := evmKeeperCaller{evm: evm}
	if fx.usdl.Treasuries, err = treasuryDrains(ctx, k, evm, caller, fx, candidates, tokens); err != nil {
		return err
	}
	if ctx, err = k.MigrateV6120USDL(ctx, caller, fx.usdl); err != nil {
		return err
	}

	var deltas []types.BookHeldDelta
	for i := range pools {
		if err := readPool(ctx, k, evm, caller, fx, &pools[i]); err != nil {
			return err
		}
		deltas = append(deltas, pools[i].Deltas...)
	}
	known := make([]types.BookHeldDelta, len(fx.pools.Deltas))
	for i, d := range fx.pools.Deltas {
		known[i] = types.BookHeldDelta{Pool: d.Pool, Asset: d.AssetAddress, Book: d.Book, Held: d.Held, Delta: d.Delta}
	}
	if err := CheckDeltaTolerance(deltas, known, fx.pools.Tolerance); err != nil {
		return err
	}
	for _, d := range deltas {
		if err := k.RecordBookHeldDelta(ctx, d); err != nil {
			return err
		}
	}

	rights, err := readFeeRights(ctx, k, evm, fx, pools, candidates)
	if err != nil {
		return err
	}
	accounting, liabilities, settled, err := readAccounting(ctx, k, evm, fx, pools)
	if err != nil {
		return err
	}
	for i, pool := range pools {
		if err := migratePool(ctx, k, evm, pool, uint64(i+1), rights[pool.Address], accounting[pool.Address]); err != nil {
			return err
		}
		if err := installPoolSurface(ctx, k, evm, pool); err != nil {
			return err
		}
	}
	k.setUint64(ctx, types.MarketCountKey, uint64(len(pools)))
	if err := writeAccounting(ctx, k, pools, accounting, liabilities); err != nil {
		return err
	}
	ctx.EventManager().EmitEvent(sdk.NewEvent(EventTypeMigrationSettledProtocolFees,
		sdk.NewAttribute(AttributeKeyAddress, generation1Roles.FeeAccumulator.Hex()),
		sdk.NewAttribute(AttributeKeyTreasury, generation1Roles.Treasury.Hex()),
		sdk.NewAttribute(types.AttributeKeyAmount, settled.String())))
	if err := readRoles(ctx, k, evm, fx, candidates); err != nil {
		return err
	}
	if err := retireAddresses(ctx, k, evm); err != nil {
		return err
	}
	for _, invariant := range []sdk.Invariant{CustodyInvariant(k), SolvencyInvariant(k)} {
		if message, broken := invariant(ctx); broken {
			return fmt.Errorf("%w: %s", types.ErrInvalidBalance, message)
		}
	}
	return nil
}

// migrationCandidates are the accounts probed for role memberships, operator
// approvals and the Treasury admin: the role addresses of both generations,
// every pool and optical, the holders of holders.json and the operator
// accounts, in address order. It also returns the pool tokens.
func migrationCandidates(ctx sdk.Context, k *Keeper, evm EVMReader, fx migrationFixture,
	pools []v6120Pool) ([]common.Address, []common.Address, error) {
	s := contractStorage{ctx: ctx, evm: evm}
	set := map[common.Address]bool{}
	add := func(a common.Address) {
		if a != (common.Address{}) {
			set[a] = true
		}
	}
	for _, roles := range []RoleAddresses{generation1Roles, k.generation2} {
		for _, a := range []common.Address{roles.SidioraFactory, roles.Router, roles.Quoter, roles.FeesRouter,
			roles.FeeAccumulator, roles.PoolRegistry, roles.ProtocolConfig, roles.SidioraNFT, roles.Treasury,
			roles.OpticalRegistry, roles.EventEmitter, roles.PoolBeacon} {
			add(a)
		}
	}
	tokens := make([]common.Address, 0, len(pools))
	for _, pool := range pools {
		add(pool.Address)
		token, err := s.address(pool.Address, uintWord(0))
		if err != nil {
			return nil, nil, err
		}
		optical, err := s.address(pool.Address, uintWord(2))
		if err != nil {
			return nil, nil, err
		}
		add(optical)
		tokens = append(tokens, token)
	}
	for _, holders := range fx.holders {
		for _, holder := range holders {
			add(holder)
		}
	}
	for _, a := range migrationOperators {
		add(a)
	}
	out := make([]common.Address, 0, len(set))
	for a := range set {
		out = append(out, a)
	}
	sort.Slice(out, func(i, j int) bool { return bytes.Compare(out[i][:], out[j][:]) < 0 })
	return out, tokens, nil
}

// treasuryDrains is the Treasury drain of each generation for step 1: the
// first candidate holding DEFAULT_ADMIN_ROLE by storage read, and USDL with
// every pool token the Treasury books or holds.
func treasuryDrains(ctx sdk.Context, k *Keeper, evm EVMReader, caller NestedCaller, fx migrationFixture,
	candidates, tokens []common.Address) ([]TreasuryDrain, error) {
	s := contractStorage{ctx: ctx, evm: evm}
	rolesSlot, err := fx.layout.slot(types.RoleTreasury, "_roles")
	if err != nil {
		return nil, err
	}
	balances, err := fx.layout.slot(types.RoleTreasury, "_balances")
	if err != nil {
		return nil, err
	}
	var drains []TreasuryDrain
	for _, treasury := range []common.Address{generation1Roles.Treasury, k.TreasuryContract()} {
		drain := TreasuryDrain{Treasury: treasury, Tokens: []common.Address{k.quoteToken(ctx)}}
		for _, account := range candidates {
			admin, err := s.flag(treasury, roleMemberSlot(rolesSlot, DefaultAdminRole, account))
			if err != nil {
				return nil, err
			}
			if admin {
				drain.Admin = account
				break
			}
		}
		if drain.Admin == (common.Address{}) {
			return nil, fmt.Errorf("treasury %s: %w", treasury.Hex(), types.ErrInvalidRoleAccount)
		}
		for _, token := range tokens {
			held, err := k.quoteBalance(ctx, caller, token, treasury)
			if err != nil {
				return nil, err
			}
			if held.IsPositive() || s.amount(treasury, mappingSlot(addressWordHash(token), balances)).IsPositive() {
				drain.Tokens = append(drain.Tokens, token)
			}
		}
		drains = append(drains, drain)
	}
	return drains, nil
}

// migratePool writes the market of a pool at index with the generation 2
// Factory, FeeAccumulator, ProtocolConfig and EventEmitter, its generation 2
// fee right and its merged accounting, grants the pool POOL_ROLE at the
// generation 2 FeeAccumulator and writes its PoolRegistry metadata and
// token index.
func migratePool(ctx sdk.Context, k *Keeper, evm EVMReader, pool v6120Pool, index uint64, right v6120FeeRight,
	acc v6120Accounting) error {
	roles := k.generation2
	market := pool.Market
	market.Index = index
	market.Generation = types.Generation2
	market.Factory = roles.SidioraFactory.Hex()
	market.FeeAccumulator = roles.FeeAccumulator.Hex()
	market.ProtocolConfig = roles.ProtocolConfig.Hex()
	market.EventEmitter = roles.EventEmitter.Hex()
	market.FeeRightCollection = roles.SidioraNFT.Hex()
	market.FeeRightID = right.ID
	market.FeeRightsHolder = evm.GetPaxAddressOrDefault(ctx, right.Owner).String()
	market.FeeStrategy = right.Strategy
	market.AccumulatedFees = acc.AccumulatedFees
	market.AirdropBalance = acc.AirdropBalance
	market.AirdropEpoch = uint64(len(acc.Epochs))
	if err := market.Validate(); err != nil {
		return fmt.Errorf("pool %s: %w", pool.Address.Hex(), err)
	}
	k.setMarket(ctx, market)
	k.indexMarket(ctx, market)

	store := k.store(ctx)
	store.Set(types.RoleKey(roles.FeeAccumulator, PoolRole, pool.Address), []byte{1})
	metadata := pool.Metadata
	metadata.NFTID = right.ID
	bz, err := json.Marshal(metadata)
	if err != nil {
		return fmt.Errorf("%w: pool metadata %s: %v", types.ErrInvalidMarket, pool.Address.Hex(), err)
	}
	store.Set(addressKey(tokenPoolPrefix, metadata.Token), pool.Address.Bytes())
	store.Set(addressKey(poolMetadataPrefix, pool.Address), bz)
	return nil
}

// installPoolSurface replaces the pool proxy with the surface runtime and
// registers the pool as the surface of its market, with the generation of
// the contract it replaced.
func installPoolSurface(ctx sdk.Context, k *Keeper, evm EVMReader, pool v6120Pool) error {
	evm.SetCode(ctx, pool.Address, surfaceRuntime)
	return k.SetSurface(ctx, pool.Address, Surface{Role: types.RolePool, Generation: pool.Generation,
		Market: pool.Market.Denom})
}

// writeAccounting writes the merged airdrop epochs and claims, optical
// surplus and totalLiabilities, moves the generation 1 FeeAccumulator's
// custody ledger into the generation 2 one that serves every market, and
// refuses when that ledger does not cover totalLiabilities.
func writeAccounting(ctx sdk.Context, k *Keeper, pools []v6120Pool, accounting map[common.Address]v6120Accounting,
	liabilities sdk.Int) error {
	store := k.store(ctx)
	for _, pool := range pools {
		acc := accounting[pool.Address]
		for _, epoch := range acc.Epochs {
			if err := k.SetPoolAirdropEpoch(ctx, epoch); err != nil {
				return err
			}
		}
		for i, holder := range acc.Claims {
			store.Set(poolAirdropClaimKey(pool.Address, holder, acc.ClaimEpochs[i]), []byte{1})
		}
		if acc.OpticalSurplus.IsPositive() {
			if err := k.setLedgerAmount(ctx, opticalSurplusKey(pool.Address), acc.OpticalSurplus); err != nil {
				return err
			}
		}
	}
	if err := k.setLedgerAmount(ctx, totalLiabilitiesKey, liabilities); err != nil {
		return err
	}
	legacy := FeeHeldLedger(generation1Roles.FeeAccumulator)
	held, err := k.GetQuoteLedger(ctx, legacy)
	if err != nil {
		return err
	}
	if held.IsPositive() {
		if err := k.MoveQuote(ctx, legacy, k.feeHeld(), held); err != nil {
			return err
		}
	}
	feeHeld, err := k.GetQuoteLedger(ctx, k.feeHeld())
	if err != nil {
		return err
	}
	if feeHeld.LT(liabilities) {
		return fmt.Errorf("%w: generation 2 FeeAccumulator holds %s, totalLiabilities %s", types.ErrInsufficientSurplus,
			feeHeld, liabilities)
	}
	return nil
}

// readParams sets the parameters read from the generation 2 ProtocolConfig
// and records every generation 1 field that differs.
func readParams(ctx sdk.Context, k *Keeper, evm EVMReader) error {
	params, diffs, err := ParamsFromProtocolConfig(ctx, evm)
	if err != nil {
		return err
	}
	if err := k.setParams(ctx, params); err != nil {
		return err
	}
	for _, diff := range diffs {
		ctx.EventManager().EmitEvent(sdk.NewEvent(EventTypeMigrationProtocolConfigDiff,
			sdk.NewAttribute(AttributeKeyField, diff.Field),
			sdk.NewAttribute(AttributeKeyGeneration1, diff.Generation1),
			sdk.NewAttribute(AttributeKeyGeneration2, diff.Generation2)))
	}
	return nil
}

// clearLegacyProtocolFees deletes the previous binary's native protocol fee
// total, held at the bare ProtocolFeesPrefix key, and refuses a nonzero one:
// the keeper has no store to carry it.
func clearLegacyProtocolFees(ctx sdk.Context, k *Keeper) error {
	bz := k.store(ctx).Get(types.ProtocolFeesPrefix)
	if bz == nil {
		return nil
	}
	var pending sdk.Int
	if err := pending.Unmarshal(bz); err != nil {
		return fmt.Errorf("%w: legacy protocol fees pending: %v", ErrMigrationStorage, err)
	}
	if !pending.IsZero() {
		return fmt.Errorf("%w: legacy protocol fees pending %s", ErrNoKeeperStore, pending)
	}
	k.store(ctx).Delete(types.ProtocolFeesPrefix)
	return nil
}

// retireAddresses installs the retirement runtime at every retired
// generation 1 role address (contracts/launchpad.md section 21) and records
// it with its generation 2 replacement.
func retireAddresses(ctx sdk.Context, k *Keeper, evm EVMReader) error {
	g1, g2 := generation1Roles, k.generation2
	for _, entry := range []struct {
		role                 string
		retired, replacement common.Address
	}{
		{types.RoleFactory, g1.SidioraFactory, g2.SidioraFactory},
		{types.RoleRouter, g1.Router, g2.Router},
		{types.RoleQuoter, g1.Quoter, g2.Quoter},
		{types.RoleProtocolConfig, g1.ProtocolConfig, g2.ProtocolConfig},
		{types.RoleFeeAccumulator, g1.FeeAccumulator, g2.FeeAccumulator},
		{types.RolePoolRegistry, g1.PoolRegistry, g2.PoolRegistry},
		{types.RoleFeeRights, g1.SidioraNFT, g2.SidioraNFT},
		{types.RoleFeesRouter, g1.FeesRouter, g2.FeesRouter},
		{types.RoleTreasury, g1.Treasury, k.TreasuryContract()},
		{types.RoleOpticalRegistry, g1.OpticalRegistry, g2.OpticalRegistry},
	} {
		if err := evm.InstallRetired(ctx, entry.retired, entry.replacement); err != nil {
			return err
		}
		if err := k.RecordRetired(ctx, types.RetiredAddress{Address: entry.retired.Hex(), Role: entry.role,
			Replacement: entry.replacement.Hex()}); err != nil {
			return err
		}
	}
	return nil
}
