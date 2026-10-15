package types_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/launchpad/types"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/require"
)

func hexAddress(value string) string { return common.HexToAddress(value).Hex() }

var (
	gen2Factory        = hexAddress("0x3993D004166738d1069B5C4e0a923f7B9D5D8830")
	gen2Router         = hexAddress("0x78FB06f525cD964388f2305A489035263E31C6e4")
	gen2FeeAccumulator = hexAddress("0x7b14808b72f61664cDb79D25E6d1587655571a66")
	gen2FeeRights      = hexAddress("0xe8DcC844a8006dbf64f5Bb851bCad90292a6409B")
	gen2Treasury       = hexAddress("0x00000000000000000000000000000000000a1001")
	gen1Factory        = hexAddress("0x8a1A09CEe72c1D39dF33B8284E38baeF8371f465")
	gen1Treasury       = hexAddress("0x15405D535ce533BfFb98c83e42f4DD242AA5e079")
	gen1Optical        = hexAddress("0x4CdA6e48632d51Ee4Fa735D81BF09F7543f644a1")
	gen2Optical        = hexAddress("0x0FB5E510372Bd3EddFED16A5Dd766d4473CffcF0")
	poolOne            = hexAddress("0xEeA16aC55AaE47c484EBef02eb1186aD606e1BF9")
	poolTwo            = hexAddress("0xCE20Da50F92D21d8BeE497910c782E9Aa25b6263")
	usdl               = hexAddress("0x85FcD13735F4309833A503EE804ea32395851479")
	poolToken          = hexAddress("0x00000000000000000000000000000000000b2002")
	holder             = hexAddress("0x00000000000000000000000000000000000c3003")
	operator           = hexAddress("0x00000000000000000000000000000000000d4004")
	poolRole           = crypto.Keccak256Hash([]byte("POOL_ROLE")).Hex()
	depositorRole      = crypto.Keccak256Hash([]byte("DEPOSITOR_ROLE")).Hex()
	legacyRoot         = crypto.Keccak256Hash([]byte("legacy airdrop")).Hex()
)

