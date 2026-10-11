package types

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/Sidiora-Labs/Paxeer-X-Network/layerxproof/codec"
)

const serviceDecoderSource = `
#include "lx_service_dispatch.h"
#include <errno.h>
#include <inttypes.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

int main(int argc, char **argv)
{
    uint8_t payload[4096];
    lx_service_decoded decoded;
    lxp_result result;
    size_t length;
    unsigned long ordinal;
    char *end;
    if (argc != 2) return 2;
    if (strcmp(argv[1], "lengths") == 0) {
        printf("%d %d %d %d %d %d %d %d %d %d %d %d\n",
               LX_SERVICE_OFFER_PUBLISH_PAYLOAD_BYTES,
               LX_SERVICE_IDENTIFIER_PAYLOAD_BYTES,
               LX_SERVICE_AGREEMENT_PROPOSE_PAYLOAD_BYTES,
               LX_SERVICE_COMMIT_TASK_PAYLOAD_BYTES,
               LX_SERVICE_COMMIT_ABANDON_PAYLOAD_BYTES,
               LX_SERVICE_PROGRESS_PAYLOAD_BYTES,
               LX_SERVICE_DELIVER_PAYLOAD_FIXED_BYTES,
               LX_SERVICE_DELIVER_PAYLOAD_ITEM_BYTES,
               LX_SERVICE_REJECT_PAYLOAD_FIXED_BYTES,
               LX_SERVICE_DISPUTE_OPEN_PAYLOAD_FIXED_BYTES,
               LX_SERVICE_DISPUTE_RESOLVE_PAYLOAD_BYTES,
               LX_SERVICE_EXECUTION_BYTES);
        return 0;
    }
    errno = 0;
    ordinal = strtoul(argv[1], &end, 10);
    if (errno != 0 || end == argv[1] || *end != '\0' || ordinal > UINT16_MAX)
        return 2;
    length = fread(payload, 1, sizeof(payload), stdin);
    if (ferror(stdin) || fgetc(stdin) != EOF) return 2;
    result = lx_service_payload_decode((uint16_t)ordinal, payload, length, &decoded);
    printf("%" PRId32 " %s\n", result, lxp_result_name(result));
    return 0;
}
`

func serviceDecoder(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	source := filepath.Join(dir, "service_decoder.c")
	if err := os.WriteFile(source, []byte(serviceDecoderSource), 0600); err != nil {
		t.Fatal(err)
	}
	binaryPath := filepath.Join(dir, "service_decoder")
	args := []string{"-std=c11", "-Wall", "-Wextra", "-Werror", "-ffunction-sections", "-fdata-sections", "-I" + filepath.Join(root, "include"), "-I" + filepath.Join(root, "src", "modules", "service"), source}
	for _, path := range []string{
		"src/modules/service/lx_service_payload.c",
		"src/modules/service/lx_service_state.c",
		"src/modules/service/lx_service_attest.c",
		"src/protocol/lxp_u128.c",
		"src/protocol/lxp_result.c",
		"src/crypto/lxp_ct.c",
	} {
		args = append(args, filepath.Join(root, path))
	}
	args = append(args, "-Wl,--gc-sections", "-o", binaryPath)
	if output, err := exec.Command("cc", args...).CombinedOutput(); err != nil {
		t.Fatalf("compile real service decoder: %v\n%s", err, output)
	}
	return binaryPath
}

func serviceDecodeResult(t *testing.T, decoder string, ordinal uint16, payload []byte, code int32, name string) {
	t.Helper()
	cmd := exec.Command(decoder, strconv.FormatUint(uint64(ordinal), 10))
	cmd.Stdin = bytes.NewReader(payload)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("run real service decoder: %v\n%s", err, output)
	}
	if want := fmt.Sprintf("%d %s\n", code, name); string(output) != want {
		t.Fatalf("decoder ordinal %d length %d: got %q, want %q", ordinal, len(payload), output, want)
	}
}

