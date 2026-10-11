package types_test

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/xweb/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const observationKernelDriver = `
#include "layerx/lx_web.h"
#include <stdio.h>
#include <string.h>

static uint8_t input[LXP_MAX_ACTIVITY_BYTES];

int main(int argc, char **argv)
{
    lx_web_observation observation;
    lxp_activity activity;
    uint8_t canonical[LX_WEB_OBSERVATION_MAX_BYTES];
    uint8_t code[4];
    const uint8_t *payload = input;
    size_t length = fread(input, 1U, sizeof(input), stdin);
    size_t canonical_length = 0U;
    lxp_result status;
    uint32_t result;
    if (argc != 2 || ferror(stdin) != 0 || fgetc(stdin) != EOF) return 1;
    if (strcmp(argv[1], "activity") == 0) {
        status = lxp_activity_decode(input, length, &activity);
        if (status != LXP_OK || activity.activity_type != LX_WEB_OBSERVATION_ACTIVITY)
            return 2;
        payload = activity.payload.bytes;
        length = activity.payload.length;
    } else if (strcmp(argv[1], "observation") != 0) {
        return 3;
    }
    status = lx_web_observation_decode(payload, length, &observation);
    if (status == LXP_OK)
        status = lx_web_observation_encode(&observation, canonical,
                                           sizeof(canonical), &canonical_length);
    result = (uint32_t)status;
    code[0] = (uint8_t)(result >> 24U);
    code[1] = (uint8_t)(result >> 16U);
    code[2] = (uint8_t)(result >> 8U);
    code[3] = (uint8_t)result;
    if (fwrite(code, 1U, sizeof(code), stdout) != sizeof(code)) return 4;
    if (status == LXP_OK &&
        fwrite(canonical, 1U, canonical_length, stdout) != canonical_length)
        return 5;
    return fflush(stdout) == 0 ? 0 : 6;
}
`

var (
	observationKernelOnce   sync.Once
	observationKernelDir    string
	observationKernelBinary string
	observationKernelError  error
)

func TestMain(m *testing.M) {
	code := m.Run()
	if observationKernelDir != "" {
		if err := os.RemoveAll(observationKernelDir); err != nil {
			fmt.Fprintln(os.Stderr, err)
			code = 1
		}
	}
	os.Exit(code)
}

func observationRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs("../../..")
	require.NoError(t, err)
	return root
}

func observationKernel(t *testing.T, mode string, raw []byte) (int32, []byte) {
	t.Helper()
	root := observationRoot(t)
	observationKernelOnce.Do(func() {
		observationKernelDir, observationKernelError = os.MkdirTemp("", "xweb-observation-")
		if observationKernelError != nil {
			return
		}
		source := filepath.Join(observationKernelDir, "observation.c")
		observationKernelError = os.WriteFile(source, []byte(observationKernelDriver), 0600)
		if observationKernelError != nil {
			return
		}
		observationKernelBinary = filepath.Join(observationKernelDir, "observation")
		args := []string{
			"-std=c17", "-pedantic", "-Werror", "-Wall", "-Wextra", "-Wconversion", "-Wshadow", "-Wvla",
			"-fno-strict-aliasing", "-ffp-contract=off", "-ffunction-sections", "-fdata-sections",
			"-Iinclude", source,
			"src/modules/web/lx_web_intake.c", "src/network/lx_web_adapter.c", "src/crypto/lxp_ct.c",
			"src/protocol/lxp_activity.c", "src/codec/lxp_codec.c", "src/protocol/lxp_u128.c",
			"src/protocol/lxp_protocol.c", "-Wl,--gc-sections", "-o", observationKernelBinary,
		}
		command := exec.Command("cc", args...)
		command.Dir = root
		output, err := command.CombinedOutput()
		if err != nil {
			observationKernelError = fmt.Errorf("compile kernel observation codec: %w\n%s", err, output)
		}
	})
	require.NoError(t, observationKernelError)
	command := exec.Command(observationKernelBinary, mode)
	command.Dir = root
	command.Stdin = bytes.NewReader(raw)
	var stderr bytes.Buffer
	command.Stderr = &stderr
	output, err := command.Output()
	require.NoError(t, err, "%s", stderr.String())
	require.GreaterOrEqual(t, len(output), 4)
	return int32(binary.BigEndian.Uint32(output[:4])), output[4:]
}

