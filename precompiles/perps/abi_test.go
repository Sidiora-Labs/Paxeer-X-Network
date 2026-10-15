package perps_test

import (
	"bytes"
	"encoding/hex"
	"os"
	"sort"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/require"

	perpsabi "github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/perps/abi"
)

const goldenPath = "testdata/abi_golden.txt"

func loadPerpsABI(t *testing.T) abi.ABI {
	t.Helper()
	parsed, err := perpsabi.ABI()
	require.NoError(t, err)
	return parsed
}

func loadGolden(t *testing.T) []string {
	t.Helper()
	raw, err := os.ReadFile(goldenPath)
	require.NoError(t, err)
	require.True(t, bytes.HasSuffix(raw, []byte("\n")), "golden file must end with a newline")
	return strings.Split(strings.TrimSuffix(string(raw), "\n"), "\n")
}

func sortedLines(kind string, entries map[string]string) []string {
	sigs := make([]string, 0, len(entries))
	for sig := range entries {
		sigs = append(sigs, sig)
	}
	sort.Strings(sigs)
	lines := make([]string, 0, len(sigs))
	for _, sig := range sigs {
		lines = append(lines, kind+" "+sig+" "+entries[sig])
	}
	return lines
}

func renderGolden(parsed abi.ABI) []string {
	methods := make(map[string]string, len(parsed.Methods))
	for _, m := range parsed.Methods {
		methods[m.Sig] = "0x" + hex.EncodeToString(m.ID)
	}
	events := make(map[string]string, len(parsed.Events))
	for _, e := range parsed.Events {
		events[e.Sig] = e.ID.Hex()
	}
	errs := make(map[string]string, len(parsed.Errors))
	for _, e := range parsed.Errors {
		errs[e.Sig] = "0x" + hex.EncodeToString(e.ID[:4])
	}
	lines := sortedLines("method", methods)
	lines = append(lines, sortedLines("event", events)...)
	return append(lines, sortedLines("error", errs)...)
}

func collectStructs(typ abi.Type, into map[string]struct{}) {
	switch typ.T {
	case abi.TupleTy:
		into[typ.TupleRawName] = struct{}{}
		for _, elem := range typ.TupleElems {
			collectStructs(*elem, into)
		}
	case abi.SliceTy, abi.ArrayTy:
		collectStructs(*typ.Elem, into)
	}
}

func collectArgNames(typ abi.Type, into *[]string) {
	switch typ.T {
	case abi.TupleTy:
		*into = append(*into, typ.TupleRawName)
		*into = append(*into, typ.TupleRawNames...)
		for _, elem := range typ.TupleElems {
			collectArgNames(*elem, into)
		}
	case abi.SliceTy, abi.ArrayTy:
		collectArgNames(*typ.Elem, into)
	}
}

func methodBySig(t *testing.T, parsed abi.ABI, sig string) abi.Method {
	t.Helper()
	for _, m := range parsed.Methods {
		if m.Sig == sig {
			return m
		}
	}
	require.Failf(t, "method not declared", "%s", sig)
	return abi.Method{}
}

func tupleFields(typ abi.Type) []string {
	fields := make([]string, 0, len(typ.TupleElems))
	for i, elem := range typ.TupleElems {
		fields = append(fields, typ.TupleRawNames[i]+" "+elem.String())
	}
	return fields
}

func TestABIGoldenCounts(t *testing.T) {
	parsed := loadPerpsABI(t)
	require.Len(t, parsed.Methods, 67)
	require.Len(t, parsed.Events, 30)
	require.Len(t, parsed.Errors, 44)

	structs := make(map[string]struct{})
	for _, m := range parsed.Methods {
		for _, arg := range append(append(abi.Arguments{}, m.Inputs...), m.Outputs...) {
			collectStructs(arg.Type, structs)
		}
	}
	for _, e := range parsed.Events {
		for _, arg := range e.Inputs {
			collectStructs(arg.Type, structs)
		}
	}
	names := make([]string, 0, len(structs))
	for name := range structs {
		names = append(names, name)
	}
	sort.Strings(names)
	require.Equal(t, []string{
		"IPerpsAccountState",
		"IPerpsAccountSummary",
		"IPerpsClosePositionQuote",
		"IPerpsMarginTier",
		"IPerpsMarketConfig",
		"IPerpsMarketQuote",
		"IPerpsOpenPositionQuote",
		"IPerpsOrderDetails",
		"IPerpsOrderParams",
		"IPerpsPositionDetails",
		"IPerpsPositionMargin",
	}, names)

	kinds := map[string]int{}
	for _, line := range loadGolden(t) {
		kinds[strings.SplitN(line, " ", 2)[0]]++
	}
	require.Equal(t, map[string]int{"method": 67, "event": 30, "error": 44}, kinds)
}

