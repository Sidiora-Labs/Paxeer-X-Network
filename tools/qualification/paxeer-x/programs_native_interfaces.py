#!/usr/bin/env python3
"""Interface-bearing native Deploy/Upgrade qualification across guest ABIs 1-4.

The driver is compiled against the candidate kernel library and the candidate
programs runtime static library, dispatches real lifecycle activities through
the registered programs module, restarts from a verified snapshot and checks
the persisted interface and lifecycle records byte for byte.
"""
import datetime
import hashlib
import json
import os
from pathlib import Path
import stat
import subprocess
import sys
import tempfile

ROOT = Path(__file__).resolve().parents[3]
KERNEL_LIB = ROOT / 'build' / 'liblayerx.a'
RUNTIME_LIB = ROOT / 'programs' / 'target' / 'debug' / 'liblayerx_programs_sandbox.a'
SOURCES = [
    'programs/crates/layerx-programs-runtime/src/ffi_interface.rs',
    'programs/crates/layerx-programs-runtime/src/abi_policy.rs',
    'programs/crates/layerx-programs-runtime/src/engine.rs',
    'programs/crates/layerx-programs-runtime/src/ffi_call.rs',
]
KERNEL_SOURCES = ['src/modules/programs/deploy.c']
DOMAIN = b'LayerX/program-interface/v1\0'
AUTHORITY = bytes([0x42]) * 32