func serviceHex(t *testing.T, value string) []byte {
	t.Helper()
	decoded, err := hex.DecodeString(value)
	if err != nil {
		t.Fatal(err)
	}
	return decoded
}

func serviceID(marker byte) [32]byte {
	var id [32]byte
	for i := range id {
		id[i] = marker
	}
	return id
}

func TestServicePayloads(t *testing.T) {
	decoder := serviceDecoder(t)
	t.Run("decoder_lengths", func(t *testing.T) {
		output, err := exec.Command(decoder, "lengths").CombinedOutput()
		if err != nil {
			t.Fatalf("read decoder lengths: %v\n%s", err, output)
		}
		if want := "179 34 130 146 36 134 67 72 37 67 72 416\n"; string(output) != want {
			t.Fatalf("decoder lengths = %q, want %q", output, want)
		}
	})

	a, b, c, d := serviceID(0x11), serviceID(0x22), serviceID(0x33), serviceID(0x44)
	ha, hb, hc, hd := strings.Repeat("11", 32), strings.Repeat("22", 32), strings.Repeat("33", 32), strings.Repeat("44", 32)
	price := codec.U128{Hi: 0x0123456789abcdef, Lo: 0xfedcba9876543210}
	const deadline uint64 = 0x1020304050607080
	const acceptance uint64 = 0x1122334455667788
	const dispute uint64 = 0x8877665544332211
	const expiry uint64 = 0xffffffffffffffff
	item := ServiceDeliverable{Hash: c, ArtifactSize: 0xfedcba9876543210, AvailabilityReference: d}
	type vector struct {
		name    string
		ordinal uint16
		minimum int
		encode  func() ([]byte, error)
		hex     string
	}
	vectors := []vector{
		{"offer", 1, 179, func() ([]byte, error) {
			return EncodeServiceOffer(a, b, price, c, d, deadline, acceptance, dispute, 2, expiry)
		}, "0001" + ha + hb + "0123456789abcdeffedcba9876543210" + hc + hd + "10203040506070801122334455667788887766554433221102ffffffffffffffff"},
		{"withdraw", 2, 34, func() ([]byte, error) { return EncodeServiceIdentifier(a) }, "0001" + ha},
		{"propose", 3, 130, func() ([]byte, error) { return EncodeServicePropose(a, b, c, d) }, "0001" + ha + hb + hc + hd},
		{"agreement_accept", 4, 34, func() ([]byte, error) { return EncodeServiceIdentifier(b) }, "0001" + hb},
		{"commit", 5, 146, func() ([]byte, error) { return EncodeServiceCommit(a, b, c, d, deadline, expiry) }, "0001" + ha + hb + hc + hd + "1020304050607080ffffffffffffffff"},
		{"abandon", 6, 36, func() ([]byte, error) { return EncodeServiceAbandon(a, 0xfedc) }, "0001" + ha + "fedc"},
		{"progress", 8, 134, func() ([]byte, error) { return EncodeServiceProgress(a, b, c, d, 10000) }, "0001" + ha + hb + hc + hd + "00002710"},
		{"deliver", 9, 67, func() ([]byte, error) { return EncodeServiceDeliver(a, b, []ServiceDeliverable{item}) }, "0001" + ha + hb + "01" + hc + "fedcba9876543210" + hd},
		{"accept", 10, 34, func() ([]byte, error) { return EncodeServiceIdentifier(c) }, "0001" + hc},
		{"reject", 11, 37, func() ([]byte, error) { return EncodeServiceReject(a, 0xfedc, [][32]byte{d, c}) }, "0001" + ha + "fedc02" + hc + hd},
		{"dispute_open", 12, 67, func() ([]byte, error) { return EncodeServiceDisputeOpen(a, b, [][32]byte{d, c}) }, "0001" + ha + hb + "02" + hc + hd},
		{"dispute_resolve", 13, 72, func() ([]byte, error) { return EncodeServiceDisputeResolve(a, 0xfedc, 6000, b) }, "0001" + ha + "fedc00001770" + hb},
	}
	for _, v := range vectors {
		t.Run("wire/"+v.name, func(t *testing.T) {
			payload, err := v.encode()
			if err != nil {
				t.Fatal(err)
			}
			want := serviceHex(t, v.hex)
			if !bytes.Equal(payload, want) {
				t.Fatalf("payload = %x, want %x", payload, want)
			}
			serviceDecodeResult(t, decoder, v.ordinal, payload, 0, "LXP_OK")
			serviceDecodeResult(t, decoder, v.ordinal, payload[:v.minimum-1], -1, "LXP_ERR_TRUNCATED")
			lengthCode, lengthName := int32(-2), "LXP_ERR_TRAILING_BYTES"
			if v.ordinal == 9 || v.ordinal == 11 || v.ordinal == 12 {
				lengthCode, lengthName = -3, "LXP_ERR_NON_CANONICAL"
				serviceDecodeResult(t, decoder, v.ordinal, payload[:len(payload)-1], lengthCode, lengthName)
			}
			serviceDecodeResult(t, decoder, v.ordinal, append(bytes.Clone(payload), 0), lengthCode, lengthName)
			for _, version := range [][2]byte{{1, 1}, {0, 0}, {0, 2}} {
				invalid := bytes.Clone(payload)
				copy(invalid, version[:])
				serviceDecodeResult(t, decoder, v.ordinal, invalid, -101, "LXP_ERR_VERSION_UNSUPPORTED")
			}
		})
	}

	t.Run("refusals", func(t *testing.T) {
		type refusal struct {
			name     string
			ordinal  uint16
			encode   func() ([]byte, error)
			raw      []byte
			code     int32
			nameCode string
		}
		var cases []refusal
		for _, v := range vectors {
			if v.ordinal == 1 {
				for field, span := range [][2]int{{2, 34}, {34, 66}, {66, 82}, {82, 114}, {114, 146}, {146, 154}, {154, 162}, {162, 170}, {170, 171}, {171, 179}} {
					ids := [4][32]byte{a, b, c, d}
					amount := price
					times := [4]uint64{deadline, acceptance, dispute, expiry}
					outcome := uint8(2)
					switch field {
					case 0:
						ids[0] = [32]byte{}
					case 1:
						ids[1] = [32]byte{}
					case 2:
						amount = codec.U128{}
					case 3:
						ids[2] = [32]byte{}
					case 4:
						ids[3] = [32]byte{}
					case 5, 6, 7:
						times[field-5] = 0
					case 8:
						outcome = 0
					case 9:
						times[3] = 0
					}
					raw := serviceHex(t, v.hex)
					clear(raw[span[0]:span[1]])
					cases = append(cases, refusal{fmt.Sprintf("offer_zero_%d", field), 1, func() ([]byte, error) {
						return EncodeServiceOffer(ids[0], ids[1], amount, ids[2], ids[3], times[0], times[1], times[2], outcome, times[3])
					}, raw, -3, "LXP_ERR_NON_CANONICAL"})
				}
				for _, outcome := range []uint8{3, 255} {
					raw := serviceHex(t, v.hex)
					raw[170] = outcome
					cases = append(cases, refusal{fmt.Sprintf("offer_outcome_%d", outcome), 1, func() ([]byte, error) {
						return EncodeServiceOffer(a, b, price, c, d, deadline, acceptance, dispute, outcome, expiry)
					}, raw, -3, "LXP_ERR_NON_CANONICAL"})
				}
				continue
			}
			var spans [][2]int
			code, name := int32(-3), "LXP_ERR_NON_CANONICAL"
			switch v.ordinal {
			case 2, 4, 10:
				spans = [][2]int{{2, 34}}
			case 3:
				spans = [][2]int{{2, 34}, {34, 66}, {66, 98}, {98, 130}}
			case 5:
				spans = [][2]int{{2, 34}, {34, 66}, {66, 98}, {130, 138}, {138, 146}}
			case 6:
				spans = [][2]int{{2, 34}, {34, 36}}
			case 8:
				spans = [][2]int{{2, 34}, {34, 66}, {66, 98}, {98, 130}, {130, 134}}
				code, name = -734, "LXP_ERR_PARAMETER_BOUNDS"
			case 9:
				spans = [][2]int{{2, 34}, {34, 66}, {67, 99}, {99, 107}, {107, 139}}
			case 11:
				spans = [][2]int{{2, 34}, {34, 36}, {37, 69}, {69, 101}}
			case 12:
				spans = [][2]int{{2, 34}, {34, 66}, {67, 99}, {99, 131}}
			case 13:
				spans = [][2]int{{2, 34}, {34, 36}, {40, 72}}
				code, name = -734, "LXP_ERR_PARAMETER_BOUNDS"
			}
			for field, span := range spans {
				raw := serviceHex(t, v.hex)
				clear(raw[span[0]:span[1]])
				cases = append(cases, refusal{fmt.Sprintf("%s_zero_%d", v.name, field), v.ordinal, func() ([]byte, error) { return serviceEncodeFields(v.ordinal, raw) }, raw, code, name})
			}
			if v.ordinal == 8 || v.ordinal == 13 {
				for _, bps := range []uint32{10001, 0xffffffff} {
					raw := serviceHex(t, v.hex)
					offset := 130
					if v.ordinal == 13 {
						offset = 36
					}
					binary.BigEndian.PutUint32(raw[offset:offset+4], bps)
					cases = append(cases, refusal{fmt.Sprintf("%s_bps_%d", v.name, bps), v.ordinal, func() ([]byte, error) { return serviceEncodeFields(v.ordinal, raw) }, raw, -734, "LXP_ERR_PARAMETER_BOUNDS"})
				}
			}
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				payload, err := tc.encode()
				want := "LXP_" + strings.TrimPrefix(tc.nameCode, "LXP_ERR_")
				if payload != nil || err == nil || err.Error() != want {
					t.Fatalf("encoding = %x, %v; want nil, %s", payload, err, want)
				}
				serviceDecodeResult(t, decoder, tc.ordinal, tc.raw, tc.code, tc.nameCode)
			})
		}
	})

	t.Run("lists", func(t *testing.T) {
		for count := 1; count <= 16; count++ {
			t.Run(strconv.Itoa(count), func(t *testing.T) {
				deliverables := make([]ServiceDeliverable, count)
				hashes := make([][32]byte, count)
				for i := range deliverables {
					deliverables[i] = item
					hashes[i] = serviceID(byte(count - i))
				}
				original := slices.Clone(hashes)
				payload, err := EncodeServiceDeliver(a, b, deliverables)
				want := "0001" + ha + hb + fmt.Sprintf("%02x", count) + strings.Repeat(hc+"fedcba9876543210"+hd, count)
				if err != nil || !bytes.Equal(payload, serviceHex(t, want)) {
					t.Fatalf("deliver = %x, %v; want %s", payload, err, want)
				}
				serviceDecodeResult(t, decoder, 9, payload, 0, "LXP_OK")
				for _, ordinal := range []uint16{11, 12} {
					var encoded []byte
					var err error
					prefix := "0001" + ha + "fedc" + fmt.Sprintf("%02x", count)
					if ordinal == 11 {
						encoded, err = EncodeServiceReject(a, 0xfedc, hashes)
					} else {
						encoded, err = EncodeServiceDisputeOpen(a, b, hashes)
						prefix = "0001" + ha + hb + fmt.Sprintf("%02x", count)
					}
					for i := 1; i <= count; i++ {
						prefix += strings.Repeat(fmt.Sprintf("%02x", i), 32)
					}
					if err != nil || !bytes.Equal(encoded, serviceHex(t, prefix)) || !slices.Equal(hashes, original) {
						t.Fatalf("ordinal %d = %x, %v; want %s with unchanged input", ordinal, encoded, err, prefix)
					}
					serviceDecodeResult(t, decoder, ordinal, encoded, 0, "LXP_OK")
				}
			})
		}
		for _, count := range []int{0, 17, 255, 256, 257} {
			t.Run("refuse_"+strconv.Itoa(count), func(t *testing.T) {
				for _, encode := range []func() ([]byte, error){
					func() ([]byte, error) {
						return EncodeServiceDeliver([32]byte{}, [32]byte{}, make([]ServiceDeliverable, count))
					},
					func() ([]byte, error) { return EncodeServiceReject([32]byte{}, 0, make([][32]byte, count)) },
					func() ([]byte, error) {
						return EncodeServiceDisputeOpen([32]byte{}, [32]byte{}, make([][32]byte, count))
					},
				} {
					payload, err := encode()
					if payload != nil || err == nil || err.Error() != "LXP_LENGTH_LIMIT" {
						t.Fatalf("count %d = %x, %v; want nil, LXP_LENGTH_LIMIT", count, payload, err)
					}
				}
				if count == 0 || count == 17 || count == 255 {
					for _, v := range vectors {
						if v.ordinal != 9 && v.ordinal != 11 && v.ordinal != 12 {
							continue
						}
						raw := serviceHex(t, v.hex)
						raw[v.minimum-1] = byte(count)
						serviceDecodeResult(t, decoder, v.ordinal, raw, -5, "LXP_ERR_LENGTH_LIMIT")
					}
				}
			})
		}
		for _, ordinal := range []uint16{11, 12} {
			t.Run("duplicate_"+strconv.Itoa(int(ordinal)), func(t *testing.T) {
				hashes := [][32]byte{c, d, c}
				var payload []byte
				var err error
				var raw []byte
				if ordinal == 11 {
					payload, err = EncodeServiceReject(a, 2, hashes)
					raw = serviceHex(t, "0001"+ha+"000203"+hc+hd+hc)
				} else {
					payload, err = EncodeServiceDisputeOpen(a, b, hashes)
					raw = serviceHex(t, "0001"+ha+hb+"03"+hc+hd+hc)
				}
				if payload != nil || err == nil || err.Error() != "LXP_DUPLICATE_ENTRY" {
					t.Fatalf("duplicate = %x, %v; want nil, LXP_DUPLICATE_ENTRY", payload, err)
				}
				serviceDecodeResult(t, decoder, ordinal, raw, -8, "LXP_ERR_DUPLICATE_ENTRY")
			})
		}
	})

	t.Run("boundary_values", func(t *testing.T) {
		payload, err := EncodeServiceOffer(a, b, codec.U128{Lo: 1}, c, d, 1, 1, 1, 1, 1)
		if err != nil || !bytes.Equal(payload, serviceHex(t, "0001"+ha+hb+strings.Repeat("00", 15)+"01"+hc+hd+strings.Repeat("0000000000000001", 3)+"01"+"0000000000000001")) {
			t.Fatalf("minimum offer = %x, %v", payload, err)
		}
		serviceDecodeResult(t, decoder, 1, payload, 0, "LXP_OK")
		payload, err = EncodeServiceCommit(a, b, c, [32]byte{}, 1, 1)
		if err != nil || !bytes.Equal(payload, serviceHex(t, "0001"+ha+hb+hc+strings.Repeat("00", 32)+strings.Repeat("0000000000000001", 2))) {
			t.Fatalf("zero escrow commit = %x, %v", payload, err)
		}
		serviceDecodeResult(t, decoder, 5, payload, 0, "LXP_OK")
		payload, err = EncodeServiceProgress(a, b, c, d, 1)
		if err != nil || !bytes.Equal(payload, serviceHex(t, "0001"+ha+hb+hc+hd+"00000001")) {
			t.Fatalf("minimum progress = %x, %v", payload, err)
		}
		serviceDecodeResult(t, decoder, 8, payload, 0, "LXP_OK")
		for _, bps := range []uint32{0, 10000} {
			payload, err := EncodeServiceDisputeResolve(a, 1, bps, b)
			if err != nil || !bytes.Equal(payload, serviceHex(t, "0001"+ha+"0001"+fmt.Sprintf("%08x", bps)+hb)) {
				t.Fatalf("resolve bps %d = %x, %v", bps, payload, err)
			}
			serviceDecodeResult(t, decoder, 13, payload, 0, "LXP_OK")
		}
	})

	t.Run("execution", func(t *testing.T) {
		execution := make([]byte, 416)
		for i := range execution {
			execution[i] = byte(i)
		}
		payload, err := EncodeServiceToolExecAttest(execution)
		if err != nil || !bytes.Equal(payload, execution) || len(payload) != 416 {
			t.Fatalf("execution = %x, %v; want unchanged 416 bytes", payload, err)
		}
		serviceDecodeResult(t, decoder, 7, payload, 0, "LXP_OK")
		payload[0] ^= 0xff
		if execution[0] != 0 {
			t.Fatal("encoded execution aliases caller input")
		}
		for _, length := range []int{0, 1, 415, 417, 418} {
			raw := make([]byte, length)
			encoded, err := EncodeServiceToolExecAttest(raw)
			if encoded != nil || err == nil || err.Error() != "LXP_NON_CANONICAL" {
				t.Fatalf("execution length %d = %x, %v; want nil, LXP_NON_CANONICAL", length, encoded, err)
			}
			serviceDecodeResult(t, decoder, 7, raw, -3, "LXP_ERR_NON_CANONICAL")
		}
		zero := make([]byte, 416)
		encoded, err := EncodeServiceToolExecAttest(zero)
		if err != nil || !bytes.Equal(encoded, zero) {
			t.Fatalf("raw zero execution = %x, %v", encoded, err)
		}
		serviceDecodeResult(t, decoder, 7, encoded, 0, "LXP_OK")
	})
}

