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
	"strconv"
	"strings"
	"testing"

	"github.com/Sidiora-Labs/Paxeer-X-Network/layerxproof/codec"
)

const budgetDecoderSource = `
#include "layerx/lx_budget.h"
#include <errno.h>
#include <inttypes.h>
#include <stdio.h>
#include <stdlib.h>
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

static void grant(const lxp_payer_grant *g)
{
    hex(g->grant_id, 32U);
    hex(g->from, 32U);
    hex(g->recipient, 32U);
    hex(g->asset, 32U);
    u128(g->per_draw_maximum);
    u128(g->allowance);
    printf(" %d", g->recurring ? 1 : 0);
    u64(g->window_length);
    u64(g->expiration);
    hex(g->purpose_hash, 32U);
    printf(" %d", g->has_reference ? 1 : 0);
    hex(g->reference_hash, 32U);
    u64(g->revocation_sequence);
    hex(g->public_key, 32U);
    hex(g->signature, 64U);
}

int main(int argc, char **argv)
{
    static uint8_t payload[4096];
    lx_budget_create_payload create;
    lx_budget_amount_payload amount;
    lx_budget_amend_payload amend;
    lx_budget_delegate_payload delegate;
    lx_budget_spend_payload spend;
    lx_budget_close_payload close;
    lx_budget_defund_payload defund;
    lx_budget_revoke_payload revoke;
    lx_budget_delegate_spend_payload delegate_spend;
    lx_budget_pull_payload pull;
    lxp_result result;
    size_t length;
    unsigned long ordinal;
    char *end;
    if (argc != 2) return 2;
    if (strcmp(argv[1], "lengths") == 0) {
        printf("%d %d %d %d %d %d %d %d %d %d %d %d\n",
               LX_BUDGET_CREATE_PAYLOAD_BYTES,
               LX_BUDGET_CREATE_V2_PAYLOAD_BYTES,
               LX_BUDGET_FUND_PAYLOAD_BYTES,
               LX_BUDGET_FUND_V2_PAYLOAD_BYTES,
               LX_BUDGET_AMEND_PAYLOAD_BYTES,
               LX_BUDGET_DELEGATE_PAYLOAD_BYTES,
               LX_BUDGET_SPEND_PAYLOAD_BYTES,
               LX_BUDGET_CLOSE_PAYLOAD_BYTES,
               LX_BUDGET_DEFUND_PAYLOAD_BYTES,
               LX_BUDGET_REVOKE_PAYLOAD_BYTES,
               LX_BUDGET_DELEGATE_SPEND_PAYLOAD_BYTES,
               LX_BUDGET_PULL_PAYLOAD_BYTES);
        return 0;
    }
    errno = 0;
    ordinal = strtoul(argv[1], &end, 10);
    if (errno != 0 || end == argv[1] || *end != '\0' || ordinal == 0UL || ordinal > 11UL)
        return 2;
    length = fread(payload, 1, sizeof(payload), stdin);
    if (ferror(stdin) || fgetc(stdin) != EOF) return 2;
    switch (ordinal) {
    case 1UL: result = lx_budget_create_decode(payload, length, &create); break;
    case 2UL: result = lx_budget_amount_decode(payload, length, &amount); break;
    case 3UL: result = lx_budget_amend_decode(payload, length, &amend); break;
    case 4UL:
    case 5UL: result = lx_budget_delegate_decode(payload, length, &delegate); break;
    case 6UL: result = lx_budget_spend_decode(payload, length, &spend); break;
    case 7UL: result = lx_budget_close_decode(payload, length, &close); break;
    case 8UL: result = lx_budget_defund_decode(payload, length, &defund); break;
    case 9UL: result = lx_budget_revoke_decode(payload, length, &revoke); break;
    case 10UL: result = lx_budget_delegate_spend_decode(payload, length, &delegate_spend); break;
    default: result = lx_budget_pull_decode(payload, length, &pull); break;
    }
    printf("%" PRId32 " %s", result, lxp_result_name(result));
    if (result == LXP_OK) {
        switch (ordinal) {
        case 1UL:
            printf(" %u", (unsigned)create.encoding_version);
            hex(create.budget_id, 32U);
            hex(create.budget_account, 32U);
            hex(create.asset_id, 32U);
            hex(create.purpose_hash, 32U);
            u128(create.per_period_limit);
            u128(create.carry_cap);
            u128(create.amount);
            u64(create.period_length);
            u64(create.period_start);
            u64(create.expiry);
            u64(create.revocation_sequence);
            printf(" %02x", create.rollover_policy);
            hex(create.source_account, 32U);
            u64(create.source_sequence);
            break;
        case 2UL:
            printf(" %u", (unsigned)amount.encoding_version);
            u64(amount.source_sequence);
            hex(amount.budget_id, 32U);
            u128(amount.amount);
            break;
        case 3UL:
            hex(amend.budget_id, 32U);
            u128(amend.per_period_limit);
            u128(amend.carry_cap);
            u64(amend.expiry);
            printf(" %02x", amend.rollover_policy);
            break;
        case 4UL:
        case 5UL:
            hex(delegate.budget_id, 32U);
            hex(delegate.delegate, 32U);
            break;
        case 6UL:
            hex(spend.budget_id, 32U);
            hex(spend.recipient, 32U);
            u128(spend.amount);
            break;
        case 7UL:
            hex(close.budget_id, 32U);
            u64(close.revocation_sequence);
            break;
        case 8UL:
            hex(defund.budget_id, 32U);
            u128(defund.amount);
            break;
        case 9UL:
            hex(revoke.budget_id, 32U);
            u64(revoke.revocation_sequence);
            break;
        case 10UL:
            printf(" %u", (unsigned)delegate_spend.encoding_version);
            hex(delegate_spend.budget_id, 32U);
            hex(delegate_spend.recipient, 32U);
            u128(delegate_spend.amount);
            hex(delegate_spend.idempotency_key, 32U);
            break;
        default:
            printf(" %u", (unsigned)pull.encoding_version);
            hex(pull.budget_id, 32U);
            hex(pull.recipient, 32U);
            u128(pull.amount);
            hex(pull.idempotency_key, 32U);
            grant(&pull.grant);
            break;
        }
    }
    putchar('\n');
    return 0;
}
`

