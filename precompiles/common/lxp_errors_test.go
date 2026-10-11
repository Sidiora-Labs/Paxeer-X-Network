package common

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	sdkerrors "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types/errors"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/stretchr/testify/require"
)

type lxpRegistryRowForTest struct {
	code    string
	message string
	origin  string
}

type lxpKernelResultForTest struct {
	name  string
	value int32
}

var (
	lxpKernelOriginForTest   = regexp.MustCompile(`^LXP_(?:ERR|FATAL)_[A-Z_]+ = (-\d+), include/layerx/lxp_result\.h:\d+`)
	lxpModuleOriginForTest   = regexp.MustCompile(`^Err\w+ \(code (\d+)\), modules/(\w+)/types/errors\.go:\d+`)
	lxpRootOriginForTest     = regexp.MustCompile(`^Err\w+ \(root codespace "sdk", code (\d+)\)`)
	lxpBindingOriginForTest  = regexp.MustCompile(`^ErrLayerX\w+, modules/evm/types/layerx_binding\.go:\d+`)
	lxpHeaderResultForTest   = regexp.MustCompile(`X\((LXP_[A-Z_]+), (-?\d+)\)`)
	lxpRustResultForTest     = regexp.MustCompile(`^\s*(\w+) = (-?\d+), (?:Terminal|Retriable);$`)
	lxpRevertSelectorForTest = []byte{0x08, 0xc3, 0x79, 0xa0}
)

func lxpReadRepoFileForTest(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", filepath.FromSlash(path)))
	require.NoError(t, err)
	return string(data)
}

func lxpRegistryRowsForTest(t *testing.T) []lxpRegistryRowForTest {
	t.Helper()
	doc := lxpReadRepoFileForTest(t, "spec/layerx-merge/contracts/address-table.md")
	start := strings.Index(doc, "| Code | Message | Emitting construct | Kernel or Go origin (file:line) | Retryable |")
	end := strings.Index(doc, "## (c) Activation")
	require.True(t, start >= 0 && end > start, "section (b) code table not found")
	var rows []lxpRegistryRowForTest
	for _, line := range strings.Split(doc[start:end], "\n") {
		if !strings.HasPrefix(line, "| LXP_") {
			continue
		}
		cells := strings.Split(strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(line), "| "), " |"), " | ")
		require.Len(t, cells, 5, line)
		rows = append(rows, lxpRegistryRowForTest{code: cells[0], message: cells[1], origin: cells[3]})
	}
	return rows
}

func lxpHeaderResultsForTest(t *testing.T) []lxpKernelResultForTest {
	t.Helper()
	header := lxpReadRepoFileForTest(t, "include/layerx/lxp_result.h")
	start := strings.Index(header, "#define LXP_RESULT_CODE_LIST(X)")
	end := strings.Index(header, "#define LXP_DECLARE_RESULT")
	require.True(t, start >= 0 && end > start, "LXP_RESULT_CODE_LIST not found")
	var results []lxpKernelResultForTest
	for _, match := range lxpHeaderResultForTest.FindAllStringSubmatch(header[start:end], -1) {
		value, err := strconv.ParseInt(match[2], 10, 32)
		require.NoError(t, err)
		results = append(results, lxpKernelResultForTest{name: match[1], value: int32(value)})
	}
	return results
}

func lxpRustResultsForTest(t *testing.T) []lxpKernelResultForTest {
	t.Helper()
	source := lxpReadRepoFileForTest(t, "agent/crates/layerx-types/src/result.rs")
	start := strings.Index(source, "protocol_result_codes! {")
	require.True(t, start >= 0, "protocol_result_codes! invocation not found")
	end := strings.Index(source[start:], "\n}")
	require.True(t, end > 0)
	var results []lxpKernelResultForTest
	for _, line := range strings.Split(source[start:start+end], "\n") {
		match := lxpRustResultForTest.FindStringSubmatch(line)
		if match == nil {
			continue
		}
		value, err := strconv.ParseInt(match[2], 10, 32)
		require.NoError(t, err)
		results = append(results, lxpKernelResultForTest{name: match[1], value: int32(value)})
	}
	return results
}

// lxpCamelForTest turns a header enum name into the Rust mirror's variant name.
func lxpCamelForTest(name string) string {
	name = strings.TrimPrefix(strings.TrimPrefix(name, "LXP_ERR_"), "LXP_")
	var out strings.Builder
	for _, word := range strings.Split(name, "_") {
		out.WriteString(word[:1])
		out.WriteString(strings.ToLower(word[1:]))
	}
	return out.String()
}

func lxpDecodeRevertForTest(t *testing.T, data []byte) string {
	t.Helper()
	require.GreaterOrEqual(t, len(data), 4)
	require.Equal(t, lxpRevertSelectorForTest, data[:4])
	stringType, err := abi.NewType("string", "", nil)
	require.NoError(t, err)
	values, err := abi.Arguments{{Type: stringType}}.Unpack(data[4:])
	require.NoError(t, err)
	require.Len(t, values, 1)
	text, ok := values[0].(string)
	require.True(t, ok)
	return text
}

