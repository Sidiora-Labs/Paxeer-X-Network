package pricefeed

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"

	pfabi "github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/pricefeed/abi"
)

func argumentTypes(arguments abi.Arguments) string {
	types := make([]string, len(arguments))
	for i, argument := range arguments {
		types[i] = argument.Type.String()
	}
	return "(" + strings.Join(types, ",") + ")"
}

func TestPriceFeedABIGolden(t *testing.T) {
	file, err := os.ReadFile("abi.json")
	require.NoError(t, err)
	require.Equal(t, string(file), pfabi.JSON)

	parsed, err := abi.JSON(strings.NewReader(pfabi.JSON))
	require.NoError(t, err)

	const (
		price       = "(uint256,uint256,uint64,int64,uint32,bool)"
		market      = "(uint64,string,string,uint256,uint256,bool)"
		observation = "(uint64,uint256,uint256,uint64,int64)"
		params      = "(uint64,uint64,uint32,uint32,uint32,uint32,uint32,uint64,uint32,uint64)"
	)
	methods := []struct {
		name, sig, selector, mutability, outputs string
	}{
		{"registerFeedKey", "registerFeedKey(bytes)", "5d6617d9", "nonpayable", "(address)"},
		{"revokeFeedKey", "revokeFeedKey()", "50efc392", "nonpayable", "()"},
		{"getPrice", "getPrice(uint256)", "e7572230", "view", "(" + price + ")"},
		{"getPriceBySymbol", "getPriceBySymbol(string)", "f86f3593", "view", "(" + price + ")"},
		{"getPrices", "getPrices(uint256[])", "09cb3a4e", "view", "(" + price + "[])"},
		{"getMarket", "getMarket(uint256)", "eb44fdd3", "view", "(" + market + ")"},
		{"getMarketCount", "getMarketCount()", "fd69f3c2", "view", "(uint256)"},
		{"marketIdOf", "marketIdOf(string)", "19fa583c", "view", "(uint256)"},
		{"getObservation", "getObservation(uint256,address)", "48f83666", "view", "(" + observation + ")"},
		{"getFeedKey", "getFeedKey(address)", "2d6f477c", "view", "(bytes,address)"},
		{"getValidatorByFeedAccount", "getValidatorByFeedAccount(address)", "007eb855", "view", "(address)"},
		{"getParams", "getParams()", "5e615a6b", "view", "(" + params + ")"},
	}
	require.Len(t, parsed.Methods, len(methods))
	for _, want := range methods {
		t.Run(want.name, func(t *testing.T) {
			got, ok := parsed.Methods[want.name]
			require.True(t, ok)
			require.Equal(t, want.sig, got.Sig)
			require.Equal(t, common.FromHex(want.selector), got.ID)
			require.Equal(t, want.mutability, got.StateMutability)
			require.Equal(t, want.outputs, argumentTypes(got.Outputs))
			resolved, err := parsed.MethodById(got.ID)
			require.NoError(t, err)
			require.Equal(t, want.name, resolved.Name)
		})
	}
	require.Equal(t, []string{"pubKey", "feedAccount"},
		[]string{parsed.Methods["getFeedKey"].Outputs[0].Name, parsed.Methods["getFeedKey"].Outputs[1].Name})
	require.Equal(t, []string{"indexPrice", "markPrice", "timestampMs", "height", "sources", "fresh"},
		parsed.Methods["getPrice"].Outputs[0].Type.TupleRawNames)
	require.Equal(t, []string{"marketId", "symbol", "chainflowSymbol", "minPrice", "maxPrice", "enabled"},
		parsed.Methods["getMarket"].Outputs[0].Type.TupleRawNames)
	require.Equal(t, []string{"sequence", "indexPrice", "markPrice", "sourceTimestampMs", "height"},
		parsed.Methods["getObservation"].Outputs[0].Type.TupleRawNames)
	require.Equal(t, []string{"stalenessWindowMs", "maxClockSkewMs", "minSources", "maxDivergenceBps", "maxDeviationBps",
		"maxObservationsPerSubmission", "maxSubmissionBytes", "markEmaTauMs", "maxMarkBasisBps", "maxPriceAgeMs"},
		parsed.Methods["getParams"].Outputs[0].Type.TupleRawNames)

	events := []struct {
		name, sig, topic string
		indexed          []bool
		inputs           []string
	}{
		{"FeedKeyRegistered", "FeedKeyRegistered(address,address,bytes,address)",
			"397ad0e3fa026f61c210e8e110f9afe6523d620f132e3155d33748bfcea99176",
			[]bool{true, true, false, false}, []string{"validator", "feedAccount", "pubKey", "previousFeedAccount"}},
		{"FeedKeyRevoked", "FeedKeyRevoked(address,address)",
			"5aaf7fdb739922e45542b2ff7dfa2a5a2e1a9537a6f30b7bbf2b280b0dfb54a1",
			[]bool{true, true}, []string{"validator", "feedAccount"}},
	}
	require.Len(t, parsed.Events, len(events))
	for _, want := range events {
		t.Run(want.name, func(t *testing.T) {
			got, ok := parsed.Events[want.name]
			require.True(t, ok)
			require.Equal(t, want.sig, got.Sig)
			require.Equal(t, common.HexToHash(want.topic), got.ID)
			require.False(t, got.Anonymous)
			indexed := make([]bool, len(got.Inputs))
			names := make([]string, len(got.Inputs))
			for i, input := range got.Inputs {
				indexed[i] = input.Indexed
				names[i] = input.Name
			}
			require.Equal(t, want.indexed, indexed)
			require.Equal(t, want.inputs, names)
			resolved, err := parsed.EventByID(got.ID)
			require.NoError(t, err)
			require.Equal(t, want.name, resolved.Name)
		})
	}

	t.Run("acceptance_5_4_7", func(t *testing.T) {
		for name, method := range parsed.Methods {
			require.NotContains(t, strings.ToLower(name), "submit", name)
			require.False(t, method.IsPayable(), name)
			if name != "registerFeedKey" && name != "revokeFeedKey" {
				require.Equal(t, "view", method.StateMutability, name)
			}
		}
	})

	t.Run("acceptance_5_8_6", func(t *testing.T) {
		source, err := os.ReadFile("PriceFeed.sol")
		require.NoError(t, err)
		code := regexp.MustCompile(`(?s)/\*.*?\*/|//[^\n]*`).ReplaceAllString(string(source), "")
		require.Contains(t, code,
			"address constant PRICEFEED_PRECOMPILE_ADDRESS = 0x0000000000000000000000000000000000001020;")
		require.Contains(t, code, "IPriceFeed constant PRICEFEED_CONTRACT = IPriceFeed(PRICEFEED_PRECOMPILE_ADDRESS);")
		var functions []string
		for _, match := range regexp.MustCompile(`\bfunction\s+(\w+)\s*\(`).FindAllStringSubmatch(code, -1) {
			functions = append(functions, match[1])
		}
		wantFunctions := make([]string, len(methods))
		for i, method := range methods {
			wantFunctions[i] = method.name
		}
		require.Equal(t, wantFunctions, functions)
		var declared []string
		for _, match := range regexp.MustCompile(`\bevent\s+(\w+)\s*\(`).FindAllStringSubmatch(code, -1) {
			declared = append(declared, match[1])
		}
		require.Equal(t, []string{"FeedKeyRegistered", "FeedKeyRevoked"}, declared)
		var structs []string
		for _, match := range regexp.MustCompile(`\bstruct\s+(\w+)\s*\{`).FindAllStringSubmatch(code, -1) {
			structs = append(structs, match[1])
		}
		require.Equal(t, []string{"Price", "Market", "Observation", "Params"}, structs)
	})
}
