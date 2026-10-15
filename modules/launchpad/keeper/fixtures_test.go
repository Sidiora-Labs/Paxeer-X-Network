package keeper_test

import (
	"bytes"
	"encoding/json"
	"math/big"
	"os"
	"path/filepath"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/stretchr/testify/require"
)

const v6120FixtureDir = "testdata/v6120"

type v6120USDLBalance struct {
	Generation uint8  `json:"generation"`
	Role       string `json:"role"`
	Address    string `json:"address"`
	Balance    string `json:"balance"`
}

type v6120USDLFixture struct {
	Height      uint64             `json:"height"`
	Address     string             `json:"address"`
	CodeHash    string             `json:"codeHash"`
	TotalSupply string             `json:"totalSupply"`
	Balances    []v6120USDLBalance `json:"balances"`
}

type v6120TokenHolders struct {
	Token   string   `json:"token"`
	Holders []string `json:"holders"`
}

type v6120StorageSlot struct {
	Slot  uint64 `json:"slot"`
	Field string `json:"field"`
	Words uint64 `json:"words,omitempty"`
}

type v6120Generation struct {
	Generation                 uint8    `json:"generation"`
	SidioraFactory             string   `json:"sidioraFactory"`
	PoolRegistry               string   `json:"poolRegistry"`
	Treasury                   string   `json:"treasury"`
	PoolBeacon                 string   `json:"poolBeacon"`
	PoolProxyCodeHash          string   `json:"poolProxyCodeHash"`
	PoolImplementation         string   `json:"poolImplementation"`
	PoolImplementationCodeHash string   `json:"poolImplementationCodeHash"`
	PoolCount                  uint64   `json:"poolCount"`
	PoolSlotsAbsent            []uint64 `json:"poolSlotsAbsent"`
}

type v6120LayoutFixture struct {
	Height       uint64                        `json:"height"`
	Treasury     string                        `json:"treasury"`
	BeaconSlot   string                        `json:"beaconSlot"`
	PausableSlot string                        `json:"pausableSlot"`
	Generations  []v6120Generation             `json:"generations"`
	Storage      map[string][]v6120StorageSlot `json:"storage"`
}

type v6120Delta struct {
	Pool         string `json:"pool"`
	Asset        string `json:"asset"`
	AssetAddress string `json:"assetAddress"`
	Book         string `json:"book"`
	Held         string `json:"held"`
	Delta        string `json:"delta"`
}

type v6120PoolsFixture struct {
	Height    uint64       `json:"height"`
	Tolerance string       `json:"tolerance"`
	Deltas    []v6120Delta `json:"deltas"`
}

func decodeV6120Fixture(t *testing.T, name string, out any) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(v6120FixtureDir, name))
	require.NoError(t, err)
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	require.NoError(t, dec.Decode(out), name)
	require.False(t, dec.More(), "%s carries trailing data", name)
}

func requireChecksumAddress(t *testing.T, s string) common.Address {
	t.Helper()
	require.True(t, common.IsHexAddress(s), "%q is not an address", s)
	a := common.HexToAddress(s)
	require.Equal(t, a.Hex(), s, "address not in checksum form")
	require.NotEqual(t, common.Address{}, a)
	return a
}

func requireHash(t *testing.T, s string) {
	t.Helper()
	b, err := hexutil.Decode(s)
	require.NoError(t, err, s)
	require.Len(t, b, common.HashLength, s)
	require.NotEqual(t, common.Hash{}, common.BytesToHash(b))
}

func requireAmount(t *testing.T, s string, signed bool) *big.Int {
	t.Helper()
	v, ok := new(big.Int).SetString(s, 10)
	require.True(t, ok, "%q is not a base-10 integer", s)
	require.Equal(t, v.String(), s, "amount not in canonical form")
	if !signed {
		require.GreaterOrEqual(t, v.Sign(), 0, s)
	}
	return v
}

