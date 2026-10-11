package types

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Sidiora-Labs/Paxeer-X-Network/layerxproof/codec"
)

const streamDecoderSource = `
#include "layerx/lx_stream.h"
#include <inttypes.h>
#include <stdio.h>
#include <string.h>

static void hex(const uint8_t *bytes, size_t length)
{
    size_t i;
    putchar(' ');
    for (i = 0U; i < length; ++i) printf("%02x", bytes[i]);
}

static void u128(lxp_u128 value)
{
    printf(" %016" PRIx64 "%016" PRIx64, value.hi, value.lo);
}

static void u64(uint64_t value)
{
    printf(" %016" PRIx64, value);
}

int main(int argc, char **argv)
{
    static uint8_t payload[4096];
    lx_stream_open_payload open;
    lx_stream_amount_payload amount;
    lx_stream_meter_attestation meter;
    lx_stream_keyed_payload keyed;
    lx_stream_id_payload id;
    lx_stream_rate_change_payload rate;
    lxp_result result;
    size_t length;
    size_t i;
    const char *kind;
    if (argc != 2) return 2;
    kind = argv[1];
    if (strcmp(kind, "lengths") == 0) {
        printf("%d %d %d %d %d %d %d %d %d %d %d %d\n",
               LX_STREAM_PAYLOAD_VERSION,
               LX_STREAM_MAX_METER_AUTHORITIES,
               LX_STREAM_OPEN_PAYLOAD_FIXED,
               LX_STREAM_OPEN_PAYLOAD_MAX,
               LX_STREAM_TOP_UP_PAYLOAD_BYTES,
               LX_STREAM_METER_PAYLOAD_BYTES,
               LX_STREAM_KEYED_PAYLOAD_BYTES,
               LX_STREAM_RATE_CHANGE_PAYLOAD_BYTES,
               LX_STREAM_ID_PAYLOAD_BYTES,
               LX_STREAM_MODE_TIME,
               LX_STREAM_MODE_METERED,
               LX_STREAM_RATE_CHANGE);
        return 0;
    }
    length = fread(payload, 1, sizeof(payload), stdin);
    if (ferror(stdin) || fgetc(stdin) != EOF) return 2;
    if (strcmp(kind, "open") == 0) {
        result = lx_stream_open_decode(payload, length, &open);
    } else if (strcmp(kind, "top_up") == 0) {
        result = lx_stream_amount_decode(payload, length, &amount);
    } else if (strcmp(kind, "meter") == 0) {
        result = lx_stream_meter_decode(payload, length, &meter);
    } else if (strcmp(kind, "keyed") == 0) {
        result = lx_stream_keyed_decode(payload, length, &keyed);
    } else if (strcmp(kind, "id") == 0) {
        result = lx_stream_id_decode(payload, length, &id);
    } else if (strcmp(kind, "rate_change") == 0) {
        result = lx_stream_rate_change_decode(payload, length, &rate);
    } else {
        return 2;
    }
    printf("%" PRId32 " %s", result, lxp_result_name(result));
    if (result == LXP_OK) {
        if (strcmp(kind, "open") == 0) {
            hex(open.record.stream_id, 32U);
            hex(open.record.payer, 32U);
            hex(open.record.stream_account, 32U);
            hex(open.record.recipient, 32U);
            hex(open.record.asset_id, 32U);
            printf(" %d", (int)open.record.mode);
            u128(open.record.rate);
            u64(open.record.rate_unit);
            u64(open.record.start_timestamp);
            u64(open.record.last_accrual_timestamp);
            u64(open.record.end_timestamp);
            u128(open.record.total_cap);
            u128(open.initial_funding);
            printf(" %zu", open.record.meter_authority_count);
            for (i = 0U; i < open.record.meter_authority_count; ++i)
                hex(open.record.meter_authorities[i], 32U);
        } else if (strcmp(kind, "top_up") == 0) {
            hex(amount.stream_id, 32U);
            u128(amount.amount);
        } else if (strcmp(kind, "meter") == 0) {
            hex(meter.stream_id, 32U);
            u64(meter.cumulative_reading);
            hex(meter.authority_key, 32U);
            hex(meter.signature, 64U);
        } else if (strcmp(kind, "keyed") == 0) {
            hex(keyed.stream_id, 32U);
            hex(keyed.idempotency_key, 32U);
        } else if (strcmp(kind, "id") == 0) {
            hex(id.stream_id, 32U);
        } else {
            hex(rate.stream_id, 32U);
            u128(rate.new_rate);
            hex(rate.idempotency_key, 32U);
        }
    }
    putchar('\n');
    return 0;
}
`