// contractGenesis is a genesis with every contract section populated.
func contractGenesis() types.GenesisState {
	gs := *types.DefaultGenesis()
	gs.Surfaces = []types.Surface{
		{Address: gen2Factory, Role: types.RoleFactory}, {Address: gen2Router, Role: types.RoleRouter},
		{Address: gen2FeeAccumulator, Role: types.RoleFeeAccumulator}, {Address: gen2FeeRights, Role: types.RoleFeeRights},
		{Address: gen2Treasury, Role: types.RoleTreasury},
		{Address: poolOne, Role: types.RolePool}, {Address: poolTwo, Role: types.RolePool},
	}
	gs.RoleMembers = []types.RoleMember{
		{Contract: gen2FeeAccumulator, Role: poolRole, Account: poolOne},
		{Contract: gen2Treasury, Role: depositorRole, Account: gen2FeeAccumulator},
	}
	gs.FeeRights = types.FeeRightCollection{
		NextTokenID: 6, PoolIndexMigrationCursor: 6, PoolIndexReady: true,
		Tokens: []types.FeeRight{
			{ID: 2, Owner: holder, Strategy: types.FeeStrategyClaim, Pool: poolTwo},
			{ID: 5, Owner: holder, Approved: operator, Strategy: types.FeeStrategyAirdrop, Pool: poolOne},
		},
		Operators: []types.FeeRightOperator{{Owner: holder, Operator: operator}},
		IDMap:     []types.FeeRightIDMapping{{Generation1ID: 3, Generation2ID: 5}},
	}
	gs.FeeAccounting = types.FeeAccounting{
		PendingProtocolFees: sdk.NewInt(0), TotalLiabilities: sdk.NewInt(900), AccountingInitialized: true, AccountingMigrationCursor: 2,
		Pools: []types.PoolFeeAccount{
			{Pool: poolOne, AccumulatedFees: sdk.NewInt(100), AirdropBalance: sdk.NewInt(700), AirdropEpoch: 2, LpRewardsBalance: sdk.NewInt(0),
				OpticalSurplus: sdk.NewInt(100), LegacyAirdropUnallocated: sdk.NewInt(0), AccountingMigrated: true},
			{Pool: poolTwo, AccumulatedFees: sdk.NewInt(0), AirdropBalance: sdk.NewInt(0), AirdropEpoch: 0, LpRewardsBalance: sdk.NewInt(0),
				OpticalSurplus: sdk.NewInt(0), LegacyAirdropUnallocated: sdk.NewInt(0), AccountingMigrated: true},
		},
		Epochs: []types.PoolAirdropEpoch{
			{Pool: poolOne, Epoch: 1, Amount: sdk.NewInt(0), SnapshotBlock: 0, EligibleSupply: sdk.NewInt(0), Remaining: sdk.NewInt(0),
				ClaimedSupply: sdk.NewInt(0), LegacyRoot: legacyRoot, LegacyRemaining: sdk.NewInt(300)},
			{Pool: poolOne, Epoch: 2, Amount: sdk.NewInt(500), SnapshotBlock: 41, EligibleSupply: sdk.NewInt(1000), Remaining: sdk.NewInt(400),
				ClaimedSupply: sdk.NewInt(200), LegacyRemaining: sdk.NewInt(0)},
		},
		Claims: []types.PoolAirdropClaim{{Pool: poolOne, Holder: holder, Epoch: 2}},
	}
	gs.HeldQuote = []types.HeldQuote{{Pool: poolOne, Amount: sdk.NewInt(4_000)}, {Pool: poolTwo, Amount: sdk.NewInt(0)}}
	gs.FeeHeld = sdk.NewInt(900)
	gs.Treasury = []types.TreasuryBalance{{Token: usdl, Ledger: sdk.NewInt(1_000_000), Held: sdk.NewInt(1_000_250)}}
	gs.BookHeldDeltas = []types.BookHeldDelta{
		{Pool: poolOne, Asset: usdl, Book: sdk.NewInt(4_100), Held: sdk.NewInt(4_000), Delta: sdk.NewInt(-100)},
		{Pool: poolOne, Asset: poolToken, Book: sdk.NewInt(5_000), Held: sdk.NewInt(4_000), Delta: sdk.NewInt(-1_000)},
	}
	gs.Retired = []types.RetiredAddress{
		{Address: gen1Factory, Role: types.RoleFactory, Replacement: gen2Factory},
		{Address: gen1Treasury, Role: types.RoleTreasury, Replacement: gen2Treasury},
		{Address: gen1Optical, Role: types.RoleOpticalRegistry, Replacement: gen2Optical},
	}
	return gs
}