func observationFixture(t *testing.T) []byte {
	t.Helper()
	encoded, err := os.ReadFile(filepath.Join(observationRoot(t), "tests/fixtures/web/observation-activity.hex"))
	require.NoError(t, err)
	activity, err := hex.DecodeString(strings.TrimSpace(string(encoded)))
	require.NoError(t, err)
	status, raw := observationKernel(t, "activity", activity)
	require.Equal(t, int32(0), status)
	require.Len(t, raw, 358)
	return raw
}

func observationHex(t *testing.T, raw string) []byte {
	t.Helper()
	decoded, err := hex.DecodeString(raw)
	require.NoError(t, err)
	return decoded
}

func TestDecodeObservationFixture(t *testing.T) {
	raw := observationFixture(t)
	t.Run("5.12.1", func(t *testing.T) {
		observation, err := types.DecodeObservation(raw)
		require.NoError(t, err)
		require.Equal(t, uint8(2), observation.Origin)
		assert.Equal(t, uint32(1), observation.NetworkID)
		var program types.Hash32
		for i := range program {
			program[i] = byte(0xa0 + i)
		}
		require.Equal(t, program, observation.ProgramID)
		require.Equal(t, uint64(0x0102030405060708), observation.RequestID)
		require.Equal(t, uint8(1), observation.Kind)
		require.Equal(t, observationHex(t, "62da65e513a2dc07e8c56bb6e148a96d6d259e496ec215d58c89522fa9649ce1"), observation.PayloadHash[:])
		require.Equal(t, observationHex(t, "2d823e82313101707a7081be2efb28edce966e3e6d1eaec9f7c1e48088f90781"), observation.ContentDigest[:])
		require.Equal(t, uint32(16), observation.FullLength)
		require.Len(t, observation.Response, 16)
		require.Equal(t, []byte("Paxeer X Network"), observation.Response)
		require.Equal(t, [][]byte{
			observationHex(t, "31947ab53187686fc5c3c21f0f78dcddc2a7ba4ae8fd7c47912f863614be8caf4bfc9541339a823e772aa657d00017a5ed0c36cc068b58e1020023cb2cab7fab1c"),
			observationHex(t, "207e4af3fb807260e2c4307b23313c31aae37b91ae2234f16d8523f4b575b3f24cdf153425e5ef708d4d7b3e8d4f270b0c5a9d7f43da00a3749a477a62cc145d1b"),
			observationHex(t, "2bd8755672a66bc20b1c68ac3ad4ea8f5eee7ca9fc41952de958527dce9bccec5150b88c5afcc010811a94e083fd8b0b38cfc9a6dba64f0a353aa3751b07a7cf1b"),
		}, observation.Signatures)
		canonical, err := types.EncodeObservation(observation)
		require.NoError(t, err)
		require.Equal(t, raw, canonical)
		status, kernel := observationKernel(t, "observation", raw)
		require.Equal(t, int32(0), status)
		require.Equal(t, canonical, kernel)
	})
	t.Run("owned_bytes", func(t *testing.T) {
		input := bytes.Clone(raw)
		observation, err := types.DecodeObservation(input)
		require.NoError(t, err)
		clear(input)
		canonical, err := types.EncodeObservation(observation)
		require.NoError(t, err)
		require.Equal(t, raw, canonical)
		observation.Response[0] ^= 1
		observation.Signatures[0][0] ^= 1
		require.Equal(t, raw, canonical)
	})
	t.Run("kernel_bounds", func(t *testing.T) {
		require.Equal(t, 146, types.ObservationHeaderBytes)
		require.Equal(t, 16, types.ObservationMaxSignatures)
		require.Equal(t, 5283, types.ObservationMaxBytes)
		fixture, err := types.DecodeObservation(raw)
		require.NoError(t, err)
		cases := []struct {
			name   string
			change func(*types.Observation)
		}{
			{"empty_response", func(o *types.Observation) {
				o.Response = []byte{}
				o.FullLength = 0
				o.Signatures = o.Signatures[:1]
			}},
			{"search", func(o *types.Observation) { o.Kind = types.KindSearch }},
			{"maximum", func(o *types.Observation) {
				o.NetworkID = ^uint32(0)
				o.RequestID = ^uint64(0)
				o.Response = bytes.Repeat([]byte{0xab}, 4096)
				o.FullLength = ^uint32(0)
				o.Signatures = make([][]byte, 16)
				for i := range o.Signatures {
					o.Signatures[i] = bytes.Clone(fixture.Signatures[i%3])
				}
			}},
			{"zero_request_and_hashes", func(o *types.Observation) {
				o.RequestID = 0
				o.PayloadHash = types.Hash32{}
				o.ContentDigest = types.Hash32{}
			}},
			{"opaque_signature", func(o *types.Observation) { o.Signatures = [][]byte{make([]byte, 65)} }},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				want := fixture
				tc.change(&want)
				encoded, err := types.EncodeObservation(want)
				require.NoError(t, err)
				status, kernel := observationKernel(t, "observation", encoded)
				require.Equal(t, int32(0), status)
				require.Equal(t, encoded, kernel)
				got, err := types.DecodeObservation(encoded)
				require.NoError(t, err)
				require.Equal(t, want, got)
				if tc.name == "maximum" {
					require.Len(t, encoded, 5283)
				}
				if tc.name == "empty_response" {
					require.Len(t, encoded, 212)
				}
			})
		}
	})
}