const (
	streamDecodedNonCanonical = "-3 LXP_ERR_NON_CANONICAL"
	streamDecodedVersion      = "-101 LXP_ERR_VERSION_UNSUPPORTED"
)

func streamDecoder(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	source := filepath.Join(dir, "stream_decoder.c")
	if err := os.WriteFile(source, []byte(streamDecoderSource), 0600); err != nil {
		t.Fatal(err)
	}
	binaryPath := filepath.Join(dir, "stream_decoder")
	args := []string{"-std=c11", "-Wall", "-Wextra", "-Werror", "-ffunction-sections", "-fdata-sections", "-I" + filepath.Join(root, "include"), source}
	for _, path := range []string{
		"src/modules/stream/lx_stream_payload.c",
		"src/protocol/lxp_u128.c",
		"src/protocol/lxp_result.c",
		"src/crypto/lxp_ct.c",
	} {
		args = append(args, filepath.Join(root, path))
	}
	args = append(args, "-Wl,--gc-sections", "-o", binaryPath)
	if output, err := exec.Command("cc", args...).CombinedOutput(); err != nil {
		t.Fatalf("compile real stream decoder: %v\n%s", err, output)
	}
	return binaryPath
}

func streamDecode(t *testing.T, decoder, kind string, payload []byte) string {
	t.Helper()
	cmd := exec.Command(decoder, kind)
	cmd.Stdin = bytes.NewReader(payload)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("run real stream decoder: %v\n%s", err, output)
	}
	return strings.TrimSuffix(string(output), "\n")
}

func streamID(marker byte) [32]byte {
	var id [32]byte
	for i := range id {
		id[i] = marker + byte(i)
	}
	return id
}

func streamHex32(id [32]byte) string { return fmt.Sprintf("%x", id[:]) }

func streamHex128(v codec.U128) string { return fmt.Sprintf("%016x%016x", v.Hi, v.Lo) }

func streamHex64(v uint64) string { return fmt.Sprintf("%016x", v) }

func streamOpenFields(open StreamOpen) string {
	fields := []string{
		streamHex32(open.StreamID), streamHex32([32]byte{}), streamHex32(open.StreamAccount), streamHex32(open.Recipient),
		streamHex32(open.AssetID), fmt.Sprintf("%d", open.Mode), streamHex128(open.Rate), streamHex64(open.RateUnit),
		streamHex64(open.StartTimestamp), streamHex64(open.StartTimestamp), streamHex64(open.EndTimestamp),
		streamHex128(open.TotalCap), streamHex128(open.InitialFunding), fmt.Sprintf("%d", len(open.MeterAuthorities)),
	}
	for _, authority := range open.MeterAuthorities {
		fields = append(fields, streamHex32(authority))
	}
	return strings.Join(fields, " ")
}

type streamEdit struct {
	offset int
	value  []byte
}

func streamMutate(t *testing.T, encode func() ([]byte, error), edits ...streamEdit) []byte {
	t.Helper()
	payload, err := encode()
	if err != nil {
		t.Fatal(err)
	}
	for _, edit := range edits {
		if edit.offset+len(edit.value) > len(payload) {
			payload = append(payload, make([]byte, edit.offset+len(edit.value)-len(payload))...)
		}
		copy(payload[edit.offset:], edit.value)
	}
	return payload
}