func TestGenesisValidate(t *testing.T) {
	require.NoError(t, types.DefaultGenesis().Validate())

	valid := contractGenesis()
	require.NoError(t, valid.Validate())
	raw, err := json.Marshal(valid)
	require.NoError(t, err)
	var decoded types.GenesisState
	require.NoError(t, json.Unmarshal(raw, &decoded))
	require.NoError(t, decoded.Validate())
	again, err := json.Marshal(decoded)
	require.NoError(t, err)
	require.JSONEq(t, string(raw), string(again))
	require.Equal(t, uint64(5), decoded.FeeRights.IDMap[0].Generation2ID)
	require.Equal(t, "-100", decoded.BookHeldDeltas[0].Delta.String())

	// SidioraNFT.initialize sets poolIndexReady with the cursor at 0 and mint
	// advances nextTokenId without the cursor, so a ready collection keeps any
	// cursor up to nextTokenId.
	for _, cursor := range []uint64{0, 5} {
		ready := contractGenesis()
		ready.FeeRights.PoolIndexMigrationCursor = cursor
		require.NoError(t, ready.Validate(), "ready collection with cursor %d", cursor)
	}

	cases := []struct {
		name   string
		reason string
		mutate func(*types.GenesisState)
	}{
		{"surface address not checksummed", "surface", func(gs *types.GenesisState) {
			gs.Surfaces[0].Address = strings.ToLower(gen2Factory)
		}},
		{"surface at the precompile", "surface", func(gs *types.GenesisState) {
			gs.Surfaces[0].Address = hexAddress(types.LaunchpadAddress)
		}},
		{"zero surface address", "surface", func(gs *types.GenesisState) {
			gs.Surfaces[0].Address = common.Address{}.Hex()
		}},
		{"duplicate surface address", "surface", func(gs *types.GenesisState) {
			gs.Surfaces = append(gs.Surfaces, types.Surface{Address: poolOne, Role: types.RolePool})
		}},
		{"second surface of a single role", "surface", func(gs *types.GenesisState) {
			gs.Surfaces = append(gs.Surfaces, types.Surface{Address: holder, Role: types.RoleRouter})
		}},
		{"optical registry is not a surface role", "surface", func(gs *types.GenesisState) {
			gs.Surfaces = append(gs.Surfaces, types.Surface{Address: gen2Optical, Role: types.RoleOpticalRegistry})
		}},
		{"unknown surface role", "surface", func(gs *types.GenesisState) {
			gs.Surfaces[0].Role = "Timelock"
		}},
		{"role member of an unknown contract", "role member", func(gs *types.GenesisState) {
			gs.RoleMembers[0].Contract = holder
		}},
		{"role member of a pool", "role member", func(gs *types.GenesisState) {
			gs.RoleMembers[0].Contract = poolOne
		}},
		{"default admin role is not carried", "role member", func(gs *types.GenesisState) {
			gs.RoleMembers[0].Role = common.Hash{}.Hex()
		}},
		{"role not a 32-byte word", "role member", func(gs *types.GenesisState) {
			gs.RoleMembers[0].Role = "0x01"
		}},
		{"duplicate role member", "role member", func(gs *types.GenesisState) {
			gs.RoleMembers = append(gs.RoleMembers, gs.RoleMembers[0])
		}},
		{"zero role account", "role member", func(gs *types.GenesisState) {
			gs.RoleMembers[0].Account = common.Address{}.Hex()
		}},
		{"migration cursor past next token id", "fee-right collection", func(gs *types.GenesisState) {
			gs.FeeRights.PoolIndexMigrationCursor = 7
		}},
		{"fee right id zero", "fee right 0", func(gs *types.GenesisState) {
			gs.FeeRights.Tokens[0].ID = 0
		}},
		{"fee right id at next token id", "fee right 6", func(gs *types.GenesisState) {
			gs.FeeRights.Tokens[0].ID = 6
		}},
		{"duplicate fee right id", "fee right 5", func(gs *types.GenesisState) {
			gs.FeeRights.Tokens[0].ID = 5
		}},
		{"fee right of a non-pool", "fee right 2", func(gs *types.GenesisState) {
			gs.FeeRights.Tokens[0].Pool = gen2Router
		}},
		{"two fee rights for one pool", "fee right 5", func(gs *types.GenesisState) {
			gs.FeeRights.Tokens[1].Pool = poolTwo
		}},
		{"fee right invalid strategy", "fee right 2", func(gs *types.GenesisState) {
			gs.FeeRights.Tokens[0].Strategy = types.FeeStrategyLpRewards + 1
		}},
		{"fee right invalid approval", "fee right 5", func(gs *types.GenesisState) {
			gs.FeeRights.Tokens[1].Approved = "0x1"
		}},
		{"fee right zero owner", "fee right 2", func(gs *types.GenesisState) {
			gs.FeeRights.Tokens[0].Owner = common.Address{}.Hex()
		}},
		{"operator equal to owner", "fee-right operator", func(gs *types.GenesisState) {
			gs.FeeRights.Operators[0].Operator = holder
		}},
		{"duplicate operator", "fee-right operator", func(gs *types.GenesisState) {
			gs.FeeRights.Operators = append(gs.FeeRights.Operators, gs.FeeRights.Operators[0])
		}},
		{"id map to a missing fee right", "fee-right id map 3 to 4", func(gs *types.GenesisState) {
			gs.FeeRights.IDMap[0].Generation2ID = 4
		}},
		{"id map from id zero", "fee-right id map 0 to 5", func(gs *types.GenesisState) {
			gs.FeeRights.IDMap[0].Generation1ID = 0
		}},
		{"duplicate generation 1 id", "fee-right id map 3 to 2", func(gs *types.GenesisState) {
			gs.FeeRights.IDMap = append(gs.FeeRights.IDMap, types.FeeRightIDMapping{Generation1ID: 3, Generation2ID: 2})
		}},
		{"duplicate generation 2 id", "fee-right id map 4 to 5", func(gs *types.GenesisState) {
			gs.FeeRights.IDMap = append(gs.FeeRights.IDMap, types.FeeRightIDMapping{Generation1ID: 4, Generation2ID: 5})
		}},
		{"nil total liabilities", "fee accounting totals", func(gs *types.GenesisState) {
			gs.FeeAccounting.TotalLiabilities = sdk.Int{}
		}},
		{"negative pending protocol fees", "fee accounting totals", func(gs *types.GenesisState) {
			gs.FeeAccounting.PendingProtocolFees = sdk.NewInt(-1)
		}},
		{"fee account of a non-pool", "fee account", func(gs *types.GenesisState) {
			gs.FeeAccounting.Pools[1].Pool = gen2Router
		}},
		{"duplicate fee account", "fee account", func(gs *types.GenesisState) {
			gs.FeeAccounting.Pools[1].Pool = poolOne
		}},
		{"negative fee account", "fee account", func(gs *types.GenesisState) {
			gs.FeeAccounting.Pools[0].OpticalSurplus = sdk.NewInt(-1)
		}},
		{"airdrop epoch past the pool epoch", "airdrop epoch", func(gs *types.GenesisState) {
			gs.FeeAccounting.Epochs[1].Epoch = 3
		}},
		{"airdrop epoch zero", "airdrop epoch", func(gs *types.GenesisState) {
			gs.FeeAccounting.Epochs[1].Epoch = 0
		}},
		{"airdrop epoch without fee account", "airdrop epoch", func(gs *types.GenesisState) {
			gs.FeeAccounting.Epochs[1].Pool = poolTwo
		}},
		{"duplicate airdrop epoch", "airdrop epoch", func(gs *types.GenesisState) {
			gs.FeeAccounting.Epochs[0].Epoch = 2
		}},
		{"remaining above the epoch amount", "airdrop epoch", func(gs *types.GenesisState) {
			gs.FeeAccounting.Epochs[1].Remaining = sdk.NewInt(501)
		}},
		{"claimed supply above eligible supply", "airdrop epoch", func(gs *types.GenesisState) {
			gs.FeeAccounting.Epochs[1].ClaimedSupply = sdk.NewInt(1001)
		}},
		{"legacy root on a snapshot epoch", "airdrop epoch", func(gs *types.GenesisState) {
			gs.FeeAccounting.Epochs[1].LegacyRoot = legacyRoot
		}},
		{"legacy remaining without a root", "airdrop epoch", func(gs *types.GenesisState) {
			gs.FeeAccounting.Epochs[1].LegacyRemaining = sdk.NewInt(1)
		}},
		{"legacy root not a word", "airdrop epoch", func(gs *types.GenesisState) {
			gs.FeeAccounting.Epochs[0].LegacyRoot = strings.ToUpper(legacyRoot)
		}},
		{"claim past the pool epoch", "airdrop claim", func(gs *types.GenesisState) {
			gs.FeeAccounting.Claims[0].Epoch = 3
		}},
		{"claim without fee account", "airdrop claim", func(gs *types.GenesisState) {
			gs.FeeAccounting.Claims[0].Pool = gen2Router
		}},
		{"duplicate claim", "airdrop claim", func(gs *types.GenesisState) {
			gs.FeeAccounting.Claims = append(gs.FeeAccounting.Claims, gs.FeeAccounting.Claims[0])
		}},
		{"held quote of a non-pool", "held quote", func(gs *types.GenesisState) {
			gs.HeldQuote[0].Pool = gen2FeeAccumulator
		}},
		{"duplicate held quote", "held quote", func(gs *types.GenesisState) {
			gs.HeldQuote[1].Pool = poolOne
		}},
		{"negative held quote", "held quote", func(gs *types.GenesisState) {
			gs.HeldQuote[0].Amount = sdk.NewInt(-1)
		}},
		{"fee held below total liabilities", "fee held below total liabilities", func(gs *types.GenesisState) {
			gs.FeeHeld = sdk.NewInt(899)
		}},
		{"nil fee held", "fee held below total liabilities", func(gs *types.GenesisState) {
			gs.FeeHeld = sdk.Int{}
		}},
		{"treasury held below ledger", "treasury", func(gs *types.GenesisState) {
			gs.Treasury[0].Held = sdk.NewInt(999_999)
		}},
		{"duplicate treasury token", "treasury", func(gs *types.GenesisState) {
			gs.Treasury = append(gs.Treasury, gs.Treasury[0])
		}},
		{"treasury token not an address", "treasury", func(gs *types.GenesisState) {
			gs.Treasury[0].Token = "uusdl"
		}},
		{"book-held delta not held minus book", "book-held delta", func(gs *types.GenesisState) {
			gs.BookHeldDeltas[0].Delta = sdk.NewInt(100)
		}},
		{"book-held delta of zero", "book-held delta", func(gs *types.GenesisState) {
			gs.BookHeldDeltas[0].Held = sdk.NewInt(4_100)
			gs.BookHeldDeltas[0].Delta = sdk.NewInt(0)
		}},
		{"duplicate book-held delta", "book-held delta", func(gs *types.GenesisState) {
			gs.BookHeldDeltas[1].Asset = usdl
		}},
		{"book-held delta of a non-pool", "book-held delta", func(gs *types.GenesisState) {
			gs.BookHeldDeltas[0].Pool = gen2Factory
		}},
		{"retired replacement of another role", "retired", func(gs *types.GenesisState) {
			gs.Retired[0].Replacement = gen2Router
		}},
		{"retired address is a surface", "retired", func(gs *types.GenesisState) {
			gs.Retired[0].Address = gen2Router
			gs.Retired[0].Role = types.RoleRouter
		}},
		{"retired pool role", "retired", func(gs *types.GenesisState) {
			gs.Retired[0].Role = types.RolePool
			gs.Retired[0].Replacement = poolOne
		}},
		{"role retired twice", "retired", func(gs *types.GenesisState) {
			gs.Retired = append(gs.Retired, types.RetiredAddress{Address: holder, Role: types.RoleFactory,
				Replacement: gen2Factory})
		}},
		{"duplicate retired address", "retired", func(gs *types.GenesisState) {
			gs.Retired[1].Address = gen1Factory
		}},
		{"retired optical registry replaced by a surface", "retired", func(gs *types.GenesisState) {
			gs.Retired[2].Replacement = gen2Router
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gs := contractGenesis()
			tc.mutate(&gs)
			err := gs.Validate()
			require.ErrorIs(t, err, types.ErrInvalidGenesis)
			require.Contains(t, err.Error(), tc.reason)
		})
	}
}