func budgetDecoder(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	source := filepath.Join(dir, "budget_decoder.c")
	if err := os.WriteFile(source, []byte(budgetDecoderSource), 0600); err != nil {
		t.Fatal(err)
	}
	binaryPath := filepath.Join(dir, "budget_decoder")
	args := []string{"-std=c11", "-Wall", "-Wextra", "-Werror", "-ffunction-sections", "-fdata-sections", "-I" + filepath.Join(root, "include"), source}
	for _, path := range []string{
		"src/modules/budget/lx_budget_codec.c",
		"src/ledger/lxp_receive.c",
		"src/protocol/lxp_u128.c",
		"src/protocol/lxp_result.c",
		"src/crypto/lxp_ct.c",
	} {
		args = append(args, filepath.Join(root, path))
	}
	args = append(args, "-Wl,--gc-sections", "-o", binaryPath)
	if output, err := exec.Command("cc", args...).CombinedOutput(); err != nil {
		t.Fatalf("compile real budget decoder: %v\n%s", err, output)
	}
	return binaryPath
}

func budgetDecode(t *testing.T, decoder string, ordinal uint16, payload []byte) string {
	t.Helper()
	cmd := exec.Command(decoder, strconv.FormatUint(uint64(ordinal), 10))
	cmd.Stdin = bytes.NewReader(payload)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("run real budget decoder: %v\n%s", err, output)
	}
	return strings.TrimSuffix(string(output), "\n")
}

func budgetID(marker byte) [32]byte {
	var id [32]byte
	for i := range id {
		id[i] = marker + byte(i)
	}
	return id
}

func budgetHex32(id [32]byte) string { return fmt.Sprintf("%x", id[:]) }

func budgetHex128(v codec.U128) string { return fmt.Sprintf("%016x%016x", v.Hi, v.Lo) }

func budgetHex64(v uint64) string { return fmt.Sprintf("%016x", v) }