func TestABIGoldenFile(t *testing.T) {
	want := loadGolden(t)
	got := renderGolden(loadPerpsABI(t))
	for i := 0; i < len(want) && i < len(got); i++ {
		require.Equalf(t, want[i], got[i], "%s line %d differs from ABI()", goldenPath, i+1)
	}
	require.Equal(t, len(want), len(got), "%s line count differs from ABI()", goldenPath)
}

func TestABICompatSelectors(t *testing.T) {
	// The 32 distinct methods the application encodes from its six perps hooks,
	// with the selector of the application's ABI signature.
	calls := []struct {
		sig      string
		selector string
	}{
		{"getPosition(uint256)", "0xeb02c301"},
		{"getUserPositionIds(address)", "0xeddf7570"},
		{"getOpenInterest(uint256)", "0x1b3ee0f1"},
		{"openPosition(uint256,address,uint256,uint256,bool)", "0x6d055f06"},
		{"closePosition(uint256)", "0xa126d601"},
		{"partialClose(uint256,uint256)", "0xeaa6185c"},
		{"addCollateral(uint256,uint256)", "0xa8f35adf"},
		{"removeCollateral(uint256,uint256)", "0x6a9b1891"},
		{"addSize(uint256,uint256,uint256)", "0x8347a72f"},
		{"getOrder(uint256)", "0xd09ef241"},
		{"getUserOrderIds(address)", "0xec641a1d"},
		{"placeLimitOrder(uint256,bool,uint256,uint256,uint256,address,uint256)", "0x569e792e"},
		{"placeStopLimitOrder(uint256,bool,uint256,uint256,uint256,uint256,address,uint256)", "0xa1d442c7"},
		{"cancelOrder(uint256)", "0x514fcac7"},
		{"placeTakeProfitOrder(uint256,uint256)", "0x4b10244d"},
		{"placeStopLossOrder(uint256,uint256)", "0x1a6c35e0"},
		{"getVault(address)", "0x0eb9af38"},
		{"createVault()", "0x5d12928b"},
		{"getBalance(address)", "0xf8b2cb4f"},
		{"getLockedBalance(address)", "0xc4086893"},
		{"deposit(address,uint256)", "0x47e7ef24"},
		{"withdraw(address,uint256)", "0xf3fef3a3"},
		{"getMarginMode()", "0x3ca0c5c5"},
		{"setMarginMode(uint8)", "0xbd5944c8"},
		{"getPositionMargin(uint256)", "0x91445f94"},
		{"topUpPosition(uint256,uint256)", "0x1efa1c55"},
		{"transferMargin(uint256,uint256,uint256)", "0xfc05c891"},
		{"getAccountSummary(address)", "0x43ad2977"},
		{"emergencyWithdraw(address)", "0x6ff1c9bc"},
		{"quoteMarket(uint256)", "0x3dc355fe"},
		{"quoteOpenPosition(uint256,address,uint256,uint256,bool)", "0xa7b85d21"},
		{"quoteClosePosition(uint256)", "0x0c849b70"},
	}
	golden := make(map[string]struct{})
	for _, line := range loadGolden(t) {
		golden[line] = struct{}{}
	}
	parsed := loadPerpsABI(t)
	distinct := make(map[string]struct{}, len(calls))
	for _, call := range calls {
		distinct[call.sig] = struct{}{}
		require.Equal(t, call.selector, "0x"+hex.EncodeToString(crypto.Keccak256([]byte(call.sig))[:4]), call.sig)
		require.Contains(t, golden, "method "+call.sig+" "+call.selector)
		require.Equal(t, call.selector, "0x"+hex.EncodeToString(methodBySig(t, parsed, call.sig).ID), call.sig)
	}
	require.Len(t, distinct, 32)

	for _, overload := range []struct {
		sig      string
		selector string
	}{
		{"getBalance(address,address)", "0xd4fac45d"},
		{"getLockedBalance(address,address)", "0xc736887e"},
		{"getAccountSummary(address,address)", "0x65d53f6c"},
		{"getMarginMode(address)", "0xb71d031c"},
	} {
		require.Contains(t, golden, "method "+overload.sig+" "+overload.selector)
		require.Equal(t, overload.selector, "0x"+hex.EncodeToString(methodBySig(t, parsed, overload.sig).ID), overload.sig)
	}
}

