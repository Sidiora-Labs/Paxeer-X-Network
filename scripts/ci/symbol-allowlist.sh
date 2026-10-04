#!/bin/sh
set -eu

build_dir=${1:-build}
objects=$(find "$build_dir/obj/src/protocol" "$build_dir/obj/src/codec" \
    "$build_dir/obj/src/state" "$build_dir/obj/src/storage" \
    "$build_dir/obj/src/modules" "$build_dir/obj/src/sequencer" \
    -type f -name '*.o' 2>/dev/null || true)
[ -z "$objects" ] && exit 0

execution_objects=$(find "$build_dir/obj/src/protocol" \
    "$build_dir/obj/src/codec" "$build_dir/obj/src/state" \
    "$build_dir/obj/src/ledger" "$build_dir/obj/src/modules" \
    -type f -name '*.o' 2>/dev/null || true)
if [ -n "$execution_objects" ] && nm -u $execution_objects | \
    awk '{print $NF}' | \
    grep -E '^(time|clock_gettime|gettimeofday|rand|random|getenv|getpid|gethostname|stat|socket|connect|accept|bind|listen|send|sendto|recv|recvfrom|getaddrinfo|curl_easy_perform)(@.*)?$'; then
    echo "consensus object imports a forbidden operating-system symbol" >&2
    exit 1
fi

if find src/crypto -type f -name '*.c' -print0 | \
    xargs -0 grep -nE '(^|[^[:alnum:]_])memcmp[[:space:]]*\('; then
    echo "crypto source calls plain memcmp instead of lxp_ct_memcmp" >&2
    exit 1
fi

if [ -n "$objects" ] && nm -u $objects | awk '{print $NF}' | \
    grep -E '^sqlite3_'; then
    echo "consensus object imports forbidden SQLite projection symbols" >&2
    exit 1
fi

module_objects=$(find "$build_dir/obj/src/modules" -type f -name '*.o' \
    2>/dev/null || true)
if [ -n "$module_objects" ] && nm -u $module_objects | awk '{print $NF}' | \
    grep -E '^(lxp_apply_transfer|lxp_apply_transfer_set|lxp_balance_apply_leg)$'; then
    echo "module object imports a ledger balance-apply internal" >&2
    exit 1
fi

if grep -nE 'ctx_(set_)?balance|balance[[:space:]]*=' \
    include/layerx/lxp_module.h; then
    echo "module ABI exposes a balance-write capability" >&2
    exit 1
fi
