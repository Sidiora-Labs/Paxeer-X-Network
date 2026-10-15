package launchpad

import (
	"bufio"
	"fmt"
	"os"
	"sort"
	"testing"

	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/launchpad/types"
	pcommon "github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/common"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/require"
)

var directRoleOrder = []string{"Router", "SidioraFactory", "Quoter", "PoolRegistry", "FeesRouter", "FeeAccumulator", "ProtocolConfig"}

func readGolden(t *testing.T) []string {
	t.Helper()
	file, err := os.Open("testdata/abi_golden.txt")
	require.NoError(t, err)
	defer file.Close()
	var lines []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	require.NoError(t, scanner.Err())
	return lines
}

func sortedSigs[T any](m map[string]T, sig func(T) string) []T {
	items := make([]T, 0, len(m))
	for _, v := range m {
		items = append(items, v)
	}
	sort.Slice(items, func(i, j int) bool { return sig(items[i]) < sig(items[j]) })
	return items
}

// selectorLines renders the functions, events and errors of one role as
// golden lines and asserts that no selector of the role maps to two
// signatures.
func selectorLines(t *testing.T, role string, parsed abi.ABI) []string {
	t.Helper()
	var lines []string
	selectors := map[string]string{}
	add := func(sig string, id []byte) {
		key := fmt.Sprintf("0x%x", id)
		prev, dup := selectors[key]
		require.False(t, dup, "%s: selector %s of %s already used by %s", role, key, sig, prev)
		selectors[key] = sig
		lines = append(lines, fmt.Sprintf("%s %s %s", role, sig, key))
	}
	for _, m := range sortedSigs(parsed.Methods, func(m abi.Method) string { return m.Sig }) {
		id := crypto.Keccak256([]byte(m.Sig))[:4]
		require.Equal(t, id, m.ID, m.Sig)
		add(m.Sig, id)
	}
	for _, e := range sortedSigs(parsed.Events, func(e abi.Event) string { return e.Sig }) {
		id := crypto.Keccak256([]byte(e.Sig))
		require.Equal(t, common.BytesToHash(id), e.ID, e.Sig)
		add(e.Sig, id)
	}
	for _, e := range sortedSigs(parsed.Errors, func(e abi.Error) string { return e.Sig }) {
		id := crypto.Keccak256([]byte(e.Sig))[:4]
		require.Equal(t, id, e.ID[:4], e.Sig)
		add(e.Sig, id)
	}
	return lines
}

func TestRoleABIGolden(t *testing.T) {
	require.Len(t, roleABIs, 11)
	counts := map[string][3]int{
		"SidioraPool":    {42, 6, 19},
		"SidioraFactory": {23, 6, 16},
		"SidioraERC20":   {19, 2, 7},
		"SidioraNFT":     {37, 10, 22},
		"Router":         {23, 9, 21},
		"Quoter":         {19, 5, 18},
		"FeesRouter":     {20, 12, 16},
		"FeeAccumulator": {54, 17, 23},
		"PoolRegistry":   {22, 7, 16},
		"ProtocolConfig": {27, 6, 13},
		"Treasury":       {17, 8, 19},
	}
	var want []string
	for _, role := range roles {
		parsed, ok := roleABIs[role]
		require.True(t, ok, role)
		require.Equal(t, counts[role], [3]int{len(parsed.Methods), len(parsed.Events), len(parsed.Errors)}, role)
		if role == "Treasury" {
			direct := pcommon.MustGetABI(f, "abi.json")
			require.Len(t, direct.Events, 12)
			want = append(want, selectorLines(t, "EventEmitter", abi.ABI{Events: direct.Events})...)
			require.Len(t, want, 549)
		}
		want = append(want, selectorLines(t, role, parsed)...)
	}
	require.Len(t, want, 593)
	for _, err := range []error{types.ErrAccessControlUnauthorizedAccount, types.ErrRetired} {
		contractErr, ok := types.ContractErrorOf(err)
		require.True(t, ok)
		want = append(want, fmt.Sprintf("Chain %s 0x%x", contractErr.Signature(), contractErr.Selector))
	}

	golden := readGolden(t)
	require.Len(t, golden, 595)
	require.Equal(t, want, golden)
	require.Equal(t, "Chain AccessControlUnauthorizedAccount(address,bytes32) 0xe2517d3f", golden[593])
	require.Equal(t, "Chain Retired(address) 0xda9d2e31", golden[594])
}