func observationRefusal(t *testing.T, raw []byte) {
	t.Helper()
	status, canonical := observationKernel(t, "observation", raw)
	require.Equal(t, int32(-3), status)
	require.Empty(t, canonical)
	observation, err := types.DecodeObservation(raw)
	require.EqualError(t, err, "LXP_NON_CANONICAL")
	require.Equal(t, types.Observation{}, observation)
}

func TestDecodeObservationRefusals(t *testing.T) {
	fixture := observationFixture(t)
	t.Run("5.12.2", func(t *testing.T) {
		cases := []struct {
			name   string
			change func([]byte) []byte
		}{
			{"origin", func(raw []byte) []byte { raw[0] = 1; return raw }},
			{"signature_count_above_cap", func(raw []byte) []byte { raw[162] = 17; return raw }},
			{"signature_count_zero", func(raw []byte) []byte { raw[162] = 0; return raw }},
			{"response_above_cap", func(raw []byte) []byte { binary.BigEndian.PutUint32(raw[142:146], 4097); return raw }},
			{"response_length_overflow", func(raw []byte) []byte { binary.BigEndian.PutUint32(raw[142:146], ^uint32(0)); return raw }},
			{"full_length_short", func(raw []byte) []byte { binary.BigEndian.PutUint32(raw[138:142], 15); return raw }},
			{"response_truncated", func(raw []byte) []byte { return raw[:161] }},
			{"response_count_missing", func(raw []byte) []byte { return raw[:162] }},
			{"signature_truncated", func(raw []byte) []byte { return raw[:len(raw)-1] }},
			{"trailing_byte", func(raw []byte) []byte { return append(raw, 0) }},
			{"extra_signature", func(raw []byte) []byte { return append(raw, raw[163:228]...) }},
			{"network_zero", func(raw []byte) []byte { clear(raw[29:33]); return raw }},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) { observationRefusal(t, tc.change(bytes.Clone(fixture))) })
		}
		for i := 1; i <= 28; i++ {
			t.Run(fmt.Sprintf("padding_%d", i), func(t *testing.T) {
				raw := bytes.Clone(fixture)
				raw[i] = 1
				observationRefusal(t, raw)
			})
		}
		for length := 0; length <= 146; length++ {
			t.Run(fmt.Sprintf("short_%d", length), func(t *testing.T) { observationRefusal(t, fixture[:length]) })
		}
	})
	t.Run("5.12.3", func(t *testing.T) {
		t.Run("program_zero", func(t *testing.T) {
			raw := bytes.Clone(fixture)
			clear(raw[33:65])
			observationRefusal(t, raw)
		})
		for _, kind := range []byte{0, 3, 255} {
			t.Run(fmt.Sprintf("kind_%d", kind), func(t *testing.T) {
				raw := bytes.Clone(fixture)
				raw[73] = kind
				observationRefusal(t, raw)
			})
		}
	})
	t.Run("5.12.4", func(t *testing.T) {
		raw := make([]byte, 5284)
		copy(raw, fixture)
		observationRefusal(t, raw)
	})
	t.Run("encoder_signature_width", func(t *testing.T) {
		observation, err := types.DecodeObservation(fixture)
		require.NoError(t, err)
		for _, width := range []int{0, 64, 66} {
			t.Run(fmt.Sprintf("width_%d", width), func(t *testing.T) {
				invalid := observation
				invalid.Signatures = [][]byte{make([]byte, width)}
				raw, err := types.EncodeObservation(invalid)
				require.EqualError(t, err, "LXP_NON_CANONICAL")
				require.Nil(t, raw)
			})
		}
	})
}
