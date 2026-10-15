package launchpad_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
)

var directRoles = []string{"Router", "SidioraFactory", "Quoter", "PoolRegistry", "FeesRouter", "FeeAccumulator", "ProtocolConfig"}

func parseABIFile(t *testing.T, path string) abi.ABI {
	t.Helper()
	f, err := os.Open(path)
	require.NoError(t, err)
	defer f.Close()
	parsed, err := abi.JSON(f)
	require.NoError(t, err)
	return parsed
}

func TestDirectABIFile(t *testing.T) {
	direct := parseABIFile(t, "abi.json")

	owner := map[string]string{}
	ownerMethod := map[string]abi.Method{}
	declaredBy := map[string]int{}
	for _, role := range directRoles {
		for _, m := range parseABIFile(t, filepath.Join("abi", role+".json")).Methods {
			declaredBy[m.Sig]++
			if _, ok := owner[m.Sig]; !ok {
				owner[m.Sig] = role
				ownerMethod[m.Sig] = m
			}
		}
	}

	shared := map[string]string{}
	for sig, n := range declaredBy {
		if n > 1 {
			shared[sig] = owner[sig]
		}
	}
	require.Equal(t, map[string]string{
		"DEFAULT_ADMIN_ROLE()":                      "Router",
		"createMarket(string,string,uint8,address)": "Router",
		"getRoleAdmin(bytes32)":                     "Router",
		"grantRole(bytes32,address)":                "Router",
		"hasRole(bytes32,address)":                  "Router",
		"poolRegistry()":                            "Router",
		"protocolConfig()":                          "Router",
		"proxiableUUID()":                           "Router",
		"registeredDefaultAdminCount()":             "Router",
		"renounceRole(bytes32,address)":             "Router",
		"revokeRole(bytes32,address)":               "Router",
		"upgradeToAndCall(address,bytes)":           "Router",
		"usdlAddress()":                             "Router",
		"eventEmitter()":                            "SidioraFactory",
		"getAllPools(uint256,uint256)":              "Quoter",
		"getPoolsByCreator(address)":                "Quoter",
		"feeAccumulator()":                          "SidioraFactory",
		"nftContract()":                             "SidioraFactory",
		"FACTORY_ROLE()":                            "PoolRegistry",
		"treasury()":                                "SidioraFactory",
		"initialize(address,address,address)":       "Quoter",
	}, shared)

	require.Len(t, direct.Methods, 113)
	require.Len(t, owner, 113)
	selectors := map[[4]byte]string{}
	for _, m := range direct.Methods {
		var id [4]byte
		copy(id[:], m.ID)
		prev, dup := selectors[id]
		require.False(t, dup, "selector %x of %s already used by %s", id, m.Sig, prev)
		selectors[id] = m.Sig

		want, ok := ownerMethod[m.Sig]
		require.True(t, ok, "%s is declared by no direct role", m.Sig)
		require.Equal(t, want.String(), m.String(), m.Sig)
		require.Equal(t, want.StateMutability, m.StateMutability, m.Sig)
	}

	createMarket, err := direct.MethodById(common.FromHex("0x57c0d7fd"))
	require.NoError(t, err)
	require.Equal(t, "createMarket(string,string,uint8,address)", createMarket.Sig)
	require.Equal(t, "Router", owner[createMarket.Sig])
	getAllPools, err := direct.MethodById(common.FromHex("0xa7753f09"))
	require.NoError(t, err)
	require.Equal(t, "Quoter", owner[getAllPools.Sig])

	events := map[string]common.Hash{}
	for _, e := range direct.Events {
		events[e.Sig] = e.ID
	}
	require.Equal(t, map[string]common.Hash{
		"Swap(bytes32,address,bool,uint256,uint256,uint256,uint256,uint256,uint256)":                 common.HexToHash("0xa234ab85728295548823d8a26077ecc77aa5815f9ed8f4c5e6fd390ac851d4cc"),
		"PoolStateUpdated(bytes32,uint256,uint256,uint256,uint256,uint256,uint256)":                  common.HexToHash("0x8155a4b38a877d212b271811786986f8f7d78074c82fce4526c58defcc6d7ebd"),
		"TokenDeployed(address,address,address,bytes32,string,string,uint8,uint256,uint256,uint256)": common.HexToHash("0xad187874b793634c55d977f35d4828526eb48170a9531c71d88f8fd10cf9c237"),
		"MarketCreated(bytes32,address,address,address,address,uint256,uint256)":                     common.HexToHash("0xb0a9c7605389f1127c2299d4a3ed85f0b2fc19894ab5411d76b453bb29f9f47b"),
		"NftMint(uint256,address,address,uint8,uint256,uint256)":                                     common.HexToHash("0xd2917d37031c0dfe5016665c7a83360cbf2ff32063096b2caf61ba66c06832e5"),
		"FeeStrategyChanged(bytes32,uint256,uint8,uint8,uint256,uint256)":                            common.HexToHash("0x337fd9f9304e9da4c04886a5b8bf56e74032b459758212a860bc61d3544353dd"),
		"NftTransfer(address,address,address,uint256,uint256,uint256)":                               common.HexToHash("0x021f03e8a1315704c74c03e8c022c12bc80c4a2bd4fec8bd36b217322c340ad3"),
		"PoolRegistered(address,address,address,address,uint256,uint256,uint256)":                    common.HexToHash("0xda5568642b67660133b1de538c867c58f3037a5722b0e231a959ee8ca11b2ed7"),
		"RouterTrade(uint8,address,address,address,address,uint256,uint256,uint256,uint256,uint256)": common.HexToHash("0x17c1899a3fd431f2b2200b90e738d270bb856a4222040c5650bfa208acb3b650"),
		"ConfigUpdated(bytes32,uint256,uint256,uint256,uint256)":                                     common.HexToHash("0x2903b3642d2105b7fbd127326de888ab50e8d975bf819398337fb58407c1b10e"),
		"FeeFlow(uint8,address,address,uint256,uint256,uint256,uint256,uint256,uint256)":             common.HexToHash("0x626e3fd5d9323ca4b7ffa3ca51c21c02766b638d15eb8896a36a4e0e97ae6de4"),
		"FeeDistributed(bytes32,uint256,uint8,uint256,address,uint256,uint256)":                      common.HexToHash("0xf7ab826cedf9d005102eddf95e65f43597aef36a55ff09ee4f452204c83f4afc"),
	}, events)
	require.Empty(t, direct.Errors)
}
