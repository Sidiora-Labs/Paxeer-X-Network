#!/usr/bin/env python3
"""Exchange time-in-force qualification: keeper ingress, Rust router and
canonical payload, and the native perps matcher, all through production code.

Prerequisites are prebuilt: build/liblayerx.a and the programs runtime
library (make), plus go and cargo toolchains. Nothing is written into the
source tree; the C driver is compiled into a private temporary directory.
"""
import os
import shlex
import subprocess
import sys
import tempfile
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
BUILD = Path(os.environ.get("BUILD_DIR", ROOT / "build"))
COMMAND = "timeout 1800s python3 tests/qualification/paxeer_x_exchange_tif.py"

DRIVER = r'''
#define main lx_perps_book_fixture_main
#include "tests/modules/test_perps_book.c"
#undef main

#include <stdio.h>

static int failures;
static int checks;

#define CHECK(cond) do { ++checks; if (!(cond)) { ++failures; \
    fprintf(stderr, "FAIL %s:%d %s\n", __FILE__, __LINE__, #cond); } } while (0)

static lxp_result run_at(uint64_t sequence, const char *did,
                         const uint8_t *payload, size_t length)
{
    const lxp_module_registration *registration = NULL;
    lxp_activity activity;
    lxp_authority_resolved authority;
    lxp_result module_result = LXP_OK;
    lxp_result status;
    size_t did_length = strlen(did);
    if (lxp_kernel_module_for_activity(&kernel, LX_PERPS_ORDER_PLACE, 0U,
                                       &registration) != LXP_OK ||
        lxp_effect_buffer_init(&effects) != LXP_OK || ctx_open() != 0 ||
        lxp_module_ctx_bind_effects(&ctx, &effects) != LXP_OK)
        return LXP_FATAL_INVARIANT;
    (void)memset(&activity, 0, sizeof(activity));
    activity.protocol_version = LXP_PROTOCOL_VERSION_OCCUPANCY;
    activity.network_id = 7U;
    activity.activity_type = LX_PERPS_ORDER_PLACE;
    activity.actor_did = (lxp_byte_span){ (const uint8_t *)did, did_length };
    activity.account_sequence = sequence;
    activity.timestamp_bound.not_after = batch_timestamp + 1000U;
    activity.idempotency_key[0] = ctx.activity_id[0];
    activity.payload = (lxp_byte_span){ payload, length };
    (void)memset(&authority, 0, sizeof(authority));
    authority.kind = LXP_AUTHORITY_OWNER;
    if (lxp_did_id_derive((const uint8_t *)did, did_length,
                          authority.actor) != LXP_OK)
        return LXP_FATAL_INVARIANT;
    (void)memcpy(authority.principal, authority.actor, 32U);
    status = lxp_kernel_dispatch(registration, &ctx, &activity, &authority,
                                 &effects, &module_result);
    if (status != LXP_OK || module_result != LXP_OK) {
        lxp_module_ctx_rollback(&ctx);
        return status != LXP_OK ? status : module_result;
    }
    if (lxp_module_ctx_commit(&ctx) != LXP_OK) return LXP_FATAL_INVARIANT;
    ++global_sequence;
    return LXP_OK;
}

static size_t encode(uint8_t order_id, const lx_account *owner,
                     lx_perps_side side, uint64_t price, uint64_t quantity,
                     lx_perps_time_in_force tif, uint8_t out[64 * 3])
{
    lx_perps_order_command command;
    size_t length = 0U;
    (void)memset(&command, 0, sizeof(command));
    command.market_id[0] = 1U;
    command.order_id[0] = order_id;
    (void)memcpy(command.owner_account_id, owner->id, 32U);
    command.side = side;
    command.price = (lxp_u128){ 0U, price };
    command.quantity = (lxp_u128){ 0U, quantity };
    command.time_in_force = tif;
    if (lx_perps_order_command_encode_versioned(&command, out, 64U * 3U,
                                                &length) != LXP_OK)
        return 0U;
    return length;
}

static lxp_result place(uint8_t order_id, const lx_account *owner,
                        lx_perps_side side, uint64_t price, uint64_t quantity,
                        lx_perps_time_in_force tif, const char *did)
{
    uint8_t payload[64 * 3];
    size_t length = encode(order_id, owner, side, price, quantity, tif,
                           payload);
    if (length == 0U) return LXP_FATAL_INVARIANT;
    return run_at(main_sequence(did), did, payload, length);
}

static uint64_t order_remaining(uint8_t order_id)
{
    lx_perps_order order;
    if (ctx_open() != 0 || order_state(order_id, &order) != LXP_OK)
        return UINT64_MAX;
    return order.remaining.lo;
}

static int absent(uint8_t order_id)
{
    lx_perps_order order;
    return ctx_open() == 0 &&
           order_state(order_id, &order) == LXP_ERR_UNKNOWN_FIELD;
}

/* An IOC/FOK identifier is spent: recorded inactive with nothing left. */
static int spent(uint8_t order_id)
{
    lx_perps_order order;
    return ctx_open() == 0 && order_state(order_id, &order) == LXP_OK &&
           !order.active && lxp_u128_is_zero(order.remaining);
}

static size_t book_count(void)
{
    lx_perps_book book;
    uint8_t market_id[32];
    identifier(1U, market_id);
    if (ctx_open() != 0 || lx_perps_book_init(&book) != LXP_OK ||
        lx_perps_order_book_load(&ctx, market_id, &book) != LXP_OK)
        return SIZE_MAX;
    return book.count;
}

static void codec_cases(void)
{
    lx_perps_order_command command;
    uint8_t legacy[LX_PERPS_ORDER_PAYLOAD_BYTES];
    uint8_t bytes[64 * 3];
    size_t length;
    lx_perps_time_in_force tif;
    length = encode(9U, alice_margin_a, LX_PERPS_SIDE_BUY, 100U, 1U,
                    LX_PERPS_TIF_GOOD_TILL_CANCELLED, bytes);
    CHECK(length == LX_PERPS_ORDER_PAYLOAD_BYTES);
    CHECK(lx_perps_order_command_decode(bytes, length, &command) == LXP_OK);
    CHECK(command.time_in_force == LX_PERPS_TIF_GOOD_TILL_CANCELLED);
    CHECK(lx_perps_order_command_encode(&command, legacy) == LXP_OK);
    CHECK(memcmp(legacy, bytes, sizeof(legacy)) == 0);
    for (tif = LX_PERPS_TIF_IMMEDIATE_OR_CANCEL; tif <= LX_PERPS_TIF_POST_ONLY;
         tif = (lx_perps_time_in_force)(tif + 1)) {
        length = encode(9U, alice_margin_a, LX_PERPS_SIDE_BUY, 100U, 1U, tif,
                        bytes);
        CHECK(length == LX_PERPS_ORDER_PAYLOAD_TIF_BYTES);
        CHECK(bytes[LX_PERPS_ORDER_PAYLOAD_BYTES] == (uint8_t)tif);
        CHECK(memcmp(legacy, bytes, sizeof(legacy)) == 0);
        CHECK(lx_perps_order_command_decode(bytes, length, &command) ==
              LXP_OK);
        CHECK(command.time_in_force == tif);
        CHECK(lx_perps_order_command_encode(&command, legacy) ==
              LXP_ERR_NON_CANONICAL);
        (void)memcpy(legacy, bytes, sizeof(legacy));
    }
    bytes[LX_PERPS_ORDER_PAYLOAD_BYTES] = 0U;
    CHECK(lx_perps_order_command_decode(bytes, 130U, &command) ==
          LXP_ERR_NON_CANONICAL);
    bytes[LX_PERPS_ORDER_PAYLOAD_BYTES] = 4U;
    CHECK(lx_perps_order_command_decode(bytes, 130U, &command) ==
          LXP_ERR_NON_CANONICAL);
    CHECK(lx_perps_order_command_decode(bytes, 131U, &command) ==
          LXP_ERR_NON_CANONICAL);
}

int main(void)
{
    lx_perps_market market;
    uint8_t payload[64 * 3];
    size_t length;
    uint64_t sequence;
    if (fixture_init() != 0 || kernel_start() != 0 ||
        account_add("agent:did:key:alice:main", LX_ACCOUNT_OPEN_CREDIT, 0U,
                    &alice_main) != 0 ||
        account_add("agent:did:key:alice:margin:a", LX_ACCOUNT_OPEN_CREDIT,
                    1000U, &alice_margin_a) != 0 ||
        account_add("agent:did:key:alice:margin:b", LX_ACCOUNT_OPEN_CREDIT,
                    1000U, &alice_margin_b) != 0 ||
        account_add("agent:did:key:bob:main", LX_ACCOUNT_OPEN_CREDIT, 0U,
                    &bob_main) != 0 ||
        account_add("agent:did:key:bob:margin:a", LX_ACCOUNT_OPEN_CREDIT,
                    1000U, &bob_margin_a) != 0)
        return 2;
    market_defaults(1U, &market);
    if (market_create(&market, admin_did) != LXP_OK ||
        oracle_push(oracle_seed, 1U, 1U, 100U, 1000U) != LXP_OK)
        return 2;
    codec_cases();

    /* Malformed TIF is refused before the account sequence advances. */
    sequence = main_sequence("did:key:bob");
    length = encode(20U, bob_margin_a, LX_PERPS_SIDE_BUY, 100U, 1U,
                    LX_PERPS_TIF_IMMEDIATE_OR_CANCEL, payload);
    payload[LX_PERPS_ORDER_PAYLOAD_BYTES] = 7U;
    CHECK(length == 130U);
    CHECK(run_at(sequence, "did:key:bob", payload, length) != LXP_OK);
    CHECK(main_sequence("did:key:bob") == sequence);
    CHECK(absent(20U));

    /* GTC rests under price-time priority. */
    CHECK(place(10U, alice_margin_b, LX_PERPS_SIDE_SELL, 100U, 3U,
                LX_PERPS_TIF_GOOD_TILL_CANCELLED, "did:key:alice") == LXP_OK);
    CHECK(order_remaining(10U) == 3U);

    /* Post-only refuses a crossing order and changes nothing. */
    sequence = main_sequence("did:key:bob");
    CHECK(place(11U, bob_margin_a, LX_PERPS_SIDE_BUY, 100U, 1U,
                LX_PERPS_TIF_POST_ONLY, "did:key:bob") ==
          LXP_ERR_AGREEMENT_STATE);
    CHECK(absent(11U) && order_remaining(10U) == 3U);
    CHECK(main_sequence("did:key:bob") == sequence);
    CHECK(balance_is(bob_margin_a, 1000U) && balance_is(alice_margin_b, 1000U));
    /* A non-crossing post-only order rests. */
    CHECK(place(12U, bob_margin_a, LX_PERPS_SIDE_BUY, 99U, 1U,
                LX_PERPS_TIF_POST_ONLY, "did:key:bob") == LXP_OK);
    CHECK(order_remaining(12U) == 1U);

    /* FOK larger than available liquidity changes no trade state. */
    CHECK(place(13U, bob_margin_a, LX_PERPS_SIDE_BUY, 100U, 5U,
                LX_PERPS_TIF_FILL_OR_KILL, "did:key:bob") ==
          LXP_ERR_AGREEMENT_STATE);
    CHECK(absent(13U) && order_remaining(10U) == 3U && book_count() == 2U);
    CHECK(balance_is(bob_margin_a, 1000U) && balance_is(alice_margin_b, 1000U));
    {
        lx_perps_position position;
        CHECK(position_state(bob_margin_a, &position) != LXP_OK);
    }
    /* FOK within liquidity fills completely and never rests. */
    CHECK(place(13U, bob_margin_a, LX_PERPS_SIDE_BUY, 100U, 2U,
                LX_PERPS_TIF_FILL_OR_KILL, "did:key:bob") == LXP_OK);
    CHECK(spent(13U) && order_remaining(10U) == 1U);
    CHECK(position_is(bob_margin_a, bob_main, LX_PERPS_SIDE_BUY, 2U, 200U,
                      true));

    /* IOC takes what is available and cancels the residual. */
    sequence = main_sequence("did:key:bob");
    length = encode(14U, bob_margin_a, LX_PERPS_SIDE_BUY, 100U, 4U,
                    LX_PERPS_TIF_IMMEDIATE_OR_CANCEL, payload);
    CHECK(run_at(sequence, "did:key:bob", payload, length) == LXP_OK);
    CHECK(spent(14U) && absent(10U) && book_count() == 1U);
    CHECK(position_is(bob_margin_a, bob_main, LX_PERPS_SIDE_BUY, 3U, 300U,
                      true));
    CHECK(open_interest_is(300U, 300U));
    /* Retrying the committed activity is refused and duplicates nothing. */
    CHECK(run_at(sequence, "did:key:bob", payload, length) ==
          LXP_ERR_SEQUENCE_REUSED);
    CHECK(position_is(bob_margin_a, bob_main, LX_PERPS_SIDE_BUY, 3U, 300U,
                      true));
    CHECK(open_interest_is(300U, 300U) && book_count() == 1U);

    /* An IOC that crosses nothing leaves the book untouched. */
    CHECK(place(15U, bob_margin_a, LX_PERPS_SIDE_BUY, 100U, 1U,
                LX_PERPS_TIF_IMMEDIATE_OR_CANCEL, "did:key:bob") == LXP_OK);
    CHECK(spent(15U) && book_count() == 1U);

    if (lxp_state_store_destroy(&store) != LXP_OK) ++failures;
    printf("native checks=%d failures=%d\n", checks, failures);
    return failures == 0 ? 0 : 1;
}
'''