func TestLXPRevertTable(t *testing.T) {
	rows := lxpRegistryRowsForTest(t)
	require.Len(t, lxpRevertTable, len(rows), "one table entry per registry row")

	codes := map[string]bool{}
	goErrs := map[error]string{}
	kernelEntries := map[int32]string{}
	for i, row := range rows {
		entry := lxpRevertTable[i]
		require.Equal(t, row.code, entry.code, "row %d", i)
		require.Equal(t, row.message, entry.message, row.code)
		require.False(t, codes[entry.code], "duplicate code %s", entry.code)
		codes[entry.code] = true

		if match := lxpKernelOriginForTest.FindStringSubmatch(row.origin); match != nil {
			value, err := strconv.ParseInt(match[1], 10, 32)
			require.NoError(t, err)
			require.True(t, entry.kernel, row.code)
			require.Equal(t, int32(value), entry.value, row.code)
			require.Nil(t, entry.goErr, row.code)
			_, dup := kernelEntries[entry.value]
			require.False(t, dup, "duplicate kernel value %d", entry.value)
			kernelEntries[entry.value] = entry.code
		} else {
			require.False(t, entry.kernel, row.code)
		}

		switch {
		case lxpModuleOriginForTest.MatchString(row.origin):
			match := lxpModuleOriginForTest.FindStringSubmatch(row.origin)
			registered, ok := entry.goErr.(*sdkerrors.Error)
			require.True(t, ok, row.code)
			require.Equal(t, match[1], strconv.FormatUint(uint64(registered.ABCICode()), 10), row.code)
			require.Equal(t, match[2], registered.Codespace(), row.code)
		case lxpRootOriginForTest.MatchString(row.origin):
			match := lxpRootOriginForTest.FindStringSubmatch(row.origin)
			registered, ok := entry.goErr.(*sdkerrors.Error)
			require.True(t, ok, row.code)
			require.Equal(t, match[1], strconv.FormatUint(uint64(registered.ABCICode()), 10), row.code)
			require.Equal(t, sdkerrors.RootCodespace, registered.Codespace(), row.code)
		case lxpBindingOriginForTest.MatchString(row.origin):
			require.NotNil(t, entry.goErr, row.code)
			require.Equal(t, row.message, entry.goErr.Error(), row.code)
		default:
			require.Nil(t, entry.goErr, row.code)
		}
		if entry.goErr != nil {
			previous, dup := goErrs[entry.goErr]
			require.False(t, dup, "%s and %s share one registered error", previous, entry.code)
			goErrs[entry.goErr] = entry.code
		}

		require.Equal(t, row.code+": "+row.message, lxpDecodeRevertForTest(t, LXPRevert(entry.code, "")))
	}

	header := lxpHeaderResultsForTest(t)
	headerValues := map[int32]bool{}
	for _, result := range header {
		require.False(t, headerValues[result.value], "duplicate header value %d", result.value)
		headerValues[result.value] = true
		switch {
		case result.value == 0:
			require.Equal(t, "LXP_OK", result.name)
			require.Equal(t, "LXP_OK", LXPCodeName(result.value))
			_, listed := kernelEntries[0]
			require.False(t, listed, "success is not a refusal")
		default:
			want := "LXP_" + strings.TrimPrefix(strings.TrimPrefix(result.name, "LXP_ERR_"), "LXP_")
			require.Equal(t, want, kernelEntries[result.value], "header %s", result.name)
			if result.value <= -1000 {
				require.Equal(t, "", LXPCodeName(result.value), result.name)
			} else {
				require.Equal(t, want, LXPCodeName(result.value), result.name)
			}
		}
	}
	for value, code := range kernelEntries {
		require.True(t, headerValues[value], "%s keyed by %d, which the header does not declare", code, value)
	}

	rust := lxpRustResultsForTest(t)
	require.Len(t, rust, len(header))
	for i, result := range header {
		require.Equal(t, result.value, rust[i].value, result.name)
		require.Equal(t, lxpCamelForTest(result.name), rust[i].name, result.name)
	}

	for _, value := range []int32{1, -9, -99, -737, -999} {
		require.Equal(t, "LXP_UNKNOWN", LXPCodeName(value), "value %d", value)
	}
	for _, value := range []int32{-1000, -1004, -2147483648} {
		require.Equal(t, "", LXPCodeName(value), "value %d", value)
	}

	require.Equal(t, "LXP_TRUNCATED: truncated; argument 2",
		lxpDecodeRevertForTest(t, LXPRevert(LXPCodeName(-1), "argument 2")))
	require.Equal(t, "LXP_XWEB_UNKNOWN_REQUEST: request not found; request 7",
		lxpDecodeRevertForTest(t, LXPRevert("LXP_XWEB_UNKNOWN_REQUEST", "request 7")))
	require.Equal(t, "LXP_UNKNOWN: unknown; LXP_NOT_A_CODE",
		lxpDecodeRevertForTest(t, LXPRevert("LXP_NOT_A_CODE", "")))
	require.Equal(t, "LXP_UNKNOWN: unknown; LXP_NOT_A_CODE; argument 1",
		lxpDecodeRevertForTest(t, LXPRevert("LXP_NOT_A_CODE", "argument 1")))
	require.Equal(t, "LXP_UNKNOWN: unknown",
		lxpDecodeRevertForTest(t, LXPRevert(LXPCodeName(-12345), "")))
}