DRIVER = r'''
#include "layerx/programs.h"
#include "layerx/lxp_hash.h"
#include "layerx/lxp_kernel.h"
#include "layerx/lxp_snapshot.h"

#include <stdio.h>
#include <stdlib.h>
#include <string.h>

static lxp_state_store stores[2];
static lxp_state_journal journals[2];
static lxp_kernel kernels[2];
static uint8_t payload[65536];
static uint8_t expected_bytes[4096];
static uint8_t snapshot_storage[1U << 20];
static uint8_t arena_bytes[1U << 18];

static const struct { const char *name; lxp_result value; } results[] = {
    {"OK", LXP_OK},
    {"NON_CANONICAL", LXP_ERR_NON_CANONICAL},
    {"VERSION_UNSUPPORTED", LXP_ERR_VERSION_UNSUPPORTED},
    {"CONTEXT_MISMATCH", LXP_ERR_CONTEXT_MISMATCH},
    {"UNKNOWN_FIELD", LXP_ERR_UNKNOWN_FIELD},
    {"PAYLOAD_HASH_MISMATCH", LXP_ERR_PAYLOAD_HASH_MISMATCH},
};

static int result_named(const char *name, lxp_result *value)
{
    size_t i;
    for (i = 0U; i < sizeof(results) / sizeof(results[0]); ++i)
        if (strcmp(results[i].name, name) == 0) {
            *value = results[i].value;
            return 0;
        }
    return 1;
}

static long hex_decode(const char *text, uint8_t *out, size_t capacity)
{
    size_t length = strlen(text), i;
    if (length % 2U != 0U || length / 2U > capacity) return -1;
    for (i = 0U; i < length / 2U; ++i) {
        unsigned int byte;
        if (sscanf(text + 2U * i, "%2x", &byte) != 1) return -1;
        out[i] = (uint8_t)byte;
    }
    return (long)(length / 2U);
}

static uint32_t read_u32(const uint8_t *b)
{
    return ((uint32_t)b[0] << 24U) | ((uint32_t)b[1] << 16U) |
           ((uint32_t)b[2] << 8U) | (uint32_t)b[3];
}

static const lxp_module_kv_entry *find_kv(const lxp_kernel *kernel,
                                          const char *prefix, size_t prefix_length,
                                          const uint8_t program_id[32])
{
    size_t i;
    for (i = 0U; i < kernel->module_kv_count; ++i) {
        const lxp_module_kv_entry *item = &kernel->module_kv[i];
        if (item->module_id == LXP_MODULE_PROGRAMS &&
            item->key_length == prefix_length + 32U &&
            memcmp(item->key, prefix, prefix_length) == 0 &&
            memcmp(item->key + prefix_length, program_id, 32U) == 0)
            return item;
    }
    return NULL;
}

static int create_kernel(int slot)
{
    uint64_t parameters = 1U;
    return lxp_state_store_init(&stores[slot], 2U) != LXP_OK ||
           lxp_kernel_create(&kernels[slot], &stores[slot], &journals[slot],
                             &parameters, 0U) != LXP_OK ||
           lxp_kernel_register_module(&kernels[slot],
                                      programs_module_registration()) != LXP_OK;
}

static int dispatch(int slot, uint16_t ordinal, size_t length,
                    lxp_result expected, lxp_result *observed)
{
    lxp_arena arena;
    lxp_module_ctx ctx;
    lxp_effect_buffer effects;
    lxp_activity activity;
    lxp_authority_resolved authority;
    const lxp_module_registration *registration;
    lxp_kernel *kernel = &kernels[slot];
    *observed = LXP_OK;
    (void)memset(&authority, 0, sizeof(authority));
    (void)memset(authority.principal, 0x42, sizeof(authority.principal));
    (void)memset(&activity, 0, sizeof(activity));
    activity.activity_type = ((uint32_t)LXP_MODULE_PROGRAMS << 16U) | ordinal;
    activity.payload = (lxp_byte_span){payload, length};
    if (lxp_arena_init(&arena, arena_bytes, sizeof(arena_bytes)) != LXP_OK ||
        lxp_kernel_module_for_activity(kernel, activity.activity_type, 0U,
                                       &registration) != LXP_OK ||
        lxp_module_ctx_init(&ctx, kernel, LXP_MODULE_PROGRAMS, 1U, 0U,
                            ordinal, 1000000U, &arena, false) != LXP_OK)
        return 1;
    ctx.protocol_version = LXP_PROTOCOL_VERSION;
    ctx.batch_number = 1U;
    if (lxp_effect_buffer_init(&effects) != LXP_OK ||
        lxp_module_ctx_bind_effects(&ctx, &effects) != LXP_OK ||
        lxp_kernel_dispatch(registration, &ctx, &activity, &authority,
                            &effects, observed) != LXP_OK)
        return 1;
    if (*observed == LXP_OK) {
        if (lxp_module_ctx_commit(&ctx) != LXP_OK) return 1;
    } else {
        lxp_module_ctx_rollback(&ctx);
    }
    return *observed == expected ? 0 : 1;
}

static int restart(int from, int to)
{
    lxp_arena arena;
    lxp_byte_span snapshot;
    lxp_snapshot_manifest_record manifest;
    uint8_t original_root[32];
    uint8_t restored_root[32];
    size_t i;
    lxp_result status;
    if (create_kernel(to) != 0) return 10;
    if (lxp_arena_init(&arena, snapshot_storage, sizeof(snapshot_storage)) != LXP_OK)
        return 11;
    status = lxp_state_root(&kernels[from], original_root);
    if (status != LXP_OK) return 1000 + (int)-status;
    (void)memcpy(kernels[from].current_state_root, original_root, 32U);
    status = lxp_snapshot_write(&kernels[from], stores[from].next_sequence - 1U, &arena,
                                &snapshot);
    if (status != LXP_OK) return 2000 + (int)-status;
    status = lxp_snapshot_manifest_build(snapshot.bytes, snapshot.length,
                                         stores[from].next_sequence - 1U, original_root,
                                         kernels[from].current_state_root, &manifest);
    if (status != LXP_OK) return 3000 + (int)-status;
    status = lxp_snapshot_load(snapshot.bytes, snapshot.length, &manifest, &kernels[to]);
    if (status != LXP_OK) return 4000 + (int)-status;
    if (lxp_state_root(&kernels[to], restored_root) != LXP_OK ||
        memcmp(original_root, restored_root, 32U) != 0)
        return 12;
    status = lxp_snapshot_verify_root(&kernels[to], &manifest);
    if (status != LXP_OK) return 5000 + (int)-status;
    if (kernels[to].module_kv_count != kernels[from].module_kv_count) return 13;
    for (i = 0U; i < kernels[from].module_kv_count; ++i) {
        const lxp_module_kv_entry *a = &kernels[from].module_kv[i];
        size_t j;
        int matched = 0;
        for (j = 0U; j < kernels[to].module_kv_count && !matched; ++j) {
            const lxp_module_kv_entry *b = &kernels[to].module_kv[j];
            matched = b->module_id == a->module_id &&
                b->key_length == a->key_length &&
                memcmp(b->key, a->key, a->key_length) == 0 &&
                b->value_length == a->value_length &&
                memcmp(b->value, a->value, a->value_length) == 0;
        }
        if (!matched) return 14;
    }
    return 0;
}

int main(void)
{
    static char line[200000];
    while (fgets(line, sizeof(line), stdin) != NULL) {
        char label[256], command[32], a[131072], b[64], c[64], d[131072];
        int fields;
        int failed = 1;
        char detail[128] = "";
        line[strcspn(line, "\n")] = '\0';
        a[0] = b[0] = c[0] = d[0] = '\0';
        fields = sscanf(line, "%255s %31s %131071s %63s %63s %131071s",
                        label, command, a, b, c, d);
        if (fields < 2) {
            printf("fail malformed-command\n");
            return 2;
        }
        if (strcmp(command, "new") == 0) {
            failed = create_kernel(atoi(a));
        } else if (strcmp(command, "dispatch") == 0) {
            /* dispatch <slot> <ordinal> <expected> <payload-hex> */
            lxp_result expected, observed = LXP_OK;
            long length = hex_decode(d, payload, sizeof(payload));
            if (length > 0 && result_named(c, &expected) == 0) {
                failed = dispatch(atoi(a), (uint16_t)atoi(b), (size_t)length,
                                  expected, &observed);
                (void)snprintf(detail, sizeof(detail), "observed=%d", (int)observed);
            }
        } else if (strcmp(command, "iface") == 0) {
            /* iface <slot> <program-hex> <version> <encoding-hex|absent> */
            uint8_t program_id[32];
            const lxp_module_kv_entry *item;
            if (hex_decode(a, program_id, 32U) == 32) {
                item = find_kv(&kernels[atoi(b)], "interface", 10U, program_id);
                if (strcmp(d, "absent") == 0) {
                    failed = item != NULL;
                } else {
                    long length = hex_decode(d, expected_bytes, sizeof(expected_bytes));
                    uint8_t digest[32];
                    failed = item == NULL || length <= 0 ||
                        item->value_length != 72U + (size_t)length ||
                        memcmp(item->value, program_id, 32U) != 0 ||
                        read_u32(item->value + 32U) != (uint32_t)atoi(c) ||
                        read_u32(item->value + 68U) != (uint32_t)length ||
                        memcmp(item->value + 72U, expected_bytes, (size_t)length) != 0 ||
                        lxp_hash_sha256(expected_bytes, (size_t)length, digest) != LXP_OK ||
                        memcmp(item->value + 36U, digest, 32U) != 0;
                }
            }
        } else if (strcmp(command, "life") == 0) {
            /* life <slot> <program-hex> <abi> <version> <code-hash-hex> */
            uint8_t program_id[32], hash[32];
            char hash_hex[65];
            const lxp_module_kv_entry *item;
            (void)snprintf(hash_hex, sizeof(hash_hex), "%.64s", d);
            if (strlen(d) > 64U && hex_decode(a, program_id, 32U) == 32 &&
                hex_decode(hash_hex, hash, 32U) == 32) {
                item = find_kv(&kernels[atoi(b)], "program", 8U, program_id);
                failed = item == NULL || item->value_length != 71U ||
                    memcmp(item->value + 33U, hash, 32U) != 0 ||
                    (((unsigned)item->value[65] << 8U) | item->value[66]) !=
                        (unsigned)atoi(c) ||
                    read_u32(item->value + 67U) != (uint32_t)strtoul(d + 64, NULL, 10);
            }
        } else if (strcmp(command, "restart") == 0) {
            failed = restart(atoi(a), atoi(b));
            (void)snprintf(detail, sizeof(detail), "step=%d", failed);
        }
        printf("%s %s %s\n", failed ? "fail" : "ok", label, detail);
        fflush(stdout);
    }
    return 0;
}
'''


