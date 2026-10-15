package types_test

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/launchpad/types"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
)

func account(fill byte) string { return sdk.AccAddress(bytes.Repeat([]byte{fill}, 20)).String() }

func nativeMarket() types.Market {
	snapshots := make([]sdk.Int, types.SnapshotSlots)
	for i := range snapshots {
		snapshots[i] = sdk.ZeroInt()
	}
	return types.Market{
		Denom: "factory/launchpad/alpha", Index: 1, Name: "Alpha", Symbol: "ALP",
		Pointer: "0x0000000000000000000000000000000000000AbC", Creator: account(1), Guardian: account(1),
		FeeRightsHolder: account(1), FeeStrategy: types.FeeStrategyClaim, TotalSupply: sdk.NewInt(1_000),
		VirtualQuoteReserve: sdk.NewInt(10), RealQuoteBalance: sdk.ZeroInt(), TokenReserve: sdk.NewInt(1_000),
		CumulativeVolume: sdk.ZeroInt(), PriceSnapshots: snapshots, AccumulatedQuoteFees: sdk.ZeroInt(),
		AccumulatedTokenFees: sdk.ZeroInt(), AccumulatedFees: sdk.ZeroInt(), AirdropBalance: sdk.ZeroInt(),
	}
}

func poolMarket() types.Market {
	m := nativeMarket()
	m.Generation = types.Generation2
	m.PoolAddress = "0xCE20Da50F92D21d8BeE497910c782E9Aa25b6263"
	m.TokenAddress = common.BytesToAddress([]byte{0x7b, 0xf2}).Hex()
	m.Optical = common.Address{}.Hex()
	m.Factory = "0x3993D004166738d1069B5C4e0a923f7B9D5D8830"
	m.FeeAccumulator = "0x7b14808b72f61664cDb79D25E6d1587655571a66"
	m.ProtocolConfig = "0xF3669eDc475Cb6E1F0d7635240ABD8e8FB7E20C6"
	m.EventEmitter = common.BytesToAddress([]byte{0x62, 0x2e}).Hex()
	m.Guardian = account(2)
	m.PendingGuardian = account(3)
	m.Paused = true
	m.LastSnapshotTimestamp = 1_700_000_060
	m.CumulativeBuyQuoteVolume = sdk.NewInt(700)
	m.CumulativeSellQuoteVolume = sdk.NewInt(300)
	m.FeeRightCollection = "0xe8DcC844a8006dbf64f5Bb851bCad90292a6409B"
	m.FeeRightID = 7
	return m
}