func TestDirectABIResolution(t *testing.T) {
	direct := pcommon.MustGetABI(f, "abi.json")
	require.Len(t, direct.Methods, 113)

	servedBy := map[string]string{}
	servedMethod := map[string]abi.Method{}
	declaredBy := map[string][]string{}
	for _, role := range directRoleOrder {
		for _, m := range roleABIs[role].Methods {
			declaredBy[m.Sig] = append(declaredBy[m.Sig], role)
			if _, ok := servedBy[m.Sig]; !ok {
				servedBy[m.Sig] = role
				servedMethod[m.Sig] = m
			}
		}
	}
	require.Len(t, servedBy, 113)

	selectors := map[[4]byte]string{}
	for _, m := range direct.Methods {
		id := [4]byte(m.ID)
		prev, dup := selectors[id]
		require.False(t, dup, "selector %x of %s already used by %s", id, m.Sig, prev)
		selectors[id] = m.Sig

		want, ok := servedMethod[m.Sig]
		require.True(t, ok, "%s is declared by no direct role", m.Sig)
		require.Equal(t, want.String(), m.String(), m.Sig)
		require.Equal(t, want.ID, m.ID, m.Sig)
	}

	shared := map[string][]string{}
	for sig, decl := range declaredBy {
		if len(decl) > 1 {
			shared[sig] = decl
		}
	}
	all := directRoleOrder
	require.Equal(t, map[string][]string{
		"DEFAULT_ADMIN_ROLE()":                      all,
		"createMarket(string,string,uint8,address)": {"Router", "SidioraFactory"},
		"getRoleAdmin(bytes32)":                     all,
		"grantRole(bytes32,address)":                all,
		"hasRole(bytes32,address)":                  all,
		"poolRegistry()":                            {"Router", "SidioraFactory", "Quoter", "FeesRouter", "FeeAccumulator"},
		"protocolConfig()":                          {"Router", "SidioraFactory", "Quoter", "FeeAccumulator"},
		"proxiableUUID()":                           all,
		"registeredDefaultAdminCount()":             all,
		"renounceRole(bytes32,address)":             all,
		"revokeRole(bytes32,address)":               all,
		"upgradeToAndCall(address,bytes)":           all,
		"usdlAddress()":                             {"Router", "SidioraFactory", "FeeAccumulator", "ProtocolConfig"},
		"eventEmitter()":                            {"SidioraFactory", "PoolRegistry", "FeeAccumulator", "ProtocolConfig"},
		"getAllPools(uint256,uint256)":              {"Quoter", "PoolRegistry"},
		"getPoolsByCreator(address)":                {"Quoter", "PoolRegistry"},
		"feeAccumulator()":                          {"SidioraFactory", "FeesRouter"},
		"nftContract()":                             {"SidioraFactory", "FeesRouter"},
		"FACTORY_ROLE()":                            {"PoolRegistry", "FeeAccumulator"},
		"treasury()":                                {"SidioraFactory", "FeeAccumulator"},
		"initialize(address,address,address)":       {"Quoter", "ProtocolConfig"},
	}, shared)

	createMarket, err := direct.MethodById(common.FromHex("0x57c0d7fd"))
	require.NoError(t, err)
	require.Equal(t, "createMarket(string,string,uint8,address)", createMarket.Sig)
	require.Equal(t, "Router", servedBy[createMarket.Sig])
	getAllPools, err := direct.MethodById(common.FromHex("0xa7753f09"))
	require.NoError(t, err)
	require.Equal(t, "getAllPools(uint256,uint256)", getAllPools.Sig)
	require.Equal(t, "Quoter", servedBy[getAllPools.Sig])
}