func TestFixtureFilesParse(t *testing.T) {
	var usdl v6120USDLFixture
	var holders []v6120TokenHolders
	var layout v6120LayoutFixture
	var pools v6120PoolsFixture
	decodeV6120Fixture(t, "usdl.json", &usdl)
	decodeV6120Fixture(t, "holders.json", &holders)
	decodeV6120Fixture(t, "layout.json", &layout)
	decodeV6120Fixture(t, "pools.json", &pools)

	require.NotZero(t, usdl.Height)
	require.Equal(t, usdl.Height, layout.Height)
	require.Equal(t, usdl.Height, pools.Height)

	// usdl.json
	require.Equal(t, "0x85FcD13735F4309833A503EE804ea32395851479", usdl.Address)
	requireChecksumAddress(t, usdl.Address)
	require.Equal(t, "0xa5d84d5e9db89e88646654510a9445271bdd67c3b0e84c64e923c6b82589aaaf", usdl.CodeHash)
	requireHash(t, usdl.CodeHash)
	require.Equal(t, "30000000000000", usdl.TotalSupply)
	supply := requireAmount(t, usdl.TotalSupply, false)

	usdlBalance := map[common.Address]*big.Int{}
	roleAddr := map[uint8]map[string]common.Address{1: {}, 2: {}}
	poolsByGen := map[uint8][]common.Address{}
	sum := new(big.Int)
	for _, b := range usdl.Balances {
		a := requireChecksumAddress(t, b.Address)
		_, dup := usdlBalance[a]
		require.False(t, dup, "duplicate usdl balance %s", b.Address)
		v := requireAmount(t, b.Balance, false)
		usdlBalance[a] = v
		sum.Add(sum, v)
		require.Contains(t, roleAddr, b.Generation)
		if b.Role == "SidioraPool" {
			poolsByGen[b.Generation] = append(poolsByGen[b.Generation], a)
			continue
		}
		_, dupRole := roleAddr[b.Generation][b.Role]
		require.False(t, dupRole, "duplicate role %d %s", b.Generation, b.Role)
		roleAddr[b.Generation][b.Role] = a
	}
	require.LessOrEqual(t, sum.Cmp(supply), 0)
	require.Len(t, poolsByGen[1], 45)
	require.Len(t, poolsByGen[2], 4)
	require.Equal(t, poolsByGen[1][0], common.HexToAddress("0xEeA16aC55AaE47c484EBef02eb1186aD606e1BF9"))
	require.Contains(t, poolsByGen[2], common.HexToAddress("0xCE20Da50F92D21d8BeE497910c782E9Aa25b6263"))
	roles := []string{"SidioraFactory", "Router", "Quoter", "FeesRouter", "FeeAccumulator", "PoolRegistry", "ProtocolConfig", "SidioraNFT", "Treasury", "OpticalRegistry", "EventEmitter", "PoolBeacon"}
	for _, g := range []uint8{1, 2} {
		require.Len(t, roleAddr[g], len(roles))
		for _, r := range roles {
			require.Contains(t, roleAddr[g], r)
		}
	}
	require.Equal(t, common.HexToAddress("0x15405D535ce533BfFb98c83e42f4DD242AA5e079"), roleAddr[1]["Treasury"])
	require.Equal(t, common.HexToAddress("0x50C69dF6637b3DCE6a7407C5A4b4F99E68514A76"), roleAddr[1]["FeeAccumulator"])
	require.Equal(t, common.HexToAddress("0x7b14808b72f61664cDb79D25E6d1587655571a66"), roleAddr[2]["FeeAccumulator"])
	require.Equal(t, common.HexToAddress("0x3993D004166738d1069B5C4e0a923f7B9D5D8830"), roleAddr[2]["SidioraFactory"])

	// layout.json
	g2Treasury := requireChecksumAddress(t, layout.Treasury)
	require.Equal(t, roleAddr[2]["Treasury"], g2Treasury)
	require.Equal(t, common.HexToAddress("0xBb82d74E071e03d8E68F1d78Fae9EFe8b6b7A723"), g2Treasury)
	require.Equal(t, "0xa3f0ad74e5423aebfd80d3ef4346578335a9a72aeaee59ff6cb3582b35133d50", layout.BeaconSlot)
	require.Equal(t, "0x62e78cea01bee320cd4e420270b5ea74000d11b0c9f74754ebdbfc544b05a258", layout.PausableSlot)
	require.Len(t, layout.Generations, 2)
	proxyHashes := map[string]bool{}
	for i, g := range layout.Generations {
		require.Equal(t, uint8(i+1), g.Generation)
		require.Equal(t, roleAddr[g.Generation]["SidioraFactory"], requireChecksumAddress(t, g.SidioraFactory))
		require.Equal(t, roleAddr[g.Generation]["PoolRegistry"], requireChecksumAddress(t, g.PoolRegistry))
		require.Equal(t, roleAddr[g.Generation]["Treasury"], requireChecksumAddress(t, g.Treasury))
		require.Equal(t, roleAddr[g.Generation]["PoolBeacon"], requireChecksumAddress(t, g.PoolBeacon))
		requireChecksumAddress(t, g.PoolImplementation)
		requireHash(t, g.PoolProxyCodeHash)
		requireHash(t, g.PoolImplementationCodeHash)
		require.False(t, proxyHashes[g.PoolProxyCodeHash], "pool proxy code hash repeated")
		proxyHashes[g.PoolProxyCodeHash] = true
		require.Equal(t, uint64(len(poolsByGen[g.Generation])), g.PoolCount)
	}
	require.Equal(t, []uint64{24, 25, 26, 27, 28}, layout.Generations[0].PoolSlotsAbsent)
	require.Empty(t, layout.Generations[1].PoolSlotsAbsent)
	require.Equal(t, "0xeeadddb17507c0b32fb9c1c76f66b33b4757efd3531d86ac6a61c96d81d8bde4", layout.Generations[1].PoolImplementationCodeHash)
	require.Equal(t, v6120Section14Storage(), layout.Storage)

	// holders.json
	poolSet := map[common.Address]bool{}
	for _, g := range []uint8{1, 2} {
		for _, p := range poolsByGen[g] {
			poolSet[p] = true
		}
	}
	tokenOfPool := map[common.Address]common.Address{}
	tokens := map[common.Address]bool{}
	for _, th := range holders {
		tok := requireChecksumAddress(t, th.Token)
		require.False(t, tokens[tok], "token %s listed twice", th.Token)
		tokens[tok] = true
		seen := map[common.Address]bool{}
		var pool common.Address
		poolsHere := 0
		for _, h := range th.Holders {
			a := requireChecksumAddress(t, h)
			require.False(t, seen[a], "holder %s listed twice for %s", h, th.Token)
			seen[a] = true
			if poolSet[a] {
				pool = a
				poolsHere++
			}
		}
		require.Equal(t, 1, poolsHere, "token %s must list exactly its pool", th.Token)
		_, dup := tokenOfPool[pool]
		require.False(t, dup)
		tokenOfPool[pool] = tok
	}
	require.Len(t, holders, len(poolSet))
	require.Len(t, tokenOfPool, len(poolSet))

	// pools.json
	tolerance := requireAmount(t, pools.Tolerance, false)
	require.Zero(t, tolerance.Sign())
	type want struct{ pool, asset, delta string }
	wants := []want{
		{"0x90665CF00C471060dfC8db4c3447e50e1092fC8B", "usdl", "-100000000"},
		{"0x67e1b3d89C1C660f06589A94D9E58d5f540c83F5", "usdl", "-50000000"},
		{"0x26Fd86Ef00F40e204dF12f652d0C0527a17c49EA", "usdl", "-1508850948"},
		{"0x67e1b3d89C1C660f06589A94D9E58d5f540c83F5", "token", "-130368747608"},
		{"0x7bF2f96443A6D7b122DD73Ce79205c85eEdae738", "token", "-5000000000000"},
	}
	require.Len(t, pools.Deltas, len(wants))
	for i, d := range pools.Deltas {
		pool := requireChecksumAddress(t, d.Pool)
		require.True(t, poolSet[pool], "delta pool %s is not a pool", d.Pool)
		require.Equal(t, wants[i].asset, d.Asset)
		require.Equal(t, wants[i].delta, d.Delta)
		require.Equal(t, wants[i].pool, d.Pool)
		book := requireAmount(t, d.Book, false)
		held := requireAmount(t, d.Held, false)
		delta := requireAmount(t, d.Delta, true)
		require.Equal(t, 0, new(big.Int).Sub(held, book).Cmp(delta))
		require.Negative(t, delta.Sign())
		asset := requireChecksumAddress(t, d.AssetAddress)
		switch d.Asset {
		case "usdl":
			require.Equal(t, common.HexToAddress(usdl.Address), asset)
			require.Equal(t, 0, usdlBalance[pool].Cmp(held), "usdl held of %s differs from usdl.json", d.Pool)
		case "token":
			require.Equal(t, tokenOfPool[pool], asset)
		}
	}
}