func TestMarketValidate(t *testing.T) {
	require.NoError(t, nativeMarket().Validate())
	require.NoError(t, poolMarket().Validate())

	gen1 := poolMarket()
	gen1.Generation = types.Generation1
	gen1.PendingGuardian = ""
	gen1.Paused = false
	gen1.CumulativeBuyQuoteVolume = sdk.ZeroInt()
	gen1.CumulativeSellQuoteVolume = sdk.ZeroInt()
	require.NoError(t, gen1.Validate())

	encoded, err := json.Marshal(poolMarket())
	require.NoError(t, err)
	for _, field := range []string{`"generation":2`, `"pool_address":"0xCE20Da50F92D21d8BeE497910c782E9Aa25b6263"`,
		`"paused":true`, `"pending_guardian":"` + account(3) + `"`, `"last_snapshot_timestamp":1700000060`,
		`"cumulative_buy_quote_volume":"700"`, `"cumulative_sell_quote_volume":"300"`, `"fee_right_id":7`,
		`"fee_right_collection":"0xe8DcC844a8006dbf64f5Bb851bCad90292a6409B"`} {
		require.Contains(t, string(encoded), field)
	}
	var decoded types.Market
	require.NoError(t, json.Unmarshal(encoded, &decoded))
	require.Equal(t, poolMarket(), decoded)

	zero := common.Address{}.Hex()
	invalid := map[string]func(*types.Market){
		"generation 3":             func(m *types.Market) { m.Generation = 3 },
		"pool address empty":       func(m *types.Market) { m.PoolAddress = "" },
		"pool address zero":        func(m *types.Market) { m.PoolAddress = zero },
		"pool address lower case":  func(m *types.Market) { m.PoolAddress = "0xce20da50f92d21d8bee497910c782e9aa25b6263" },
		"pool address no prefix":   func(m *types.Market) { m.PoolAddress = "CE20Da50F92D21d8BeE497910c782E9Aa25b6263" },
		"pool address short":       func(m *types.Market) { m.PoolAddress = "0xCE20Da50F92D21d8BeE497910c782E9Aa25b62" },
		"token address zero":       func(m *types.Market) { m.TokenAddress = zero },
		"optical empty":            func(m *types.Market) { m.Optical = "" },
		"factory zero":             func(m *types.Market) { m.Factory = zero },
		"fee accumulator zero":     func(m *types.Market) { m.FeeAccumulator = zero },
		"protocol config zero":     func(m *types.Market) { m.ProtocolConfig = zero },
		"event emitter zero":       func(m *types.Market) { m.EventEmitter = zero },
		"fee right collection bad": func(m *types.Market) { m.FeeRightCollection = "0x1234" },
		"pending guardian hex":     func(m *types.Market) { m.PendingGuardian = m.PoolAddress },
		"guardian empty":           func(m *types.Market) { m.Guardian = "" },
		"negative timestamp":       func(m *types.Market) { m.LastSnapshotTimestamp = -1 },
		"buy volume nil":           func(m *types.Market) { m.CumulativeBuyQuoteVolume = sdk.Int{} },
		"sell volume negative":     func(m *types.Market) { m.CumulativeSellQuoteVolume = sdk.NewInt(-1) },
		"fee right id zero":        func(m *types.Market) { m.FeeRightID = 0 },
	}
	for name, mutate := range invalid {
		m := poolMarket()
		mutate(&m)
		require.ErrorIs(t, m.Validate(), types.ErrInvalidMarket, name)
	}

	native := map[string]func(*types.Market){
		"pool address":       func(m *types.Market) { m.PoolAddress = poolMarket().PoolAddress },
		"optical":            func(m *types.Market) { m.Optical = zero },
		"fee right":          func(m *types.Market) { m.FeeRightCollection = poolMarket().FeeRightCollection },
		"pending guardian":   func(m *types.Market) { m.PendingGuardian = account(3) },
		"snapshot timestamp": func(m *types.Market) { m.LastSnapshotTimestamp = 1 },
		"buy volume":         func(m *types.Market) { m.CumulativeBuyQuoteVolume = sdk.NewInt(1) },
		"sell volume":        func(m *types.Market) { m.CumulativeSellQuoteVolume = sdk.NewInt(-1) },
		"fee right id":       func(m *types.Market) { m.FeeRightID = 1 },
	}
	for name, mutate := range native {
		m := nativeMarket()
		mutate(&m)
		require.ErrorIs(t, m.Validate(), types.ErrInvalidMarket, name)
	}
	paused := nativeMarket()
	paused.Paused = true
	require.NoError(t, paused.Validate())

	// The keeper stores a market with json.Marshal and reads it with
	// json.Unmarshal: nil native volumes come back as non-nil zeros.
	stored, err := json.Marshal(nativeMarket())
	require.NoError(t, err)
	require.Contains(t, string(stored), `"cumulative_buy_quote_volume":"0"`)
	require.Contains(t, string(stored), `"cumulative_sell_quote_volume":"0"`)
	var loaded types.Market
	require.NoError(t, json.Unmarshal(stored, &loaded))
	require.False(t, loaded.CumulativeBuyQuoteVolume.IsNil())
	require.False(t, loaded.CumulativeSellQuoteVolume.IsNil())
	require.True(t, loaded.CumulativeBuyQuoteVolume.IsZero())
	require.True(t, loaded.CumulativeSellQuoteVolume.IsZero())
	require.NoError(t, loaded.Validate())
	genesis := types.DefaultGenesis()
	genesis.Markets = []types.Market{loaded}
	exported, err := json.Marshal(genesis)
	require.NoError(t, err)
	var imported types.GenesisState
	require.NoError(t, json.Unmarshal(exported, &imported))
	require.NoError(t, imported.Validate())

	pool := [20]byte(common.HexToAddress("0xCE20Da50F92D21d8BeE497910c782E9Aa25b6263"))
	token := [20]byte(common.HexToAddress("0x85FcD13735F4309833A503EE804ea32395851479"))
	var role [32]byte
	role[0], role[31] = 0xaa, 0xbb
	cat := func(parts ...[]byte) []byte { return bytes.Join(parts, nil) }
	keys := []struct {
		name string
		got  []byte
		want []byte
	}{
		{"surface", types.SurfaceKey(pool), cat([]byte{0x30}, pool[:])},
		{"role", types.RoleKey(pool, role, token), cat([]byte{0x31}, pool[:], role[:], token[:])},
		{"held quote", types.HeldQuoteKey(pool), cat([]byte{0x32}, pool[:])},
		{"fee held", types.FeeHeldKey(pool), cat([]byte{0x33}, pool[:])},
		{"treasury ledger", types.TreasuryLedgerKey(token), cat([]byte{0x34}, token[:])},
		{"treasury held", types.TreasuryHeldKey(token), cat([]byte{0x35}, token[:])},
		{"book held delta", types.BookHeldDeltaKey(pool, token), cat([]byte{0x36}, pool[:], token[:])},
		{"fee right id map", types.FeeRightIDMapKey(258), []byte{0x37, 0, 0, 0, 0, 0, 0, 1, 2}},
		{"retired", types.RetiredKey(token), cat([]byte{0x38}, token[:])},
		{"migrated", types.MigratedKey, []byte{0x39}},
	}
	require.Len(t, types.RoleKey(pool, role, token), 73)
	prefixes := map[byte]string{}
	for _, existing := range [][]byte{types.ParamsKey, types.MarketCountKey, types.ProtocolFeesPrefix, types.MarketPrefix,
		types.MarketIndexPrefix, types.MarketPointerPrefix, types.CreatorMarketPrefix, types.AirdropEpochPrefix,
		types.AirdropClaimPrefix, types.HoldingHistoryPrefix, types.HoldingCountPrefix, types.HoldingEntryPrefix,
		types.AirdropBasisPrefix} {
		prefixes[existing[0]] = "existing"
	}
	for _, key := range keys {
		require.Equal(t, key.want, key.got, key.name)
		_, taken := prefixes[key.got[0]]
		require.False(t, taken, "prefix of %s collides with %s", key.name, prefixes[key.got[0]])
		prefixes[key.got[0]] = key.name
	}
}