def run(step, argv, cwd, env=None, log=None):
    print(f"== {step}: {shlex.join(argv)}", flush=True)
    result = subprocess.run(argv, cwd=cwd, env=env, stdout=subprocess.PIPE,
                            stderr=subprocess.STDOUT, text=True)
    sys.stdout.write(result.stdout)
    if log is not None:
        log.write(f"== {step} exit={result.returncode}\n{result.stdout}\n")
    print(f"== {step} exit={result.returncode}", flush=True)
    return result.returncode


def make_var(name):
    out = subprocess.run(["make", "-s", "--no-print-directory", "-f", "Makefile", "-f", "-",
                          "print-var"], cwd=ROOT, input=f"print-var:\n\t@echo $({name})\n",
                         stdout=subprocess.PIPE, stderr=subprocess.DEVNULL, text=True)
    return out.stdout.strip()


def main():
    revision = subprocess.run(["git", "rev-parse", "HEAD"], cwd=ROOT, stdout=subprocess.PIPE,
                              text=True).stdout.strip()
    evidence = os.environ.get("PAXEER_X_EVIDENCE_DIR")
    missing = []
    library = BUILD / "liblayerx.a"
    runtime = make_var("PROGRAMS_RUNTIME_LIB")
    for path in (library, ROOT / runtime if runtime else None):
        if path is None or not Path(path).is_file():
            missing.append(str(path))
    for tool in ("go", "cargo", os.environ.get("CC", "cc")):
        if subprocess.run(["sh", "-c", f"command -v {shlex.quote(tool)}"],
                          stdout=subprocess.DEVNULL).returncode != 0:
            missing.append(tool)
    if not evidence or not Path(evidence).is_dir():
        missing.append("PAXEER_X_EVIDENCE_DIR")
    if missing:
        print(f"missing prerequisite: {', '.join(missing)}", file=sys.stderr)
        print(f"revision={revision} command={COMMAND!r} exit=1 evidence={evidence}")
        return 1

    failures = 0
    tests = 0
    log_path = Path(evidence) / "paxeer_x_exchange_tif.log"
    with open(log_path, "w", encoding="utf-8") as log, \
            tempfile.TemporaryDirectory(prefix="tif-") as scratch:
        os.chmod(log_path, 0o600)
        log.write(f"revision={revision}\n")
        driver = Path(scratch) / "tif_driver.c"
        driver.write_text(DRIVER, encoding="utf-8")
        binary = Path(scratch) / "tif_driver"
        cflags = shlex.split(make_var("CPPFLAGS")) + shlex.split(make_var("CFLAGS"))
        ldflags = shlex.split(make_var("EXTRA_LDFLAGS"))
        cc = [os.environ.get("CC", "cc")] + cflags + ["-I", str(ROOT), str(driver), str(library),
                                                     str(ROOT / runtime), str(library)] + ldflags + \
            ["-lcrypto", "-pthread", "-ldl", "-lm", "-o", str(binary)]
        steps = [
            ("native-compile", cc, ROOT),
            ("native-matcher", [str(binary)], ROOT),
            ("go-keeper", ["go", "test", "-count=1", "./modules/layerxexchange/keeper/", "-run",
                           "TestOrderTimeInForceIsRecordedDistinctlyBeforeAnyNonce|"
                           "TestIntentsAreRecordedWithoutMovingFunds"], ROOT),
            ("rust-payload", ["cargo", "test", "--locked", "-p", "layerx-types", "--test",
                              "trading_payload"], ROOT / "agent"),
            ("rust-router", ["cargo", "test", "--locked", "-p", "layerx-intents", "--test",
                             "precompile_router"], ROOT / "human"),
        ]
        for step, argv, cwd in steps:
            tests += 1
            code = run(step, argv, cwd, log=log)
            if code != 0:
                failures += 1
                if step == "native-compile":
                    tests += 1
                    failures += 1
                    break
        status = 0 if failures == 0 else 1
        log.write(f"tests={tests} failures={failures} exit={status}\n")
    print(f"revision={revision} command={COMMAND!r} exit={status} evidence={log_path}")
    print(f"PAXEER_X_GATE tests={tests} skipped=0")
    return status


if __name__ == "__main__":
    sys.exit(main())
