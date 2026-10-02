#!/usr/bin/env python3
"""Build explicitly, then qualify prebuilt TIF components and real daemon processes."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
import time

ROOT = Path(__file__).resolve().parents[2]
COMMAND = 'timeout 1800s python3 tests/qualification/paxeer_x_exchange_tif.py'
COMPONENTS = ['go-precompile', 'go-keeper', 'rust-payload', 'rust-router', 'native-matcher', 'genesis-module-table']
SOURCES = ['tests/qualification/paxeer_x_exchange_tif.py', 'tests/daemon/lxp_test_exchange_tif_client.c',
           'tests/daemon/lxp_test_oracle_transport_client.c', 'tests/daemon/paxeer_x_runtime_fixture.py',
           'tests/modules/test_perps_book.c', 'modules/layerxexchange/keeper/keeper_test.go',
           'agent/crates/layerx-types/tests/trading_payload.rs', 'human/crates/layerx-intents/tests/precompile_router.rs',
           'precompiles/layerxexchange/layerxexchange_test.go', 'precompiles/layerxexchange/abi.json',
           'api/layerxexchange/params.proto', 'modules/layerxexchange/types/params.go',
           'modules/layerxexchange/types/params.pb.go', 'modules/layerxexchange/types/state.go',
           'modules/layerxexchange/keeper/intents.go', 'include/layerx/lx_perps.h', 'include/layerx/lxp_genesis.h',
           'src/protocol/lxp_genesis.c', 'src/protocol/lxp_module_ctx.c', 'platform/hosted/node/bootstrap.sh',
           'tests/test_genesis_module_table.c', 'cmd/layerx-genesis/lxp_genesis_build_cli.c',
           'cmd/layerx-genesis/lxp_genesis_builder.c', 'src/modules/asset/lx_asset_registry.c',
           'src/modules/perps/lx_perps_command.c', 'src/modules/perps/lx_perps_dispatch.c', 'src/modules/perps/lx_perps_book.c']

DRIVER = r'''
#define main lx_perps_book_fixture_main
#include "tests/modules/test_perps_book.c"
#undef main

#include <stdio.h>
#include <stdlib.h>

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
    ctx.protocol_version = LXP_PROTOCOL_VERSION_STATE_COMMITMENT;
    activity.protocol_version = LXP_PROTOCOL_VERSION_STATE_COMMITMENT;
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
    CHECK(lx_perps_order_command_decode_versioned(bytes, length, &command) == LXP_OK);
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
        CHECK(lx_perps_order_command_decode_versioned(bytes, length, &command) ==
              LXP_OK);
        CHECK(command.time_in_force == tif);
        CHECK(lx_perps_order_command_decode(bytes, length, &command) == LXP_ERR_NON_CANONICAL);
        CHECK(lx_perps_order_command_decode_versioned(bytes, length, &command) == LXP_OK);
        CHECK(lx_perps_order_command_encode(&command, legacy) ==
              LXP_ERR_NON_CANONICAL);
        (void)memcpy(legacy, bytes, sizeof(legacy));
    }
    bytes[LX_PERPS_ORDER_PAYLOAD_BYTES] = 0U;
    CHECK(lx_perps_order_command_decode(bytes, 130U, &command) ==
          LXP_ERR_NON_CANONICAL);
    CHECK(lx_perps_order_command_decode_versioned(bytes, 130U, &command) == LXP_ERR_NON_CANONICAL);
    bytes[LX_PERPS_ORDER_PAYLOAD_BYTES] = 4U;
    CHECK(lx_perps_order_command_decode(bytes, 130U, &command) ==
          LXP_ERR_NON_CANONICAL);
    CHECK(lx_perps_order_command_decode_versioned(bytes, 130U, &command) == LXP_ERR_NON_CANONICAL);
    CHECK(lx_perps_order_command_decode_versioned(bytes, 131U, &command) == LXP_ERR_NON_CANONICAL);
    CHECK(lx_perps_order_command_decode(bytes, 131U, &command) ==
          LXP_ERR_NON_CANONICAL);
}

static int component_asset_commit(void)
{
    lx_asset_record canonical = asset_record, decoded[1];
    uint8_t key[38], bytes[384]; size_t length = 0U, count = 0U;
    canonical.name[0] = 'Q'; canonical.name_length = 1U;
    canonical.issuer_kind = 2U;
    (void)memcpy(canonical.issuer_did32, administrator, 32U);
    canonical.total_units = (lxp_u128){0U, 0U};
    for (size_t i = 0U; i < accounts.count; ++i) {
        const lx_account *account = &accounts.accounts[i];
        if (account->has_asset && memcmp(account->asset_id, canonical.asset_id, 32U) == 0 &&
            lxp_u128_add(canonical.total_units, account->balance, &canonical.total_units) != LXP_OK)
            return 1;
    }
    if (lx_asset_record_encode(&canonical, bytes, sizeof(bytes), &length) != LXP_OK ||
        lxp_kernel_register_module(&kernel, lx_asset_module_iface()) != LXP_OK ||
        lxp_arena_init(&arena, arena_storage.bytes, sizeof(arena_storage.bytes)) != LXP_OK ||
        lxp_module_ctx_init(&ctx, &kernel, LXP_MODULE_ASSET, batch_timestamp, 0U,
                            global_sequence, 1000000U, &arena, true) != LXP_OK)
        return 1;
    ctx.protocol_version = LXP_PROTOCOL_VERSION_STATE_COMMITMENT;
    (void)memcpy(key, "asset:", 6U); (void)memcpy(key + 6U, canonical.asset_id, 32U);
    if (lxp_ctx_kv_put(&ctx, key, sizeof(key), bytes, length) != LXP_OK ||
        lxp_module_ctx_commit(&ctx) != LXP_OK ||
        lx_asset_committed_records(&kernel, decoded, 1U, &count) != LXP_OK)
        return 1;
    return count != 1U || memcmp(decoded[0].asset_id, canonical.asset_id, 32U) != 0 ||
           lxp_u128_cmp(decoded[0].total_units, canonical.total_units) != 0;
}

static int imported_order_semantics(const lx_perps_order_command *command)
{
    lx_perps_market market;
    lx_perps_order incoming = {0};
    lx_perps_book book, before;
    lx_perps_fill fills[1]; size_t fill_count = 0U, legs = 0U;
    market_defaults(1U, &market);
    (void)memcpy(market.market_id, command->market_id, 32U);
    (void)memcpy(incoming.market_id, command->market_id, 32U);
    (void)memcpy(incoming.order_id, command->order_id, 32U);
    (void)memcpy(incoming.owner_account_id, command->owner_account_id, 32U);
    incoming.side = command->side; incoming.price = command->price;
    incoming.quantity = command->quantity; incoming.time_in_force = command->time_in_force;
    if (lx_perps_book_init(&book) != LXP_OK || ctx_open() != 0) return 1;
    ctx.protocol_version = LXP_PROTOCOL_VERSION_STATE_COMMITMENT;
    before = book;
    lxp_result status = lx_perps_order_place_execute(&ctx, &book, &market, &incoming,
        bob_margin_a->balance, fills, 1U, &fill_count, &legs);
    CHECK(fill_count == 0U && legs == 0U);
    if (command->time_in_force == LX_PERPS_TIF_FILL_OR_KILL) {
        CHECK(status == LXP_ERR_AGREEMENT_STATE && memcmp(&book, &before, sizeof(book)) == 0);
    } else if (command->time_in_force == LX_PERPS_TIF_IMMEDIATE_OR_CANCEL) {
        CHECK(status == LXP_OK && memcmp(&book, &before, sizeof(book)) == 0);
    } else {
        CHECK(status == LXP_OK && book.count == 1U && book.orders[0].active);
        CHECK(lxp_u128_cmp(book.orders[0].remaining, command->quantity) == 0 &&
              memcmp(book.orders[0].order_id, command->order_id, 32U) == 0);
    }
    return 0;
}

static int cross_language_payloads(void)
{
    const char *path = getenv("PAXEER_X_TIF_PAYLOAD_FILE");
    if (path == NULL) return 1;
    FILE *file = fopen(path, "rb");
    if (file == NULL) return 1;
    lx_perps_order_command previous = {0};
    for (unsigned i = 0U; i < 4U; ++i) {
        char line[264]; uint8_t payload[130], canonical[130]; size_t encoded = 0U;
        lx_perps_order_command command, legacy_decoded;
        size_t length = i == 0U ? 129U : 130U;
        if (fgets(line, sizeof(line), file) == NULL || strlen(line) != 3U + 2U*length ||
            line[0] != (char)('0' + i) || line[1] != ' ' || line[2U + 2U*length] != '\n') { fclose(file); return 1; }
        for (size_t j = 0U; j < length; ++j) {
            const char *digits = "0123456789abcdef";
            const char *high = strchr(digits, line[2U + j*2U]), *low = strchr(digits, line[3U + j*2U]);
            if (high == NULL || low == NULL || *high == 0 || *low == 0) { fclose(file); return 1; }
            payload[j] = (uint8_t)(((unsigned)(high-digits) << 4U) | (unsigned)(low-digits));
        }
        CHECK(lx_perps_order_command_decode_versioned(payload, length, &command) == LXP_OK);
        CHECK((unsigned)command.time_in_force == i && command.side == LX_PERPS_SIDE_BUY);
        CHECK(command.price.hi == 0U && command.price.lo == 17U && command.quantity.hi == 0U && command.quantity.lo == 2U);
        CHECK(lx_perps_order_command_encode_versioned(&command, canonical, sizeof(canonical), &encoded) == LXP_OK);
        CHECK(encoded == length && memcmp(canonical, payload, length) == 0);
        CHECK(lx_perps_order_command_decode(payload, length, &legacy_decoded) == (i == 0U ? LXP_OK : LXP_ERR_NON_CANONICAL));
        for (size_t j = 0U; j < 32U; ++j) CHECK(command.owner_account_id[j] == 0x32U);
        if (i != 0U) {
            CHECK(memcmp(command.market_id, previous.market_id, 32U) == 0);
            CHECK(memcmp(command.order_id, previous.order_id, 32U) != 0);
        }
        if (imported_order_semantics(&command) != 0) { fclose(file); return 1; }
        previous = command;
    }
    int result = fgetc(file) == EOF && !ferror(file) ? 0 : 1;
    if (fclose(file) != 0) return 1;
    return result;
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
    length = encode(99U, bob_margin_a, LX_PERPS_SIDE_BUY, 100U, 1U,
                    LX_PERPS_TIF_IMMEDIATE_OR_CANCEL, payload);
    sequence = main_sequence("did:key:bob");
    CHECK(length == 130U);
    CHECK(run_at(sequence, "did:key:bob", payload, length) == LXP_ERR_NON_CANONICAL);
    CHECK(main_sequence("did:key:bob") == sequence && absent(99U));
    if (lxp_kernel_register_module(&kernel, lx_perps_oracle_transport_module_iface()) != LXP_OK) return 2;
    CHECK(run_at(sequence, "did:key:bob", payload, length) == LXP_ERR_NON_CANONICAL);
    CHECK(main_sequence("did:key:bob") == sequence && absent(99U));
    if (component_asset_commit() != 0 ||
        lxp_kernel_register_module(&kernel, lx_perps_tif_module_iface()) != LXP_OK) return 2;
    codec_cases();
    if (cross_language_payloads() != 0) return 2;

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

def require(ok, reason):
    if not ok:
        raise RuntimeError(reason)

def run(argv, **kw):
    return subprocess.run([str(a) for a in argv], cwd=ROOT, check=True, **kw)

def digest(path):
    with Path(path).open('rb') as stream:
        return hashlib.file_digest(stream, 'sha256').hexdigest()

def identity():
    require(not run(['git', 'status', '--porcelain'], capture_output=True, text=True).stdout.strip(), 'dirty source')
    return run(['git', 'rev-parse', 'HEAD'], capture_output=True, text=True).stdout.strip()

def directory():
    p = Path(os.environ['PAXEER_X_TIF_BUILD_DIR'])
    require(p.is_absolute() and p.is_dir() and not p.is_symlink() and p.stat().st_mode & 0o077 == 0, 'private build directory required')
    return p

def build():
    out = directory(); revision = identity(); commands = []; binaries = {}
    def produce(argv, **kw):
        commands.append([str(a) for a in argv]); print('BUILD ' + json.dumps(commands[-1]), flush=True)
        return run(argv, **kw)
    produce([sys.executable, 'tools/bringup/tests/foundation-artifacts.py', '--build', '--output', out / 'foundation'])
    native = ROOT / 'build/liblayerx.a'; programs = ROOT / 'programs/target/debug/liblayerx_programs_sandbox.a'
    env = dict(os.environ, PAXEER_X_RUNTIME_NATIVE_LIBRARY=str(native), PAXEER_X_RUNTIME_NATIVE_REVISION=revision,
               PAXEER_X_RUNTIME_PROGRAMS_LIBRARY=str(programs), PAXEER_X_RUNTIME_EVIDENCE=str(out / 'common'))
    produce(['bash', 'tools/paxeer-x/gates/24.14.sh', 'build'], env=env)
    produce(['make', '-j5', 'build/tests/test_genesis_module_table'])
    shutil.copyfile(ROOT / 'build/tests/test_genesis_module_table', out / 'genesis-module-table')
    source = out / 'native-matcher.c'; source.write_text(DRIVER)
    for name, path in [('native-matcher', source), ('tif-client', ROOT / 'tests/daemon/lxp_test_exchange_tif_client.c')]:
        produce(['cc', '-std=c17', '-O2', '-ffunction-sections', '-fdata-sections', '-I.', '-Iinclude', '-Itests/daemon', path,
                 '-Wl,--gc-sections', '-Wl,--start-group', native, programs, '-Wl,--end-group', '-lcrypto', '-lsqlite3', '-pthread', '-ldl', '-lm', '-o', out / name])
    produce(['go', 'test', '-c', '-o', out / 'go-keeper', './modules/layerxexchange/keeper/'])
    produce(['go', 'test', '-c', '-o', out / 'go-precompile', './precompiles/layerxexchange/'])
    for name, workspace, package, target in [('rust-payload', 'agent', 'layerx-types', 'trading_payload'),
                                             ('rust-router', 'human', 'layerx-intents', 'precompile_router')]:
        argv = ['cargo', 'test', '--locked', '--no-run', '--message-format=json', '-p', package, '--test', target]
        commands.append(argv)
        compiled = subprocess.run(argv, cwd=ROOT / workspace, check=True, capture_output=True, text=True)
        (out / (name + '-build.log')).write_text(compiled.stdout + compiled.stderr)
        rows = [json.loads(line) for line in compiled.stdout.splitlines() if line.startswith('{')]
        artifacts = [row['executable'] for row in rows if row.get('reason') == 'compiler-artifact' and
                     row.get('target', {}).get('name') == target and row.get('executable')]
        require(len(artifacts) == 1, 'unambiguous prebuilt Rust test executable required')
        shutil.copyfile(artifacts[0], out / name)
    for name in COMPONENTS + ['tif-client']:
        (out / name).chmod(0o500); binaries[name] = {'path': str(out / name), 'sha256': digest(out / name)}
    require(identity() == revision, 'source changed during build')
    manifest = {'revision': revision, 'tree': run(['git', 'rev-parse', 'HEAD^{tree}'], capture_output=True, text=True).stdout.strip(),
                'sources': {p: digest(ROOT / p) for p in SOURCES}, 'binaries': binaries, 'commands': commands, 'exit_code': 0}
    (out / 'targets.json').write_text(json.dumps(manifest, indent=2) + '\n'); (out / 'targets.json').chmod(0o600)

def targets():
    out = directory(); path = out / 'targets.json'
    require(path.stat().st_mode & 0o077 == 0 and not path.is_symlink(), 'unprotected target manifest')
    value = json.loads(path.read_text())
    require(value['revision'] == identity() and value['exit_code'] == 0, 'prebuilt revision mismatch')
    require(value['tree'] == run(['git', 'rev-parse', 'HEAD^{tree}'], capture_output=True, text=True).stdout.strip(), 'prebuilt source tree mismatch')
    require(value['sources'] == {p: digest(ROOT / p) for p in SOURCES}, 'prebuilt source digest mismatch')
    require(set(value['binaries']) == set(COMPONENTS + ['tif-client']), 'incomplete prebuilt targets')
    for item in value['binaries'].values():
        p = Path(item['path'])
        require(p.parent == out and not p.is_symlink() and os.access(p, os.X_OK) and digest(p) == item['sha256'], 'prebuilt binary mismatch')
    return value

def runtime_import():
    sys.path.insert(0, str(ROOT / 'tests/daemon'))
    import paxeer_x_runtime_fixture as fixture
    return fixture

def rows(raw, prefix):
    return [dict(x.split('=', 1) for x in line.split()[1:]) for line in raw.decode().splitlines() if line.startswith(prefix + ' ')]

def worker(base):
    fixture = runtime_import()
    built = targets()
    out = directory()
    bundle = fixture.artifacts(out / 'foundation/manifest.json')
    client = fixture.client_artifact(out / 'common/client-manifest.json')
    for name in ('net', 'pid', 'mnt'):
        require(os.readlink('/proc/self/ns/' + name) != os.environ['TIF_PARENT_' + name.upper()], 'namespace not isolated')
    fixture.run(['ip', 'link', 'set', 'lo', 'up'])
    fixture.run(['mount', '-t', 'tmpfs', '-o', 'mode=0755,nosuid,nodev', 'tif-fixture', '/tmp'])
    source = Path('/tmp/tif-source'); source.mkdir(mode=0o755)
    fixture.run(['mount', '--bind', ROOT, source])
    fixture.run(['mount', '-o', 'remount,bind,ro,nosuid,nodev', source])
    fixture.ROOT = source
    python_root = Path('/tmp/tif-python'); python_root.mkdir(mode=0o755)
    fixture.run(['mount', '--bind', sys.prefix, python_root])
    fixture.run(['mount', '-o', 'remount,bind,ro,nosuid,nodev', python_root])
    os.environ['PATH'] = str(python_root / 'bin') + ':/usr/local/bin:/usr/bin:/bin'
    class ActivatedFixture(fixture.RuntimeFixture):
        def produce(self, label, argv, env=None, timeout=120):
            if label == 'bootstrap':
                argv = [*argv, '--perps-oracle-transport', '1', '--perps-order-tif', '1']
            return super().produce(label, argv, env, timeout)
    cases = []
    runtime_result = base / 'result.json'
    def record_case(name):
        cases.append(name)
        runtime_result.write_text(json.dumps({'cases': cases, 'skipped': 0, 'exit_code': 1}, indent=2) + '\n')
    runtime_result.write_text(json.dumps({'cases': [], 'skipped': 0, 'exit_code': 1}) + '\n')
    for activated in (True,):
        d = base / ('activated' if activated else 'legacy')
        runtime = (ActivatedFixture if activated else fixture.RuntimeFixture)(d, bundle, client)
        retained = []
        try:
            runtime.generate()
            wires = d / 'activities'; wires.mkdir(mode=0o700)
            sequence, bob_sequence = 0, 0
            def invoke(operation, expected=0, observation_sequence=2, replay=None, actor='treasury', consume=True, extra=None):
                nonlocal sequence, bob_sequence
                seq = bob_sequence if actor == 'bob' else sequence
                path = replay or wires / (operation + '-' + str(seq) + '-' + str(time.time_ns()))
                command = [built['binaries']['tif-client']['path'], str(d / 'run/layerxd.lni.sock'), str(d / 'salt'),
                           'replay' if replay else operation, str(seq), str(observation_sequence), str(path), str(expected)]
                env = runtime.env | {'PAXEER_X_FIXTURE_KEYS': str(d / 'keys'), 'ORACLE_ASSET': fixture.ASSET,
                    'ORACLE_CREDIT_PROFILE': str(d / 'custody.profile'), 'ORACLE_CREDIT_FILE': str(d / (operation + '.credit')), 'TIF_ACTOR': actor} | (extra or {})
                result = subprocess.run(command, env=env, capture_output=True, timeout=45)
                (d / (operation + '-' + str(time.time_ns()) + '.log')).write_bytes(result.stdout + result.stderr)
                require(result.returncode == 0, 'oracle client failed: ' + operation + ', fixture=' + str(d))
                got = rows(result.stdout, 'receipt')
                ingress = operation.startswith('ingress-') and replay is None
                if ingress:
                    require(len(rows(result.stdout, 'refusal')) == 1 and not got, 'missing ingress refusal')
                    return None, path, []
                require(len(got) == 1 and int(got[0]['result']) == expected, 'wrong receipt result')
                if replay is None:
                    retained.extend(got)
                    if consume:
                        if actor == 'bob': bob_sequence += 1
                        else: sequence += 1
                return got[0], path, rows(result.stdout, 'observation')
            def state(include_oracle=False, bob=False, insurance_stream=False):
                op = ('read-bob-state' if include_oracle else 'read-bob') if bob else 'read-state' if include_oracle else 'read-accounts'
                if insurance_stream:
                    require(not include_oracle and not bob, 'ambiguous insurance stream state read')
                    op = 'read-insurance-stream'
                result = subprocess.run([built['binaries']['tif-client']['path'], str(d / 'run/layerxd.lni.sock'), str(d / 'salt'), op, '0', '0', '/dev/null', '0'],
                    env=runtime.env | {'PAXEER_X_FIXTURE_KEYS': str(d / 'keys'), 'ORACLE_ASSET': fixture.ASSET}, capture_output=True, timeout=20)
                (d / ('state-' + str(time.time_ns()) + '.log')).write_bytes(result.stdout + result.stderr)
                require(result.returncode == 0, 'authenticated state read failed: ' + str(d))
                got = rows(result.stdout, 'state')
                require(len(got) == 4 + int(include_oracle) + int(insurance_stream) and len({r['root'] for r in got}) == 1, 'inconsistent signed state head')
                value = {r['label']: bytes.fromhex(r['raw']) for r in got}
                require(len(value) == len(got) and len(value['global_sequence']) == 8, 'duplicate state label or invalid sequence')
                preparation = rows(result.stdout, 'preparation')
                require(len(preparation) == 1 and preparation[0]['root'] == got[0]['root'] and
                        int(preparation[0]['head']) + 1 == int.from_bytes(value['global_sequence'], 'big') and
                        len({r['timestamp'] for r in got}) == 1 and preparation[0]['timestamp'] == got[0]['timestamp'],
                        'local preparation sequence does not match authenticated state head')
                value['actor_sequence'] = int(preparation[0]['actor_sequence']).to_bytes(8, 'big')
                value['root'] = bytes.fromhex(got[0]['root'])
                return value
            def decode_account(raw):
                n = int.from_bytes(raw[:2], 'big')
                require(len(raw) == n + 103 and n > 0 and raw[51+n] == 1 and
                        raw[19+n:51+n].hex() == fixture.ASSET, 'account custody asset or canonical length')
                return n, int.from_bytes(raw[3+n:19+n], 'big'), int.from_bytes(raw[52+n:60+n], 'big')
            def balance(raw):
                return decode_account(raw)[1]
            def account_effect(before, after, delta, sequence_delta, label):
                n, funds, seq = decode_account(before)
                m, updated, updated_seq = decode_account(after)
                require(n == m and updated - funds == delta and updated_seq - seq == sequence_delta,
                        label + ': exact balance/ledger sequence effect')
                left, right = bytearray(before), bytearray(after)
                for value in (left, right):
                    value[3+n:19+n] = bytes(16)
                    value[52+n:60+n] = bytes(8)
                require(left == right, label + ': unrelated account field mutation')
            expected_fee = json.loads((ROOT / 'platform/hosted/node/genesis-module-fees.json').read_text())['perps']
            require(type(expected_fee) is int and 0 < expected_fee < 10000, 'positive bounded production Perps fee')
            def oracle_effect(before, after, receipt, fee, consume=True):
                require(int(receipt['fee']) == fee, 'receipt fee differs from configured production fee')
                require(int(receipt['module']) == 3, 'receipt uses wrong Perps ABI')
                require(int.from_bytes(after['actor_sequence'], 'big') - int.from_bytes(before['actor_sequence'], 'big') == int(consume),
                        'wrong consumed actor sequence')
                require(int(receipt['sequence']) >= int.from_bytes(before['global_sequence'], 'big') and
                        int.from_bytes(after['global_sequence'], 'big') > int(receipt['sequence']), 'terminal receipt not in committed sequence')
                account_effect(before['owner'], after['owner'], -fee, 0, 'oracle actor')
                account_effect(before['fees'], after['fees'], fee, int(fee > 0), 'fee treasury')
                require(after['insurance'] == before['insurance'], 'oracle changed insurance account')
            runtime.produce('custody-event-topic', ['python3', fixture.ROOT / 'platform/hosted/paxeer/evm.py', 'keccak',
                '0x' + b'CustodyDeposit(bytes32,bytes32,address,bytes32,uint256,uint64)'.hex()])
            topic = (d / 'custody-event-topic.log').read_text().strip().removeprefix('0x').lower()
            require(len(topic) == 64, 'custody event topic encoding')
            runtime.produce('custody-depositor', ['python3', fixture.ROOT / 'platform/hosted/paxeer/evm.py', 'address', d / 'keys/deployer.key'])
            depositor = (d / 'custody-depositor.log').read_text().strip().removeprefix('0x').lower()
            require(len(depositor) == 40, 'custody depositor address encoding')
            from cryptography.hazmat.primitives.asymmetric.ed25519 import Ed25519PrivateKey
            from cryptography.hazmat.primitives.serialization import Encoding, PublicFormat
            for actor, operation in [('treasury', 'credit'), ('bob', 'credit-bob')]:
                public = Ed25519PrivateKey.from_private_bytes((d / ('keys/' + actor + '.seed')).read_bytes()).public_key().public_bytes(Encoding.Raw, PublicFormat.Raw)
                did = ('did:layerx:' + public.hex()).encode()
                account_name = b'agent:' + did + b':main'
                beneficiary = hashlib.sha256(b'LX:ACCOUNT:v1' + len(account_name).to_bytes(4, 'big') + account_name).digest()
                amount = 1000000
                runtime.produce(operation + '-deposit', ['python3', fixture.ROOT / 'platform/hosted/paxeer/evm.py', 'send',
                    '--rpc', runtime.rpc_url, '--chain', '125', '--key-file', d / 'keys/deployer.key', '--value', str(amount * 10**12),
                    '0x0000000000000000000000000000000000001013', 'deposit(bytes32)', '0x' + beneficiary.hex()])
                deposited = json.loads((d / (operation + '-deposit.log')).read_text())
                require(int(deposited['status'], 16) == 1, 'native deposit reverted')
                logs = [e for e in deposited['logs'] if e['address'].lower() == '0x0000000000000000000000000000000000001013' and len(e['topics']) == 4 and
                        e['topics'][0].removeprefix('0x').lower() == topic]
                require(len(logs) == 1, 'deposit event count')
                entry = logs[0]; data = bytes.fromhex(entry['data'].removeprefix('0x'))
                require(len(data) == 96 and data[64:88] == bytes(24) and
                        entry['topics'][2].removeprefix('0x').lower() == fixture.ASSET and
                        entry['topics'][3].removeprefix('0x').lower() == '0' * 24 + depositor and
                        entry['blockNumber'] == deposited['blockNumber'] and entry.get('removed') is not True and
                        data[:32] == beneficiary and int.from_bytes(data[32:64], 'big') == amount, 'deposit event/payer/asset/beneficiary/amount binding')
                runtime.wait(lambda: int(runtime.rpc('eth_blockNumber'), 16) >= int(deposited['blockNumber'], 16) + 2)
                runtime.produce(operation + '-proof', [runtime.binary('layerx-custody-proof'), 'light-credit', '--rpc', 'http://127.0.0.1:' + str(runtime.ports[2]),
                    '--profile', d / 'custody.profile', '--deposit-id', entry['topics'][1], '--owner-key', '0x' + public.hex(), '--output', d / (operation + '.credit')])
                credit = (d / (operation + '.credit')).read_bytes()
                require(len(credit) > 363 and credit[:5] == b'LXDC3' and credit[43:75] == bytes.fromhex(entry['topics'][1][2:]) and credit[75:107].hex() == fixture.ASSET and credit[107:139] == beneficiary and credit[139:171] == public and int.from_bytes(credit[191:207], 'big') == amount, 'credit proof binding')
                invoke(operation, actor=actor)
                require(balance(state(bob=actor == 'bob')['owner']) == amount, 'native funding balance')
            prefix = 'activated-' if activated else 'legacy-'
            record_case(prefix + 'real-custody-funding')
            before = state()
            stream_fee = json.loads((ROOT / 'platform/hosted/node/genesis-module-fees.json').read_text())['stream']
            require(type(stream_fee) is int and 0 < stream_fee < 10000, 'positive bounded production Stream fee')
            opened, _, _ = invoke('insurance-open')
            funded = state(insurance_stream=True)
            require(int(opened['fee']) == stream_fee and balance(funded['stream']) == 10000 and
                    funded['insurance'] == before['insurance'], 'insurance stream funding or fee')
            stream_name_length = decode_account(funded['stream'])[0]
            owner_name_length = decode_account(funded['owner'])[0]
            stream_id = bytes([0x44, 0x04, 0x01]) + bytes(29)
            expected_stream_name = funded['owner'][2:2+owner_name_length].removesuffix(b':main') + b':stream:' + stream_id.hex().encode()
            require(funded['stream'][2:2+stream_name_length] == expected_stream_name and
                    funded['insurance'][2:2+decode_account(funded['insurance'])[0]] == b'system:insurance',
                    'insurance funding account identity')
            account_effect(before['owner'], funded['owner'], -10000-stream_fee, 1, 'stream funding owner')
            account_effect(before['fees'], funded['fees'], stream_fee, 1, 'stream funding fee treasury')
            insurance, _, _ = invoke('insurance')
            after = state(insurance_stream=True)
            require(int(insurance['fee']) == stream_fee, 'insurance settlement production fee')
            account_effect(funded['owner'], after['owner'], -stream_fee, 0, 'stream settlement owner')
            account_effect(funded['stream'], after['stream'], -10000, 1, 'stream settlement custody')
            account_effect(funded['insurance'], after['insurance'], 10000, 0, 'stream settlement insurance')
            account_effect(funded['fees'], after['fees'], stream_fee, 1, 'stream settlement fee treasury')
            require(balance(after['insurance']) - balance(before['insurance']) == 10000 and
                    balance(before['owner']) - balance(after['owner']) == 10000 + int(opened['fee']) + int(insurance['fee']) and
                    balance(after['stream']) == 0 and
                    sum(balance(before[label]) for label in ('owner', 'insurance', 'fees')) ==
                    sum(balance(after[label]) for label in ('owner', 'insurance', 'fees', 'stream')),
                    'insurance conservation and exact asset/fee accounting')
            record_case(prefix + 'real-insurance-funding')
            market, _, _ = invoke('market')
            require(int(market['module']) == 3 and int(market['fee']) > 0, 'selected market ABI or production fee')
            invoke('oracle', observation_sequence=1)
            observation_sequence = 1
            observation_clock = time.monotonic()
            def tif_state(actor='treasury', order=0, position=False):
                env = runtime.env | {'PAXEER_X_FIXTURE_KEYS': str(d / 'keys'), 'ORACLE_ASSET': fixture.ASSET, 'TIF_ACTOR': actor}
                if position: env['TIF_POSITION'] = '1'
                result = subprocess.run([built['binaries']['tif-client']['path'], str(d / 'run/layerxd.lni.sock'),
                    str(d / 'salt'), 'tif-read', '0', str(order), '/dev/null', '0'], env=env, capture_output=True, timeout=20)
                (d / ('tif-state-' + str(time.time_ns()) + '.log')).write_bytes(result.stdout + result.stderr)
                require(result.returncode == 0, 'authenticated TIF state read failed')
                got = rows(result.stdout, 'state')
                require(len(got) == 2 + int(order != 0) + int(position) and len({r['root'] for r in got}) == 1, 'inconsistent TIF state head')
                return {r['label']: bytes.fromhex(r['raw']) for r in got}
            def order(identifier, side, price, quantity, tif, actor='bob', expected=0, malformed=False, refresh=True):
                nonlocal observation_sequence, observation_clock
                if refresh and time.monotonic() - observation_clock > 20.0:
                    observation_sequence += 1
                    invoke('oracle', observation_sequence=observation_sequence)
                    observation_clock = time.monotonic()
                before = state(bob=actor == 'bob')
                other_before = state(bob=actor != 'bob')
                operation = 'tif-malformed' if malformed else 'tif-order'
                result, wire, _ = invoke(operation, expected, observation_sequence=identifier, actor=actor,
                    extra={'TIF_SIDE': side, 'TIF_PRICE': str(price), 'TIF_QUANTITY': str(quantity), 'TIF_VALUE': str(tif)})
                require(int(result['module']) == 3 and int(result['fee']) == expected_fee, 'order ABI/fee mismatch')
                after = state(bob=actor == 'bob'); other_after = state(bob=actor != 'bob')
                account_effect(before['owner'], after['owner'], -expected_fee, 0, 'order owner fee')
                account_effect(before['fees'], after['fees'], expected_fee, 1, 'order fee treasury')
                require(after['insurance'] == before['insurance'] and other_after['owner'] == other_before['owner'] and
                        int.from_bytes(after['actor_sequence'], 'big') == int.from_bytes(before['actor_sequence'], 'big') + 1,
                        'order fee conservation, insurance or actor sequence mutation')
                return result, wire
            def remaining(raw):
                require(len(raw) == 170, 'noncanonical order record')
                return int.from_bytes(raw[129:145], 'big'), raw[169] == 1
            def position_size(raw):
                require(len(raw) == 211 and raw[-1] == 1, 'noncanonical or closed position')
                return int.from_bytes(raw[161:177], 'big'), int.from_bytes(raw[177:193], 'big')
            for actor in ('treasury', 'bob'):
                before = state(bob=actor == 'bob')
                invoke('tif-register-wrong-id', -409, actor=actor)
                invoke('tif-register-other-owner', -409, actor=actor)
                registered, _, _ = invoke('tif-register', actor=actor)
                require(int(registered['module']) == 3, 'margin registration ABI')
                invoke('tif-register', -301, actor=actor)
                opened, _, _ = invoke('tif-fund-open', actor=actor)
                staged = tif_state(actor)
                require(balance(staged['margin']) == 0 and balance(staged['margin_stream']) == 10000, 'margin not zero on registration or stream not funded')
                settled, _, _ = invoke('tif-fund-settle', actor=actor)
                funded = tif_state(actor); after = state(bob=actor == 'bob')
                require(balance(funded['margin']) == 10000 and balance(funded['margin_stream']) == 0, 'margin funding not settled exactly')
                account_effect(staged['margin'], funded['margin'], 10000, 0, 'margin recipient')
                account_effect(staged['margin_stream'], funded['margin_stream'], -10000, 1, 'margin stream')
                expected_name = after['owner'][2:2+decode_account(after['owner'])[0]].removesuffix(b':main') + b':margin:' + (bytes([0x44, 0x04]) + bytes(30)).hex().encode()
                require(funded['margin'][2:2+decode_account(funded['margin'])[0]] == expected_name, 'canonical margin owner/market binding')
                require(balance(before['owner']) - balance(after['owner']) == 10000 + 4*expected_fee + 2*stream_fee, 'margin exact owner debit and fees')
                require(balance(after['fees']) - balance(before['fees']) == 4*expected_fee + 2*stream_fee and after['insurance'] == before['insurance'], 'margin conservation or insurance mutation')
                record_case('real-margin-funding-' + actor)
            order(10, 'sell', 100, 3, 0, actor='treasury')
            require(remaining(tif_state('treasury', 10)['order']) == (3, True), 'GTC did not rest')
            record_case('gtc-rests')
            before = tif_state('treasury', 10); bob_before = tif_state()
            for identifier, quantity, tif in [(11, 1, 3), (13, 5, 2)]:
                order(identifier, 'buy', 100, quantity, tif, expected=-706)
                require(tif_state('treasury', 10) == before and tif_state() == bob_before, 'refused post-only/FOK changed trade state')
            record_case('crossing-post-only-and-insufficient-fok-atomic-refusal')
            order(12, 'buy', 99, 1, 3)
            require(remaining(tif_state(order=12)['order']) == (1, True), 'noncrossing post-only did not rest')
            record_case('noncrossing-post-only-rests')
            fok, fok_wire = order(13, 'buy', 100, 2, 2)
            require(remaining(tif_state(order=13)['order']) == (0, False) and remaining(tif_state('treasury', 10)['order']) == (1, True), 'FOK residual/maker quantity')
            require(position_size(tif_state(position=True)['position']) == (2, 200), 'FOK position mismatch')
            record_case('fok-full-fill')
            ioc, ioc_wire = order(14, 'buy', 100, 4, 1)
            snapshot = tif_state(order=14, position=True)
            require(remaining(snapshot['order']) == (0, False) and position_size(snapshot['position']) == (3, 300), 'IOC fill or residual cancellation mismatch')
            require(position_size(tif_state('treasury', position=True)['position']) == (3, 300), 'maker fill mismatch')
            record_case('ioc-partial-fill-cancels-residual')
            for killed in (False, True):
                treasury_state = state(); bob_state = state(bob=True)
                maker_state = tif_state('treasury', position=True)
                original_wire = ioc_wire.read_bytes()
                runtime.restart(kill=killed)
                repeated, _, _ = invoke('tif-order', replay=ioc_wire, actor='bob')
                require(repeated['raw'] == ioc['raw'] and ioc_wire.read_bytes() == original_wire and
                        tif_state(order=14, position=True) == snapshot and tif_state('treasury', position=True) == maker_state and
                        state() == treasury_state and state(bob=True) == bob_state, 'matching restart/retry changed original receipt or economic state')
                record_case('matching-commit-' + ('forced' if killed else 'clean') + '-restart-once')
            order(15, 'buy', 100, 1, 1)
            require(remaining(tif_state(order=15)['order']) == (0, False), 'empty IOC rested')
            for tif in (0, 1, 2, 3):
                before = tif_state(order=12, position=True); maker_before = tif_state('treasury', position=True)
                order(30+tif, 'buy', 100, 1000000, tif, expected=-705)
                order(40+tif, 'buy', 150, 1, tif, expected=-732)
                require(tif_state(order=12, position=True) == before and tif_state('treasury', position=True) == maker_before, 'margin/slippage refusal changed trade state')
                record_case('tif-' + str(tif) + '-margin-and-slippage-refusal')
            order(60, 'buy', 100, 1, 1, expected=-3, malformed=True)
            record_case('malformed-tif-refusal')
            for tif in (0, 1, 2):
                first, second, taker = 70+3*tif, 71+3*tif, 72+3*tif
                order(first, 'sell', 100, 2, 0, actor='treasury')
                order(second, 'sell', 100, 2, 0, actor='treasury')
                before_size, before_notional = position_size(tif_state(position=True)['position'])
                order(taker, 'buy', 100, 3, tif)
                require(remaining(tif_state('treasury', second)['order']) == (1, True) and
                        position_size(tif_state(position=True)['position']) == (before_size+3, before_notional+300),
                        'price-time matching consumed later maker first')
                invoke('tif-cancel', observation_sequence=second, actor='treasury')
                record_case('tif-' + str(tif) + '-price-time-priority')
            stale_at = time.monotonic() + 61.0
            while time.monotonic() < stale_at:
                time.sleep(max(0.0, min(0.25, stale_at-time.monotonic())))
            for tif in (0, 1, 2, 3):
                before = tif_state(order=12, position=True); maker_before = tif_state('treasury', position=True)
                order(90+tif, 'buy', 100, 1, tif, expected=-704, refresh=False)
                require(tif_state(order=12, position=True) == before and tif_state('treasury', position=True) == maker_before,
                        'stale oracle refusal changed trade state')
                record_case('tif-' + str(tif) + '-stale-oracle-refusal')
            runtime.catch_up(retained)
            record_case('authenticated-replica-catchup')
        finally:
            runtime.cleanup()
    runtime_result.write_text(json.dumps({'cases': cases, 'skipped': 0, 'exit_code': 0}, indent=2) + '\n')

def qualify():
    out = directory()
    evidence = Path(os.environ['PAXEER_X_EVIDENCE_DIR'])
    evidence.mkdir(mode=0o700, parents=True, exist_ok=True)
    require(evidence.is_absolute() and not evidence.is_symlink() and evidence.stat().st_mode & 0o077 == 0, 'private evidence directory required')
    result = {'task': '4.2', 'command': COMMAND, 'cases': [], 'tests': 0, 'skipped': 0, 'exit_code': 1}
    try:
        built = targets(); result['revision'] = built['revision']; result['tree'] = built['tree']
        base = Path(tempfile.mkdtemp(prefix='px-tif-', dir='/var/tmp'))
        os.chown(base, 0, runtime_import().UID)
        base.chmod(0o710)
        result['runtime_directory'] = str(base)
        env = dict(os.environ)
        for name in ('net', 'pid', 'mnt'):
            env['TIF_PARENT_' + name.upper()] = os.readlink('/proc/self/ns/' + name)
        try:
            with (evidence / 'runtime.log').open('wb') as stream:
                run(['unshare', '--mount', '--net', '--pid', '--fork', '--kill-child=KILL', '--mount-proc', '--propagation', 'private',
                     sys.executable, str(Path(__file__).resolve()), '--worker', base], env=env, stdout=stream, stderr=stream, timeout=900)
        finally:
            runtime_path = base / 'result.json'
            if runtime_path.is_file():
                runtime = json.loads(runtime_path.read_text())
                result['cases'].extend(runtime['cases'])
        require(runtime_path.is_file() and runtime['exit_code'] == 0 and runtime['skipped'] == 0, 'runtime incomplete or skipped a case')
        component_env = dict(os.environ, PAXEER_X_TIF_INGRESS_FILE=str(evidence / 'keeper-intents.json'),
                             PAXEER_X_TIF_PAYLOAD_FILE=str(evidence / 'native-payloads.txt'),
                             PAXEER_X_TIF_NATIVE_GENESIS_FILE=str(evidence / 'native-genesis.manifest'))
        require(not Path(component_env['PAXEER_X_TIF_INGRESS_FILE']).exists() and not Path(component_env['PAXEER_X_TIF_PAYLOAD_FILE']).exists() and
                not Path(component_env['PAXEER_X_TIF_INGRESS_FILE'] + '.events').exists(),
                'fresh cross-language evidence paths required')
        genesis = base / 'activated/node/genesis/genesis.manifest'
        fixture_manifest = json.loads((base / 'activated/fixture.json').read_text())
        require(genesis.is_file() and not genesis.is_symlink() and digest(genesis) == fixture_manifest['genesis_sha256'] and
                fixture_manifest['source_revision'] == built['revision'] and fixture_manifest['artifacts_source_revision'] == built['revision'],
                'actual signed runtime genesis/source binding failed')
        genesis_copy = Path(component_env['PAXEER_X_TIF_NATIVE_GENESIS_FILE'])
        require(not genesis_copy.exists(), 'fresh signed native genesis evidence required')
        shutil.copyfile(genesis, genesis_copy); genesis_copy.chmod(0o400)
        require(digest(genesis_copy) == fixture_manifest['genesis_sha256'], 'signed native genesis copy mismatch')
        result['native_genesis_sha256'] = digest(genesis_copy)
        working = {'go-precompile': 'precompiles/layerxexchange', 'go-keeper': 'modules/layerxexchange/keeper',
                   'rust-payload': 'agent/crates/layerx-types', 'rust-router': 'human/crates/layerx-intents', 'native-matcher': '.', 'genesis-module-table': '.'}
        for name in COMPONENTS:
            argv = [built['binaries'][name]['path']]
            if name == 'go-precompile':
                argv += ['-test.count=1', '-test.run=^TestNativeTimeInForceCapabilityPrecompile$']
            if name == 'go-keeper':
                argv += ['-test.count=1', '-test.run=TestOrderTimeInForceIsRecordedDistinctlyBeforeAnyNonce|TestIntentsAreRecordedWithoutMovingFunds|TestOrderIntentSurvivesCommittedKeeperReopen|TestNativeTimeInForceCapabilityRefusesBeforeNonce|TestNativeCapabilityTruncationAndEvidenceBounds|TestNativeCapabilityMarketWireRoundTrip']
            with (evidence / (name + '.log')).open('wb') as stream:
                subprocess.run(argv, cwd=ROOT / working[name], env=component_env, stdout=stream, stderr=stream, timeout=300, check=True)
            result['cases'].append(name)
        require(Path(component_env['PAXEER_X_TIF_INGRESS_FILE']).is_file() and
                Path(component_env['PAXEER_X_TIF_INGRESS_FILE'] + '.events').stat().st_size == 4*288 and
                len(Path(component_env['PAXEER_X_TIF_PAYLOAD_FILE']).read_text().splitlines()) == 4, 'incomplete production cross-language outputs')
        result['exit_code'] = 0
    except (OSError, ValueError, KeyError, RuntimeError, subprocess.SubprocessError) as error:
        result['failure'] = str(error)
        print(str(error), file=sys.stderr)
    result['tests'] = len(result['cases'])
    path = evidence / 'exchange-tif-result.json'
    path.write_text(json.dumps(result, indent=2) + '\n'); path.chmod(0o600)
    print('PAXEER_X_GATE tests=' + str(result['tests']) + ' skipped=0')
    print('revision=' + result.get('revision', 'unknown') + ' command=' + COMMAND + ' exit_code=' + str(result['exit_code']) + ' log=' + str(path))
    return result['exit_code']

def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--build', action='store_true')
    parser.add_argument('--worker', type=Path)
    args = parser.parse_args()
    os.umask(0o077)
    if args.build:
        build(); return 0
    if args.worker:
        worker(args.worker); return 0
    return qualify()

if __name__ == '__main__':
    sys.exit(main())