def fail(message):
    print('programs_native_interfaces: FAILED: ' + message, file=sys.stderr)
    sys.exit(1)


def leb(value):
    out = bytearray()
    while True:
        byte = value & 0x7F
        value >>= 7
        out.append(byte | (0x80 if value else 0))
        if not value:
            return bytes(out)


def name(text):
    return leb(len(text)) + text.encode()


def section(identifier, body):
    return bytes([identifier]) + leb(len(body)) + body


def vector(items):
    return leb(len(items)) + b''.join(items)


def module(result, import_module=None, import_name=None):
    """A guest exporting layerx_call/layerx_reserve/memory, optionally calling one host import."""
    types = [b'\x60\x02\x7f\x7f\x01\x7f', b'\x60\x01\x7f\x01\x7f',
             b'\x60\x04\x7f\x7f\x7f\x7f\x01\x7f']
    imported = 1 if import_module else 0
    out = b'\0asm\x01\0\0\0' + section(1, vector(types))
    if imported:
        out += section(2, vector([name(import_module) + name(import_name) + b'\x00\x02']))
    out += section(3, vector([b'\x00', b'\x01']))
    out += section(5, vector([b'\x00\x01']))
    out += section(7, vector([
        name('layerx_call') + b'\x00' + leb(imported),
        name('layerx_reserve') + b'\x00' + leb(imported + 1),
        name('memory') + b'\x02\x00']))
    if imported:
        call_body = b'\x00' + b'\x41\x00' * 4 + b'\x10\x00' + b'\x1a' + b'\x41' + leb(result) + b'\x0b'
    else:
        call_body = b'\x00\x41' + leb(result) + b'\x0b'
    reserve_body = b'\x00\x41\x00\x0b'
    out += section(10, vector([leb(len(call_body)) + call_body,
                               leb(len(reserve_body)) + reserve_body]))
    return out