func v6120Section14Storage() map[string][]v6120StorageSlot {
	seq := func(fields ...string) []v6120StorageSlot {
		out := make([]v6120StorageSlot, len(fields))
		for i, f := range fields {
			out[i] = v6120StorageSlot{Slot: uint64(i), Field: f}
		}
		return out
	}
	pool := seq("tokenAddress", "usdlAddress", "opticalAddress", "feeAccumulator", "eventEmitter", "protocolConfig", "guardian", "virtualUsdlReserve", "realUsdlBalance", "tokenReserve", "creationTimestamp", "cumulativeVolume")
	pool = append(pool, v6120StorageSlot{Slot: 12, Field: "priceSnapshots", Words: 8})
	for i, f := range []string{"snapshotIndex", "snapshotCount", "accumulatedUsdlFees", "accumulatedTokenFees", "factory", "pendingGuardian", "lastSnapshotTimestamp", "cumulativeBuyUsdlVolume", "cumulativeSellUsdlVolume"} {
		pool = append(pool, v6120StorageSlot{Slot: uint64(20 + i), Field: f})
	}
	return map[string][]v6120StorageSlot{
		"SidioraERC20":   seq("name", "symbol", "totalSupply", "balanceOf", "allowance", "nonces", "_balanceCheckpoints", "_supplyCheckpoints", "pool"),
		"SidioraFactory": seq("_roles", "poolBeacon", "nftContract", "poolRegistry", "eventEmitter", "protocolConfig", "treasury", "feeAccumulator", "usdlAddress", "_nonces"),
		"SidioraNFT":     seq("name", "symbol", "_owners", "_balances", "_tokenApprovals", "_operatorApprovals", "_allTokens", "_allTokensIndex", "_ownedTokens", "_ownedTokensIndex", "_roles", "nextTokenId", "_feeStrategy", "_poolAddress", "eventEmitter", "poolTokenId", "poolIndexMigrationCursor", "poolIndexReady"),
		"SidioraPool":    pool,
		"FeeAccumulator": seq("_roles", "protocolConfig", "treasury", "poolRegistry", "eventEmitter", "usdlAddress", "_accumulatedFees", "_protocolFeesPending", "_airdropBalance", "_airdropEpoch", "_airdropEpochAmount", "_airdropClaimed", "_lpRewardsBalance", "_opticalSurplus", "totalLiabilities", "airdropSnapshotBlock", "airdropEligibleSupply", "airdropEpochRemaining", "airdropClaimedSupply", "accountingInitialized", "accountingMigrationCursor", "_accountingMigratedPool", "legacyAirdropUnallocated", "legacyAirdropRoot", "legacyAirdropRemaining"),
		"PoolRegistry":   seq("_roles", "_tokenToPool", "_poolToNftId", "_creatorToPools", "_poolMetadata", "_registeredPools", "_allPools", "eventEmitter"),
		"FeesRouter":     seq("_roles", "nftContract", "feeAccumulator", "poolRegistry"),
		"Quoter":         seq("_roles", "poolRegistry", "protocolConfig"),
		"Router":         seq("_roles", "factory", "poolRegistry", "protocolConfig", "usdlAddress"),
		"ProtocolConfig": seq("_roles", "usdlAddress", "virtualUsdlDefault", "virtualTokenDefault", "minFeeBps", "maxFeeBps", "baseFeeBps", "protocolFeeBps", "feeDecayRate", "volatilityWeight", "concentrationWeight", "creationFee", "eventEmitter"),
		"Treasury":       seq("_roles", "_balances", "eventEmitter"),
	}
}