type budgetGrant struct {
	grantID, from, recipient, asset [32]byte
	perDrawMaximum, allowance       codec.U128
	recurring                       byte
	windowLength, expiration        uint64
	purposeHash                     [32]byte
	hasReference                    byte
	referenceHash                   [32]byte
	revocationSequence              uint64
	publicKey                       [32]byte
	signature                       [64]byte
}

func (g budgetGrant) bytes() []byte {
	out := make([]byte, 0, PayerGrantBytes)
	perDraw, allowance := g.perDrawMaximum.Bytes(), g.allowance.Bytes()
	out = append(out, g.grantID[:]...)
	out = append(out, g.from[:]...)
	out = append(out, g.recipient[:]...)
	out = append(out, g.asset[:]...)
	out = append(out, perDraw[:]...)
	out = append(out, allowance[:]...)
	out = append(out, g.recurring)
	out = append(out, binary.BigEndian.AppendUint64(nil, g.windowLength)...)
	out = append(out, binary.BigEndian.AppendUint64(nil, g.expiration)...)
	out = append(out, g.purposeHash[:]...)
	out = append(out, g.hasReference)
	out = append(out, g.referenceHash[:]...)
	out = append(out, binary.BigEndian.AppendUint64(nil, g.revocationSequence)...)
	out = append(out, g.publicKey[:]...)
	return append(out, g.signature[:]...)
}

func (g budgetGrant) fields() string {
	return strings.Join([]string{
		budgetHex32(g.grantID), budgetHex32(g.from), budgetHex32(g.recipient), budgetHex32(g.asset),
		budgetHex128(g.perDrawMaximum), budgetHex128(g.allowance), strconv.Itoa(int(g.recurring)),
		budgetHex64(g.windowLength), budgetHex64(g.expiration), budgetHex32(g.purposeHash),
		strconv.Itoa(int(g.hasReference)), budgetHex32(g.referenceHash), budgetHex64(g.revocationSequence),
		budgetHex32(g.publicKey), fmt.Sprintf("%x", g.signature[:]),
	}, " ")
}

type budgetEdit struct {
	offset int
	value  []byte
}

func budgetMutate(encode func() ([]byte, error), edits ...budgetEdit) func(*testing.T) []byte {
	return func(t *testing.T) []byte {
		t.Helper()
		payload, err := encode()
		if err != nil {
			t.Fatal(err)
		}
		for _, edit := range edits {
			copy(payload[edit.offset:], edit.value)
		}
		return payload
	}
}