def u16(value):
    return value.to_bytes(2, 'big')


def u32(value):
    return value.to_bytes(4, 'big')


def interface(code_hash, abi, calldata_bytes=64, capabilities=b'', capability_count=0):
    out = DOMAIN + code_hash + u16(abi) + u16(1)
    out += u16(11) + b'layerx_call' + b'\0\0\0\0'
    out += b'\x01\x20' + u32(calldata_bytes)
    out += b'\x01\x20' + u32(64)
    out += u16(capability_count) + capabilities
    out += u16(0) + u16(0)
    return out


def sha(data):
    return hashlib.sha256(data).digest()


def deploy(program, abi, wasm, encoding, code_hash=None):
    code_hash = code_hash or sha(wasm)
    return (program + u16(abi) + b'\x01\x00' + AUTHORITY + code_hash +
            u32(len(wasm)) + u32(len(encoding)) + encoding + wasm)


def upgrade(program, abi, old_hash, wasm, encoding, breaking=False):
    return (program + u16(abi) + bytes([2 if breaking else 0]) + b'\x00' + old_hash +
            sha(wasm) + u16(0) + u32(len(wasm)) + u32(len(encoding)) + encoding + wasm)


class Plan:
    def __init__(self):
        self.lines = []
        self.cases = []

    def add(self, case, command, *args):
        label = 'c%03d' % len(self.lines)
        self.lines.append(' '.join([label, command] + [str(a) for a in args]))
        self.cases.append((label, case))

    def dispatch(self, case, slot, ordinal, expected, body):
        self.add(case, 'dispatch', slot, ordinal, expected, body.hex())

    def iface(self, case, slot, program, version, encoding):
        self.add(case, 'iface', program.hex(), slot, version,
                 encoding.hex() if encoding else 'absent')

    def life(self, case, slot, program, abi, version, code_hash):
        self.add(case, 'life', program.hex(), slot, abi, code_hash.hex() + str(version))