func serviceEncodeFields(ordinal uint16, raw []byte) ([]byte, error) {
	id := func(offset int) [32]byte { return [32]byte(raw[offset : offset+32]) }
	switch ordinal {
	case 2, 4, 10:
		return EncodeServiceIdentifier(id(2))
	case 3:
		return EncodeServicePropose(id(2), id(34), id(66), id(98))
	case 5:
		return EncodeServiceCommit(id(2), id(34), id(66), id(98), binary.BigEndian.Uint64(raw[130:138]), binary.BigEndian.Uint64(raw[138:146]))
	case 6:
		return EncodeServiceAbandon(id(2), binary.BigEndian.Uint16(raw[34:36]))
	case 8:
		return EncodeServiceProgress(id(2), id(34), id(66), id(98), binary.BigEndian.Uint32(raw[130:134]))
	case 9:
		return EncodeServiceDeliver(id(2), id(34), []ServiceDeliverable{{Hash: id(67), ArtifactSize: binary.BigEndian.Uint64(raw[99:107]), AvailabilityReference: id(107)}})
	case 11:
		return EncodeServiceReject(id(2), binary.BigEndian.Uint16(raw[34:36]), [][32]byte{id(37), id(69)})
	case 12:
		return EncodeServiceDisputeOpen(id(2), id(34), [][32]byte{id(67), id(99)})
	case 13:
		return EncodeServiceDisputeResolve(id(2), binary.BigEndian.Uint16(raw[34:36]), binary.BigEndian.Uint32(raw[36:40]), id(40))
	default:
		return nil, fmt.Errorf("unexpected fixture ordinal %d", ordinal)
	}
}