func TestBudgetPayloads(t *testing.T) {
	decoder := budgetDecoder(t)

	t.Run("decoder_lengths", func(t *testing.T) {
		output, err := exec.Command(decoder, "lengths").CombinedOutput()
		if err != nil {
			t.Fatalf("read decoder lengths: %v\n%s", err, output)
		}
		want := fmt.Sprintf("%d %d %d %d %d %d %d %d %d %d %d %d\n",
			BudgetCreatePayloadBytes, BudgetCreateNativePayloadBytes, BudgetFundPayloadBytes, BudgetFundNativePayloadBytes,
			BudgetAmendPayloadBytes, BudgetDelegatePayloadBytes, BudgetSpendPayloadBytes, BudgetClosePayloadBytes,
			BudgetDefundPayloadBytes, BudgetRevokePayloadBytes, BudgetDelegateSpendPayloadBytes, BudgetPullPayloadBytes)
		if want != "211 251 50 58 75 66 82 42 50 42 114 460\n" || string(output) != want {
			t.Fatalf("decoder lengths = %q, Go lengths = %q", output, want)
		}
	})

	id, account, asset, purpose := budgetID(0x10), budgetID(0x40), budgetID(0x70), budgetID(0xa0)
	recipient, key, source, delegate := budgetID(0x30), budgetID(0x60), budgetID(0x90), budgetID(0xc0)
	limit := codec.U128{Hi: 0x0102030405060708, Lo: 0x090a0b0c0d0e0f10}
	carry := codec.U128{Hi: 0x1112131415161718, Lo: 0x191a1b1c1d1e1f20}
	amount := codec.U128{Hi: 0x2122232425262728, Lo: 0x292a2b2c2d2e2f30}
	const periodLength, periodStart, expiry, revocation uint64 = 0x3132333435363738, 0x4142434445464748, 0x5152535455565758, 0x6162636465666768
	const sourceSequence uint64 = math.MaxUint64 - 1
	grant := budgetGrant{
		grantID: budgetID(0x01), from: budgetID(0x21), recipient: recipient, asset: asset,
		perDrawMaximum: codec.U128{Hi: 0x7172737475767778, Lo: 0x797a7b7c7d7e7f80},
		allowance:      codec.U128{Hi: 0x8182838485868788, Lo: 0x898a8b8c8d8e8f90},
		recurring:      1, windowLength: 0x9192939495969798, expiration: 0xa1a2a3a4a5a6a7a8,
		purposeHash: purpose, hasReference: 1, referenceHash: budgetID(0x51),
		revocationSequence: 0xb1b2b3b4b5b6b7b8, publicKey: budgetID(0xd1),
	}
	for i := range grant.signature {
		grant.signature[i] = byte(0xe0 + i)
	}
	grantBytes := grant.bytes()
	if len(grantBytes) != PayerGrantBytes {
		t.Fatalf("grant length = %d, want %d", len(grantBytes), PayerGrantBytes)
	}

	h := budgetHex32
	createFields := strings.Join([]string{h(id), h(account), h(asset), h(purpose), budgetHex128(limit), budgetHex128(carry), budgetHex128(amount),
		budgetHex64(periodLength), budgetHex64(periodStart), budgetHex64(expiry), budgetHex64(revocation), "02"}, " ")
	spendFields := h(id) + " " + h(recipient) + " " + budgetHex128(amount)

	type vector struct {
		name    string
		ordinal uint16
		size    int
		encode  func() ([]byte, error)
		fields  string
	}
	vectors := []vector{
		{"create", 1, 211, func() ([]byte, error) {
			return EncodeBudgetCreate(id, account, asset, purpose, limit, carry, amount, periodLength, periodStart, expiry, revocation, 2)
		}, "1 " + createFields + " " + h([32]byte{}) + " " + budgetHex64(0)},
		{"create_native", 1, 251, func() ([]byte, error) {
			return EncodeBudgetCreateNative(id, account, asset, purpose, limit, carry, amount, periodLength, periodStart, expiry, revocation, 2, source, sourceSequence)
		}, "2 " + createFields + " " + h(source) + " " + budgetHex64(sourceSequence)},
		{"fund", 2, 50, func() ([]byte, error) { return EncodeBudgetFund(id, amount) },
			"1 " + budgetHex64(0) + " " + h(id) + " " + budgetHex128(amount)},
		{"fund_native", 2, 58, func() ([]byte, error) { return EncodeBudgetFundNative(id, amount, sourceSequence) },
			"2 " + budgetHex64(sourceSequence) + " " + h(id) + " " + budgetHex128(amount)},
		{"amend", 3, 75, func() ([]byte, error) { return EncodeBudgetAmend(id, limit, carry, expiry, 1) },
			h(id) + " " + budgetHex128(limit) + " " + budgetHex128(carry) + " " + budgetHex64(expiry) + " 01"},
		{"delegate_add", 4, 66, func() ([]byte, error) { return EncodeBudgetDelegate(id, delegate) }, h(id) + " " + h(delegate)},
		{"delegate_remove", 5, 66, func() ([]byte, error) { return EncodeBudgetDelegate(id, delegate) }, h(id) + " " + h(delegate)},
		{"spend", 6, 82, func() ([]byte, error) { return EncodeBudgetSpend(id, recipient, amount) }, spendFields},
		{"close", 7, 42, func() ([]byte, error) { return EncodeBudgetClose(id, revocation) }, h(id) + " " + budgetHex64(revocation)},
		{"defund", 8, 50, func() ([]byte, error) { return EncodeBudgetDefund(id, amount) }, h(id) + " " + budgetHex128(amount)},
		{"revoke", 9, 42, func() ([]byte, error) { return EncodeBudgetRevoke(id, math.MaxUint64) }, h(id) + " " + budgetHex64(math.MaxUint64)},
		{"delegate_spend", 10, 114, func() ([]byte, error) { return EncodeBudgetDelegateSpend(id, recipient, amount, key) },
			"1 " + spendFields + " " + h(key)},
		{"pull", 11, 460, func() ([]byte, error) { return EncodeBudgetPull(id, recipient, amount, key, grantBytes) },
			"1 " + spendFields + " " + h(key) + " " + grant.fields()},
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
			if got, want := budgetDecode(t, decoder, v.ordinal, payload), "0 LXP_OK "+v.fields; got != want {
				t.Fatalf("decoded fields\n got %q\nwant %q", got, want)
			}
			const refused = "-3 LXP_ERR_NON_CANONICAL"
			if got := budgetDecode(t, decoder, v.ordinal, payload[:len(payload)-1]); got != refused {
				t.Fatalf("truncated payload: got %q, want %q", got, refused)
			}
			if got := budgetDecode(t, decoder, v.ordinal, append(bytes.Clone(payload), 0)); got != refused {
				t.Fatalf("extended payload: got %q, want %q", got, refused)
			}
			for _, version := range [][2]byte{{1, payload[1]}, {0, 0}, {0, 3}} {
				invalid := bytes.Clone(payload)
				copy(invalid, version[:])
				if got := budgetDecode(t, decoder, v.ordinal, invalid); got != refused {
					t.Fatalf("header %x: got %q, want %q", version, got, refused)
				}
			}
		})
	}

	t.Run("version_crossover", func(t *testing.T) {
		for _, c := range []struct {
			ordinal uint16
			encode  func() ([]byte, error)
			version byte
		}{
			{1, func() ([]byte, error) {
				return EncodeBudgetCreate(id, account, asset, purpose, limit, carry, amount, periodLength, periodStart, expiry, revocation, 2)
			}, 2},
			{1, func() ([]byte, error) {
				return EncodeBudgetCreateNative(id, account, asset, purpose, limit, carry, amount, periodLength, periodStart, expiry, revocation, 2, source, sourceSequence)
			}, 1},
			{2, func() ([]byte, error) { return EncodeBudgetFund(id, amount) }, 2},
			{2, func() ([]byte, error) { return EncodeBudgetFundNative(id, amount, sourceSequence) }, 1},
		} {
			payload, err := c.encode()
			if err != nil {
				t.Fatal(err)
			}
			payload[1] = c.version
			if got := budgetDecode(t, decoder, c.ordinal, payload); got != "-3 LXP_ERR_NON_CANONICAL" {
				t.Fatalf("length %d under version %d: got %q", len(payload), c.version, got)
			}
		}
	})

	t.Run("refusals", func(t *testing.T) {
		zero := [32]byte{}
		badGrant := func(offset int, value byte) []byte {
			g := bytes.Clone(grantBytes)
			g[offset] = value
			return g
		}
		type refusal struct {
			name    string
			ordinal uint16
			encode  func() ([]byte, error)
			raw     func(*testing.T) []byte
			err     error
			decoded string
		}
		create := func() ([]byte, error) {
			return EncodeBudgetCreate(id, account, asset, purpose, limit, carry, amount, periodLength, periodStart, expiry, revocation, 2)
		}
		createNative := func() ([]byte, error) {
			return EncodeBudgetCreateNative(id, account, asset, purpose, limit, carry, amount, periodLength, periodStart, expiry, revocation, 2, source, sourceSequence)
		}
		fundNative := func() ([]byte, error) { return EncodeBudgetFundNative(id, amount, sourceSequence) }
		spend := func() ([]byte, error) { return EncodeBudgetDelegateSpend(id, recipient, amount, key) }
		pull := func() ([]byte, error) { return EncodeBudgetPull(id, recipient, amount, key, grantBytes) }
		zero16 := make([]byte, 16)
		max64 := binary.BigEndian.AppendUint64(nil, math.MaxUint64)
		const nonCanonical, invalidAmount = "-3 LXP_ERR_NON_CANONICAL", "-412 LXP_ERR_INVALID_AMOUNT"
		refusals := []refusal{
			{"create_zero_id", 1, func() ([]byte, error) {
				return EncodeBudgetCreate(zero, account, asset, purpose, limit, carry, amount, periodLength, periodStart, expiry, revocation, 2)
			}, budgetMutate(create, budgetEdit{2, zero[:]}), errBudgetNonCanonical, nonCanonical},
			{"create_zero_account", 1, func() ([]byte, error) {
				return EncodeBudgetCreate(id, zero, asset, purpose, limit, carry, amount, periodLength, periodStart, expiry, revocation, 2)
			}, budgetMutate(create, budgetEdit{34, zero[:]}), errBudgetNonCanonical, nonCanonical},
			{"create_zero_asset", 1, func() ([]byte, error) {
				return EncodeBudgetCreate(id, account, zero, purpose, limit, carry, amount, periodLength, periodStart, expiry, revocation, 2)
			}, budgetMutate(create, budgetEdit{66, zero[:]}), errBudgetNonCanonical, nonCanonical},
			{"create_zero_limit", 1, func() ([]byte, error) {
				return EncodeBudgetCreate(id, account, asset, purpose, codec.U128{}, carry, amount, periodLength, periodStart, expiry, revocation, 2)
			}, budgetMutate(create, budgetEdit{130, zero16}), errBudgetInvalidAmount, invalidAmount},
			{"create_zero_amount", 1, func() ([]byte, error) {
				return EncodeBudgetCreate(id, account, asset, purpose, limit, carry, codec.U128{}, periodLength, periodStart, expiry, revocation, 2)
			}, budgetMutate(create, budgetEdit{162, zero16}), errBudgetInvalidAmount, invalidAmount},
			{"create_native_zero_source", 1, func() ([]byte, error) {
				return EncodeBudgetCreateNative(id, account, asset, purpose, limit, carry, codec.U128{}, periodLength, periodStart, expiry, revocation, 2, zero, sourceSequence)
			}, budgetMutate(createNative, budgetEdit{162, zero16}, budgetEdit{211, zero[:]}), errBudgetNonCanonical, nonCanonical},
			{"create_native_sequence_exhausted", 1, func() ([]byte, error) {
				return EncodeBudgetCreateNative(id, account, asset, purpose, limit, carry, amount, periodLength, periodStart, expiry, revocation, 2, source, math.MaxUint64)
			}, budgetMutate(createNative, budgetEdit{243, max64}), errBudgetNonCanonical, nonCanonical},
			{"fund_zero_id", 2, func() ([]byte, error) { return EncodeBudgetFund(zero, amount) },
				budgetMutate(func() ([]byte, error) { return EncodeBudgetFund(id, amount) }, budgetEdit{2, zero[:]}), errBudgetNonCanonical, nonCanonical},
			{"fund_zero_amount", 2, func() ([]byte, error) { return EncodeBudgetFund(id, codec.U128{}) },
				budgetMutate(func() ([]byte, error) { return EncodeBudgetFund(id, amount) }, budgetEdit{34, zero16}), errBudgetInvalidAmount, invalidAmount},
			{"fund_native_sequence_exhausted", 2, func() ([]byte, error) { return EncodeBudgetFundNative(zero, codec.U128{}, math.MaxUint64) },
				budgetMutate(fundNative, budgetEdit{2, zero[:]}, budgetEdit{34, zero16}, budgetEdit{50, max64}), errBudgetNonCanonical, nonCanonical},
			{"amend_zero_limit", 3, func() ([]byte, error) { return EncodeBudgetAmend(id, codec.U128{}, carry, expiry, 1) },
				budgetMutate(func() ([]byte, error) { return EncodeBudgetAmend(id, limit, carry, expiry, 1) }, budgetEdit{34, zero16}), errBudgetInvalidAmount, invalidAmount},
			{"delegate_zero_delegate", 4, func() ([]byte, error) { return EncodeBudgetDelegate(id, zero) },
				budgetMutate(func() ([]byte, error) { return EncodeBudgetDelegate(id, delegate) }, budgetEdit{34, zero[:]}), errBudgetNonCanonical, nonCanonical},
			{"spend_zero_recipient", 6, func() ([]byte, error) { return EncodeBudgetSpend(id, zero, amount) },
				budgetMutate(func() ([]byte, error) { return EncodeBudgetSpend(id, recipient, amount) }, budgetEdit{34, zero[:]}), errBudgetNonCanonical, nonCanonical},
			{"spend_zero_amount", 6, func() ([]byte, error) { return EncodeBudgetSpend(id, recipient, codec.U128{}) },
				budgetMutate(func() ([]byte, error) { return EncodeBudgetSpend(id, recipient, amount) }, budgetEdit{66, zero16}), errBudgetInvalidAmount, invalidAmount},
			{"close_zero_sequence", 7, func() ([]byte, error) { return EncodeBudgetClose(id, 0) },
				budgetMutate(func() ([]byte, error) { return EncodeBudgetClose(id, revocation) }, budgetEdit{34, binary.BigEndian.AppendUint64(nil, 0)}), errBudgetNonCanonical, nonCanonical},
			{"defund_zero_amount", 8, func() ([]byte, error) { return EncodeBudgetDefund(id, codec.U128{}) },
				budgetMutate(func() ([]byte, error) { return EncodeBudgetDefund(id, amount) }, budgetEdit{34, zero16}), errBudgetInvalidAmount, invalidAmount},
			{"revoke_zero_id", 9, func() ([]byte, error) { return EncodeBudgetRevoke(zero, revocation) },
				budgetMutate(func() ([]byte, error) { return EncodeBudgetRevoke(id, revocation) }, budgetEdit{2, zero[:]}), errBudgetNonCanonical, nonCanonical},
			{"delegate_spend_zero_key", 10, func() ([]byte, error) { return EncodeBudgetDelegateSpend(id, recipient, amount, zero) },
				budgetMutate(spend, budgetEdit{82, zero[:]}), errBudgetNonCanonical, nonCanonical},
			{"delegate_spend_zero_amount", 10, func() ([]byte, error) { return EncodeBudgetDelegateSpend(id, recipient, codec.U128{}, key) },
				budgetMutate(spend, budgetEdit{66, zero16}), errBudgetInvalidAmount, invalidAmount},
			{"pull_zero_amount", 11, func() ([]byte, error) { return EncodeBudgetPull(id, recipient, codec.U128{}, key, grantBytes) },
				budgetMutate(pull, budgetEdit{66, zero16}), errBudgetInvalidAmount, invalidAmount},
			{"pull_recurring_flag", 11, func() ([]byte, error) { return EncodeBudgetPull(id, recipient, amount, key, badGrant(160, 2)) },
				budgetMutate(pull, budgetEdit{BudgetDelegateSpendPayloadBytes + 160, []byte{2}}), errBudgetNonCanonical, nonCanonical},
			{"pull_reference_flag", 11, func() ([]byte, error) { return EncodeBudgetPull(id, recipient, amount, key, badGrant(209, 2)) },
				budgetMutate(pull, budgetEdit{BudgetDelegateSpendPayloadBytes + 209, []byte{2}}), errBudgetNonCanonical, nonCanonical},
			{"pull_short_grant", 11, func() ([]byte, error) {
				return EncodeBudgetPull(id, recipient, amount, key, grantBytes[:PayerGrantBytes-1])
			},
				func(t *testing.T) []byte { return budgetMutate(pull)(t)[:BudgetPullPayloadBytes-1] }, errBudgetNonCanonical, nonCanonical},
		}
		for _, r := range refusals {
			t.Run(r.name, func(t *testing.T) {
				payload, err := r.encode()
				if !errors.Is(err, r.err) || payload != nil {
					t.Fatalf("encode = %x, %v; want nil, %v", payload, err, r.err)
				}
				if got := budgetDecode(t, decoder, r.ordinal, r.raw(t)); got != r.decoded {
					t.Fatalf("decoder: got %q, want %q", got, r.decoded)
				}
			})
		}
	})
}