def build_plan():
    plan = Plan()
    plan.add('kernel', 'new', 0)
    storage = (b'layerx_v1', 'storage_read')
    web = (b'layerx_v4', 'web_read')
    oracle = (b'layerx_v3', 'oracle_read')
    for abi in (1, 2, 3, 4):
        program = bytes([0x10 + abi]) * 32
        plain = module(abi)
        reading = module(16 + abi, storage[0].decode(), storage[1])
        tag = 'abi%d' % abi
        # Refusals first: nothing may be persisted by a refused deploy.
        other = module(40 + abi)
        plan.dispatch(tag + ':deploy-wrong-interface-hash', 0, 1, 'CONTEXT_MISMATCH',
                      deploy(program, abi, plain, interface(sha(other), abi)))
        wrong_abi = 1 + abi % 4
        plan.dispatch(tag + ':deploy-interface-abi-mismatch', 0, 1, 'CONTEXT_MISMATCH',
                      deploy(program, abi, plain, interface(sha(plain), wrong_abi)))
        plan.dispatch(tag + ':deploy-unknown-interface-abi', 0, 1, 'VERSION_UNSUPPORTED',
                      deploy(program, abi, plain, interface(sha(plain), 5)))
        plan.dispatch(tag + ':deploy-undeclared-capability', 0, 1, 'NON_CANONICAL',
                      deploy(program, abi, reading, interface(sha(reading), abi)))
        plan.dispatch(tag + ':deploy-overdeclared-capability', 0, 1, 'NON_CANONICAL',
                      deploy(program, abi, plain,
                             interface(sha(plain), abi, capabilities=b'\x00', capability_count=1)))
        if abi < 4:
            forbidden = module(50 + abi, web[0].decode(), web[1])
            plan.dispatch(tag + ':deploy-unsupported-import', 0, 1, 'NON_CANONICAL',
                          deploy(program, abi, forbidden, interface(sha(forbidden), abi)))
        if abi < 3:
            forbidden = module(54 + abi, oracle[0].decode(), oracle[1])
            plan.dispatch(tag + ':deploy-unsupported-oracle-import', 0, 1, 'NON_CANONICAL',
                          deploy(program, abi, forbidden, interface(sha(forbidden), abi)))
        plan.iface(tag + ':refusals-persist-nothing', 0, program, 0, None)
        # Valid interface-bearing deploy that declares exactly the imported capability.
        deployed = interface(sha(reading), abi, capabilities=b'\x00', capability_count=1)
        plan.dispatch(tag + ':deploy-accepted', 0, 1, 'OK', deploy(program, abi, reading, deployed))
        plan.iface(tag + ':deploy-persisted-interface', 0, program, 1, deployed)
        plan.life(tag + ':deploy-lifecycle-abi', 0, program, abi, 1, sha(reading))
        # Capability widening is refused without the explicit breaking authorization.
        widened = module(60 + abi, storage[0].decode(), 'storage_write')
        widened_iface = interface(sha(widened), abi, capabilities=b'\x01', capability_count=1)
        plan.dispatch(tag + ':upgrade-capability-widening-refused', 0, 2, 'NON_CANONICAL',
                      upgrade(program, abi, sha(reading), widened, widened_iface))
        # Schema narrowing is a breaking change and needs the explicit flag.
        rebuilt = module(70 + abi, storage[0].decode(), storage[1])
        narrowed = interface(sha(rebuilt), abi, calldata_bytes=32,
                             capabilities=b'\x00', capability_count=1)
        plan.dispatch(tag + ':upgrade-breaking-schema-refused', 0, 2, 'NON_CANONICAL',
                      upgrade(program, abi, sha(reading), rebuilt, narrowed))
        plan.iface(tag + ':refused-upgrade-keeps-interface', 0, program, 1, deployed)
        plan.dispatch(tag + ':upgrade-unknown-abi', 0, 2, 'VERSION_UNSUPPORTED',
                      upgrade(program, 5, sha(reading), rebuilt, narrowed))
        plan.dispatch(tag + ':upgrade-wrong-interface-hash', 0, 2, 'CONTEXT_MISMATCH',
                      upgrade(program, abi, sha(reading), rebuilt,
                              interface(sha(reading), abi, capabilities=b'\x00',
                                        capability_count=1)))
        # Compatible upgrade: wider calldata, capability narrowed to none.
        compatible = module(80 + abi)
        compatible_iface = interface(sha(compatible), abi, calldata_bytes=96)
        plan.dispatch(tag + ':upgrade-compatible-narrowing', 0, 2, 'OK',
                      upgrade(program, abi, sha(reading), compatible, compatible_iface))
        plan.iface(tag + ':upgrade-persisted-interface', 0, program, 2, compatible_iface)
        plan.life(tag + ':upgrade-lifecycle-abi', 0, program, abi, 2, sha(compatible))
        # The narrowed contract is now binding: re-adding the capability is widening.
        plan.dispatch(tag + ':upgrade-rewidening-refused', 0, 2, 'NON_CANONICAL',
                      upgrade(program, abi, sha(compatible), rebuilt,
                              interface(sha(rebuilt), abi, calldata_bytes=96,
                                        capabilities=b'\x00', capability_count=1)))
        breaking = interface(sha(rebuilt), abi, calldata_bytes=32,
                             capabilities=b'\x00', capability_count=1)
        plan.dispatch(tag + ':upgrade-breaking-authorized', 0, 2, 'OK',
                      upgrade(program, abi, sha(compatible), rebuilt, breaking, breaking=True))
        plan.iface(tag + ':breaking-persisted-interface', 0, program, 3, breaking)
        plan.life(tag + ':breaking-lifecycle-abi', 0, program, abi, 3, sha(rebuilt))
        if abi > 1:
            plan.dispatch(tag + ':upgrade-downgrade-refused', 0, 2, 'VERSION_UNSUPPORTED',
                          upgrade(program, abi - 1, sha(rebuilt), compatible,
                                  interface(sha(compatible), abi - 1, calldata_bytes=96)))
    # Compatible cross-ABI upgrades under the central monotonic policy.
    program = bytes([0x30]) * 32
    current = module(90)
    encoding = interface(sha(current), 1)
    plan.dispatch('cross:deploy-abi1', 0, 1, 'OK', deploy(program, 1, current, encoding))
    for version, abi in enumerate((2, 3, 4), start=2):
        following = module(90 + abi)
        following_iface = interface(sha(following), abi)
        plan.dispatch('cross:upgrade-to-abi%d' % abi, 0, 2, 'OK',
                      upgrade(program, abi, sha(current), following, following_iface))
        plan.iface('cross:persisted-abi%d' % abi, 0, program, version, following_iface)
        plan.life('cross:lifecycle-abi%d' % abi, 0, program, abi, version, sha(following))
        current = following
    # Restart from a verified snapshot and keep consuming the persisted contracts.
    plan.add('restart:snapshot-restore', 'restart', 0, 1)
    for abi in (1, 2, 3, 4):
        program = bytes([0x10 + abi]) * 32
        rebuilt = module(70 + abi, 'layerx_v1', 'storage_read')
        breaking = interface(sha(rebuilt), abi, calldata_bytes=32,
                             capabilities=b'\x00', capability_count=1)
        plan.iface('restart:abi%d-interface' % abi, 1, program, 3, breaking)
        plan.life('restart:abi%d-lifecycle' % abi, 1, program, abi, 3, sha(rebuilt))
        widened = module(60 + abi, 'layerx_v1', 'storage_write')
        plan.dispatch('restart:abi%d-widening-refused' % abi, 1, 2, 'NON_CANONICAL',
                      upgrade(program, abi, sha(rebuilt), widened,
                              interface(sha(widened), abi, calldata_bytes=32,
                                        capabilities=b'\x01', capability_count=1)))
        after = module(100 + abi)
        after_iface = interface(sha(after), abi, calldata_bytes=64)
        plan.dispatch('restart:abi%d-compatible-upgrade' % abi, 1, 2, 'OK',
                      upgrade(program, abi, sha(rebuilt), after, after_iface))
        plan.iface('restart:abi%d-upgraded-interface' % abi, 1, program, 4, after_iface)
        plan.life('restart:abi%d-upgraded-lifecycle' % abi, 1, program, abi, 4, sha(after))
    return plan