func TestABITupleOrder(t *testing.T) {
	parsed := loadPerpsABI(t)
	cases := []struct {
		sig    string
		name   string
		fields []string
	}{
		{"quoteOpenPosition(uint256,address,uint256,uint256,bool)", "IPerpsOpenPositionQuote", []string{
			"entryPrice uint256",
			"sizeUsd uint256",
			"collateralUsd uint256",
			"leverage uint256",
			"tradingFee uint256",
			"tradingFeeUsd uint256",
			"priceImpact uint256",
			"liquidationPrice uint256",
			"estimatedFunding24h int256",
			"maintenanceMarginBps uint256",
		}},
		{"quoteClosePosition(uint256)", "IPerpsClosePositionQuote", []string{
			"exitPrice uint256",
			"unrealizedPnl int256",
			"tradingFee uint256",
			"tradingFeeUsd uint256",
			"fundingOwed int256",
			"netPnl int256",
			"estimatedPayout uint256",
		}},
		{"quoteMarket(uint256)", "IPerpsMarketQuote", []string{
			"indexPrice uint256",
			"markPrice uint256",
			"oracleTWAP uint256",
			"fundingRatePerSecond int256",
			"fundingRate24h int256",
			"longOI uint256",
			"shortOI uint256",
			"maxLeverage uint256",
			"maintenanceMarginBps uint256",
			"enabled bool",
			"priceStale bool",
		}},
		{"getAccountSummary(address)", "IPerpsAccountSummary", []string{
			"idle uint256",
			"totalLocked uint256",
			"positionCount uint256",
		}},
		{"getAccountSummary(address,address)", "IPerpsAccountSummary", []string{
			"idle uint256",
			"totalLocked uint256",
			"positionCount uint256",
		}},
		{"getPositionMargin(uint256)", "IPerpsPositionMargin", []string{
			"token address",
			"lockedAmount uint256",
			"marketId uint256",
			"isLong bool",
		}},
	}
	for _, tc := range cases {
		m := methodBySig(t, parsed, tc.sig)
		require.Len(t, m.Outputs, 1, tc.sig)
		out := m.Outputs[0].Type
		require.Equal(t, abi.TupleTy, out.T, tc.sig)
		require.Equal(t, tc.name, out.TupleRawName, tc.sig)
		require.Equal(t, tc.fields, tupleFields(out), tc.sig)
	}
}

func TestABINoBorrowing(t *testing.T) {
	parsed := loadPerpsABI(t)
	var names []string
	for _, m := range parsed.Methods {
		names = append(names, m.RawName)
		for _, arg := range append(append(abi.Arguments{}, m.Inputs...), m.Outputs...) {
			names = append(names, arg.Name)
			collectArgNames(arg.Type, &names)
		}
	}
	for _, e := range parsed.Events {
		names = append(names, e.RawName)
		for _, arg := range e.Inputs {
			names = append(names, arg.Name)
			collectArgNames(arg.Type, &names)
		}
	}
	for _, e := range parsed.Errors {
		names = append(names, e.Name)
	}
	for _, name := range names {
		require.NotContains(t, strings.ToLower(name), "borrowing", name)
	}

	// Diamond borrowing views getBorrowingFeeRate() and getPendingBorrowingFee(uint256).
	diamond := map[string]string{
		"getBorrowingFeeRate()":           "0x27646b81",
		"getPendingBorrowingFee(uint256)": "0x687185aa",
	}
	for _, line := range loadGolden(t) {
		require.NotContains(t, strings.ToLower(line), "borrowing", line)
		for sig, selector := range diamond {
			require.NotContains(t, line, selector, sig)
		}
	}
	for sig, selector := range diamond {
		require.Equal(t, selector, "0x"+hex.EncodeToString(crypto.Keccak256([]byte(sig))[:4]), sig)
		for _, m := range parsed.Methods {
			require.NotEqual(t, selector, "0x"+hex.EncodeToString(m.ID), m.Sig)
		}
	}
}