func TestStreamPayloads(t *testing.T) {
	decoder := streamDecoder(t)

	t.Run("decoder_constants", func(t *testing.T) {
		output, err := exec.Command(decoder, "lengths").CombinedOutput()
		if err != nil {
			t.Fatalf("read decoder constants: %v\n%s", err, output)
		}
		want := fmt.Sprintf("%d %d %d %d %d %d %d %d %d %d %d %d\n",
			streamPayloadVersion, StreamMaxMeterAuthorities, StreamOpenPayloadFixedBytes, StreamOpenPayloadMaxBytes,
			StreamTopUpPayloadBytes, StreamMeterPayloadBytes, StreamKeyedPayloadBytes, StreamRateChangePayloadBytes,
			StreamIDPayloadBytes, StreamModeTime, StreamModeMetered, 0x00040008)
		if want != "1 8 204 460 50 138 66 82 34 1 2 262152\n" || string(output) != want {
			t.Fatalf("decoder constants = %q, Go constants = %q", output, want)
		}
	})

	id, account, recipient, asset := streamID(0x10), streamID(0x30), streamID(0x50), streamID(0x70)
	key, authorityKey := streamID(0x90), streamID(0xb0)
	rate := codec.U128{Hi: 0x0102030405060708, Lo: 0x090a0b0c0d0e0f10}
	newRate := codec.U128{Hi: 0x1112131415161718, Lo: 0x191a1b1c1d1e1f20}
	totalCap := codec.U128{Hi: 0x2122232425262728, Lo: 0x292a2b2c2d2e2f30}
	funding := codec.U128{Hi: 0x3132333435363738, Lo: 0x393a3b3c3d3e3f40}
	amount := codec.U128{Hi: math.MaxUint64, Lo: math.MaxUint64}
	var signature [64]byte
	for i := range signature {
		signature[i] = byte(0xc0 + i)
	}
	authorities := make([][32]byte, StreamMaxMeterAuthorities)
	for i := range authorities {
		authorities[i] = streamID(byte(0x01 + 0x10*i))
	}
	timeOpen := StreamOpen{
		StreamID: id, StreamAccount: account, Recipient: recipient, AssetID: asset, Mode: StreamModeTime,
		Rate: rate, RateUnit: 0x4142434445464748, StartTimestamp: 0x5152535455565758, EndTimestamp: 0x6162636465666768,
		TotalCap: totalCap, InitialFunding: funding,
	}
	meteredOne := timeOpen
	meteredOne.Mode, meteredOne.EndTimestamp, meteredOne.MeterAuthorities = StreamModeMetered, 0, authorities[:1]
	meteredFull := timeOpen
	meteredFull.Mode, meteredFull.MeterAuthorities = StreamModeMetered, authorities
	openOf := func(open StreamOpen) func() ([]byte, error) {
		return func() ([]byte, error) { return EncodeStreamOpen(open) }
	}
	topUp := func() ([]byte, error) { return EncodeStreamTopUp(id, amount) }
	meter := func() ([]byte, error) { return EncodeStreamMeter(id, math.MaxUint64, authorityKey, signature) }
	keyed := func() ([]byte, error) { return EncodeStreamKeyed(id, key) }
	streamIDPayload := func() ([]byte, error) { return EncodeStreamID(id) }
	rateChange := func() ([]byte, error) { return EncodeStreamRateChange(id, newRate, key) }

	vectors := []struct {
		name   string
		kind   string
		size   int
		encode func() ([]byte, error)
		fields string
	}{
		{"open_time", "open", 204, openOf(timeOpen), streamOpenFields(timeOpen)},
		{"open_metered_one_authority", "open", 236, openOf(meteredOne), streamOpenFields(meteredOne)},
		{"open_metered_eight_authorities", "open", 460, openOf(meteredFull), streamOpenFields(meteredFull)},
		{"top_up", "top_up", 50, topUp, streamHex32(id) + " " + streamHex128(amount)},
		{"meter", "meter", 138, meter, streamHex32(id) + " " + streamHex64(math.MaxUint64) + " " + streamHex32(authorityKey) + " " + fmt.Sprintf("%x", signature[:])},
		{"meter_zero_reading", "meter", 138, func() ([]byte, error) { return EncodeStreamMeter(id, 0, authorityKey, signature) },
			streamHex32(id) + " " + streamHex64(0) + " " + streamHex32(authorityKey) + " " + fmt.Sprintf("%x", signature[:])},
		{"keyed", "keyed", 66, keyed, streamHex32(id) + " " + streamHex32(key)},
		{"id", "id", 34, streamIDPayload, streamHex32(id)},
		{"rate_change", "rate_change", 82, rateChange, streamHex32(id) + " " + streamHex128(newRate) + " " + streamHex32(key)},
	}
	for _, v := range vectors {
		t.Run("decode/"+v.name, func(t *testing.T) {
			payload, err := v.encode()
			if err != nil {
				t.Fatal(err)
			}
			if len(payload) != v.size {
				t.Fatalf("payload length = %d, want %d", len(payload), v.size)
			}
			if payload[0] != 0 || payload[1] != 1 {
				t.Fatalf("payload header = %x, want 0001", payload[:2])
			}
			if got, want := streamDecode(t, decoder, v.kind, payload), "0 LXP_OK "+v.fields; got != want {
				t.Fatalf("decoded fields\n got %q\nwant %q", got, want)
			}
			if got := streamDecode(t, decoder, v.kind, payload[:len(payload)-1]); got != streamDecodedNonCanonical {
				t.Fatalf("truncated payload: got %q, want %q", got, streamDecodedNonCanonical)
			}
			if got := streamDecode(t, decoder, v.kind, append(bytes.Clone(payload), 1)); got != streamDecodedNonCanonical {
				t.Fatalf("extended payload: got %q, want %q", got, streamDecodedNonCanonical)
			}
			for _, header := range [][2]byte{{1, 1}, {0, 0}, {0, 2}, {0xff, 0xff}} {
				invalid := bytes.Clone(payload)
				copy(invalid, header[:])
				if got := streamDecode(t, decoder, v.kind, invalid); got != streamDecodedVersion {
					t.Fatalf("header %x: got %q, want %q", header, got, streamDecodedVersion)
				}
			}
		})
	}

	t.Run("rate_change_golden", func(t *testing.T) {
		payload, err := EncodeStreamRateChange([32]byte{1}, codec.U128{Hi: 2, Lo: 3}, [32]byte{4})
		if err != nil {
			t.Fatal(err)
		}
		want := "0001" +
			"0100000000000000000000000000000000000000000000000000000000000000" +
			"00000000000000020000000000000003" +
			"0400000000000000000000000000000000000000000000000000000000000000"
		if got := fmt.Sprintf("%x", payload); got != want {
			t.Fatalf("rate change payload\n got %s\nwant %s", got, want)
		}
	})

	t.Run("independent_buffers", func(t *testing.T) {
		input := bytes.Clone(authorities[0][:])
		first, err := EncodeStreamOpen(meteredFull)
		if err != nil {
			t.Fatal(err)
		}
		snapshot := bytes.Clone(first)
		for i := range first {
			first[i] ^= 0xff
		}
		second, err := EncodeStreamOpen(meteredFull)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(second, snapshot) {
			t.Fatalf("encoding changed after modifying a returned payload: got %x, want %x", second, snapshot)
		}
		if !bytes.Equal(authorities[0][:], input) {
			t.Fatalf("meter authority input changed: %x", authorities[0])
		}
	})

	t.Run("refusals", func(t *testing.T) {
		zero := [32]byte{}
		zero16 := make([]byte, 16)
		zero8 := make([]byte, 8)
		with := func(change func(*StreamOpen)) StreamOpen {
			open := timeOpen
			change(&open)
			return open
		}
		withMetered := func(change func(*StreamOpen)) StreamOpen {
			open := meteredFull
			open.MeterAuthorities = append([][32]byte(nil), meteredFull.MeterAuthorities...)
			change(&open)
			return open
		}
		nine := append(append([][32]byte(nil), authorities...), streamID(0x90))
		openRefusals := []struct {
			name string
			open StreamOpen
			raw  func(*testing.T) []byte
		}{
			{"zero_stream_id", with(func(o *StreamOpen) { o.StreamID = zero }),
				func(t *testing.T) []byte { return streamMutate(t, openOf(timeOpen), streamEdit{2, zero[:]}) }},
			{"zero_stream_account", with(func(o *StreamOpen) { o.StreamAccount = zero }),
				func(t *testing.T) []byte { return streamMutate(t, openOf(timeOpen), streamEdit{34, zero[:]}) }},
			{"zero_recipient", with(func(o *StreamOpen) { o.Recipient = zero }),
				func(t *testing.T) []byte { return streamMutate(t, openOf(timeOpen), streamEdit{66, zero[:]}) }},
			{"zero_asset", with(func(o *StreamOpen) { o.AssetID = zero }),
				func(t *testing.T) []byte { return streamMutate(t, openOf(timeOpen), streamEdit{98, zero[:]}) }},
			{"account_is_recipient", with(func(o *StreamOpen) { o.Recipient = account }),
				func(t *testing.T) []byte { return streamMutate(t, openOf(timeOpen), streamEdit{66, account[:]}) }},
			{"mode_zero", with(func(o *StreamOpen) { o.Mode = 0 }),
				func(t *testing.T) []byte { return streamMutate(t, openOf(timeOpen), streamEdit{130, []byte{0}}) }},
			{"mode_three", with(func(o *StreamOpen) { o.Mode = 3 }),
				func(t *testing.T) []byte { return streamMutate(t, openOf(timeOpen), streamEdit{130, []byte{3}}) }},
			{"zero_rate", with(func(o *StreamOpen) { o.Rate = codec.U128{} }),
				func(t *testing.T) []byte { return streamMutate(t, openOf(timeOpen), streamEdit{131, zero16}) }},
			{"zero_rate_unit", with(func(o *StreamOpen) { o.RateUnit = 0 }),
				func(t *testing.T) []byte { return streamMutate(t, openOf(timeOpen), streamEdit{147, zero8}) }},
			{"zero_start", with(func(o *StreamOpen) { o.StartTimestamp, o.EndTimestamp = 0, 0 }),
				func(t *testing.T) []byte {
					return streamMutate(t, openOf(timeOpen), streamEdit{155, zero8}, streamEdit{163, zero8})
				}},
			{"end_equals_start", with(func(o *StreamOpen) { o.EndTimestamp = o.StartTimestamp }),
				func(t *testing.T) []byte {
					return streamMutate(t, openOf(timeOpen), streamEdit{163, binary.BigEndian.AppendUint64(nil, timeOpen.StartTimestamp)})
				}},
			{"end_before_start", with(func(o *StreamOpen) { o.EndTimestamp = o.StartTimestamp - 1 }),
				func(t *testing.T) []byte {
					return streamMutate(t, openOf(timeOpen), streamEdit{163, binary.BigEndian.AppendUint64(nil, timeOpen.StartTimestamp-1)})
				}},
			{"zero_total_cap", with(func(o *StreamOpen) { o.TotalCap = codec.U128{} }),
				func(t *testing.T) []byte { return streamMutate(t, openOf(timeOpen), streamEdit{171, zero16}) }},
			{"zero_initial_funding", with(func(o *StreamOpen) { o.InitialFunding = codec.U128{} }),
				func(t *testing.T) []byte { return streamMutate(t, openOf(timeOpen), streamEdit{187, zero16}) }},
			{"time_with_authority", with(func(o *StreamOpen) { o.MeterAuthorities = authorities[:1] }),
				func(t *testing.T) []byte {
					return streamMutate(t, openOf(meteredOne), streamEdit{130, []byte{StreamModeTime}})
				}},
			{"metered_without_authority", with(func(o *StreamOpen) { o.Mode = StreamModeMetered }),
				func(t *testing.T) []byte {
					return streamMutate(t, openOf(timeOpen), streamEdit{130, []byte{StreamModeMetered}})
				}},
			{"nine_authorities", withMetered(func(o *StreamOpen) { o.MeterAuthorities = nine }),
				func(t *testing.T) []byte {
					return streamMutate(t, openOf(meteredFull), streamEdit{203, []byte{9}}, streamEdit{StreamOpenPayloadMaxBytes, nine[8][:]})
				}},
			{"zero_authority", withMetered(func(o *StreamOpen) { o.MeterAuthorities[0] = zero }),
				func(t *testing.T) []byte { return streamMutate(t, openOf(meteredFull), streamEdit{204, zero[:]}) }},
			{"duplicate_authority", withMetered(func(o *StreamOpen) { o.MeterAuthorities[4] = o.MeterAuthorities[3] }),
				func(t *testing.T) []byte {
					return streamMutate(t, openOf(meteredFull), streamEdit{204 + 4*32, authorities[3][:]})
				}},
			{"descending_authorities", withMetered(func(o *StreamOpen) {
				o.MeterAuthorities[6], o.MeterAuthorities[7] = o.MeterAuthorities[7], o.MeterAuthorities[6]
			}),
				func(t *testing.T) []byte {
					return streamMutate(t, openOf(meteredFull), streamEdit{204 + 6*32, authorities[7][:]}, streamEdit{204 + 7*32, authorities[6][:]})
				}},
		}
		for _, r := range openRefusals {
			t.Run("open/"+r.name, func(t *testing.T) {
				payload, err := EncodeStreamOpen(r.open)
				if !errors.Is(err, errStreamNonCanonical) || payload != nil {
					t.Fatalf("encode = %x, %v; want nil, %v", payload, err, errStreamNonCanonical)
				}
				if got := streamDecode(t, decoder, "open", r.raw(t)); got != streamDecodedNonCanonical {
					t.Fatalf("decoder: got %q, want %q", got, streamDecodedNonCanonical)
				}
			})
		}

		refusals := []struct {
			name   string
			kind   string
			encode func() ([]byte, error)
			raw    func(*testing.T) []byte
		}{
			{"top_up_zero_id", "top_up", func() ([]byte, error) { return EncodeStreamTopUp(zero, amount) },
				func(t *testing.T) []byte { return streamMutate(t, topUp, streamEdit{2, zero[:]}) }},
			{"top_up_zero_amount", "top_up", func() ([]byte, error) { return EncodeStreamTopUp(id, codec.U128{}) },
				func(t *testing.T) []byte { return streamMutate(t, topUp, streamEdit{34, zero16}) }},
			{"meter_zero_id", "meter", func() ([]byte, error) { return EncodeStreamMeter(zero, 1, authorityKey, signature) },
				func(t *testing.T) []byte { return streamMutate(t, meter, streamEdit{2, zero[:]}) }},
			{"meter_zero_authority", "meter", func() ([]byte, error) { return EncodeStreamMeter(id, 1, zero, signature) },
				func(t *testing.T) []byte { return streamMutate(t, meter, streamEdit{42, zero[:]}) }},
			{"meter_zero_signature", "meter", func() ([]byte, error) { return EncodeStreamMeter(id, 1, authorityKey, [64]byte{}) },
				func(t *testing.T) []byte { return streamMutate(t, meter, streamEdit{74, make([]byte, 64)}) }},
			{"keyed_zero_id", "keyed", func() ([]byte, error) { return EncodeStreamKeyed(zero, key) },
				func(t *testing.T) []byte { return streamMutate(t, keyed, streamEdit{2, zero[:]}) }},
			{"keyed_zero_key", "keyed", func() ([]byte, error) { return EncodeStreamKeyed(id, zero) },
				func(t *testing.T) []byte { return streamMutate(t, keyed, streamEdit{34, zero[:]}) }},
			{"id_zero", "id", func() ([]byte, error) { return EncodeStreamID(zero) },
				func(t *testing.T) []byte { return streamMutate(t, streamIDPayload, streamEdit{2, zero[:]}) }},
			{"rate_change_zero_id", "rate_change", func() ([]byte, error) { return EncodeStreamRateChange(zero, newRate, key) },
				func(t *testing.T) []byte { return streamMutate(t, rateChange, streamEdit{2, zero[:]}) }},
			{"rate_change_zero_rate", "rate_change", func() ([]byte, error) { return EncodeStreamRateChange(id, codec.U128{}, key) },
				func(t *testing.T) []byte { return streamMutate(t, rateChange, streamEdit{34, zero16}) }},
			{"rate_change_zero_key", "rate_change", func() ([]byte, error) { return EncodeStreamRateChange(id, newRate, zero) },
				func(t *testing.T) []byte { return streamMutate(t, rateChange, streamEdit{50, zero[:]}) }},
		}
		for _, r := range refusals {
			t.Run(r.name, func(t *testing.T) {
				payload, err := r.encode()
				if !errors.Is(err, errStreamNonCanonical) || payload != nil {
					t.Fatalf("encode = %x, %v; want nil, %v", payload, err, errStreamNonCanonical)
				}
				if got := streamDecode(t, decoder, r.kind, r.raw(t)); got != streamDecodedNonCanonical {
					t.Fatalf("decoder: got %q, want %q", got, streamDecodedNonCanonical)
				}
			})
		}
	})
}