def private_dir(variable):
    value = os.environ.get(variable)
    if not value:
        fail('missing configuration %s' % variable)
    path = Path(value)
    path.mkdir(mode=0o700, parents=True, exist_ok=True)
    if path.stat().st_mode & 0o077 or ROOT in path.resolve().parents:
        fail('%s must be a private directory outside the source tree' % variable)
    return path


def main():
    evidence = private_dir('PAXEER_X_EVIDENCE_DIR')
    manifest = os.environ.get('PAXEER_X_CANDIDATE_MANIFEST')
    if not manifest or not Path(manifest).is_file():
        fail('missing candidate manifest PAXEER_X_CANDIDATE_MANIFEST')
    revision = subprocess.run(['git', 'rev-parse', 'HEAD'], cwd=ROOT, check=True,
                              capture_output=True, text=True).stdout.strip()
    for artifact, sources in ((KERNEL_LIB, KERNEL_SOURCES), (RUNTIME_LIB, SOURCES)):
        if not artifact.is_file():
            fail('missing candidate artifact %s' % artifact.relative_to(ROOT))
        built = artifact.stat().st_mtime
        stale = [s for s in sources if (ROOT / s).stat().st_mtime > built]
        if stale:
            fail('candidate artifact %s is older than %s' % (artifact.relative_to(ROOT), stale))
    plan = build_plan()
    with tempfile.TemporaryDirectory(prefix='programs-native-interfaces-') as scratch:
        source = Path(scratch) / 'driver.c'
        binary = Path(scratch) / 'driver'
        source.write_text(DRIVER)
        compiler = os.environ.get('CC', 'cc')
        command = [compiler, '-std=c17', '-O1', '-Wall', '-Wextra', '-Werror',
                   '-I' + str(ROOT / 'include'), '-I' + str(ROOT / 'build' / 'generated'),
                   str(source), str(KERNEL_LIB), str(RUNTIME_LIB),
                   '-lcrypto', '-pthread', '-ldl', '-lm', '-o', str(binary)]
        compiled = subprocess.run(command, capture_output=True, text=True)
        if compiled.returncode != 0:
            fail('driver compilation failed:\n' + compiled.stderr[-4000:])
        run = subprocess.run([str(binary)], input='\n'.join(plan.lines) + '\n',
                             capture_output=True, text=True, timeout=1500)
    observed = {}
    for line in run.stdout.splitlines():
        parts = line.split()
        if len(parts) >= 2 and parts[0] in ('ok', 'fail'):
            observed[parts[1]] = (parts[0], ' '.join(parts[2:]))
    results = []
    for label, case in plan.cases:
        status, detail = observed.get(label, ('missing', ''))
        results.append({'case': case, 'status': status, 'detail': detail})
    passed = sum(1 for r in results if r['status'] == 'ok')
    stamp = datetime.datetime.now(datetime.timezone.utc).strftime('%Y%m%dT%H%M%S%fZ')
    record = evidence / ('programs-native-interfaces-%s.json' % stamp)
    payload = {
        'schema': 'paxeer-x.qualification.programs-native-interfaces.v1',
        'revision': revision,
        'command': 'timeout 30m python3 tools/qualification/paxeer-x/programs_native_interfaces.py',
        'driver_exit_code': run.returncode,
        'cases': results,
        'artifacts': {str(a.relative_to(ROOT)): hashlib.sha256(a.read_bytes()).hexdigest()
                      for a in (KERNEL_LIB, RUNTIME_LIB)},
        'stderr_tail': run.stderr[-2000:],
    }
    descriptor = os.open(record, os.O_WRONLY | os.O_CREAT | os.O_EXCL, stat.S_IRUSR | stat.S_IWUSR)
    with os.fdopen(descriptor, 'w') as stream:
        json.dump(payload, stream, indent=2)
    for result in results:
        print('%-5s %s %s' % (result['status'], result['case'], result['detail']))
    print('revision=%s evidence=%s' % (revision, record))
    print('cases=%d passed=%d skipped=0' % (len(results), passed))
    if run.returncode != 0 or passed != len(results):
        fail('%d of %d cases did not pass (driver exit %d)' %
             (len(results) - passed, len(results), run.returncode))
    print('programs_native_interfaces: PASSED')


if __name__ == '__main__':
    main()
