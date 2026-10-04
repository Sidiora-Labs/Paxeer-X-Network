#!/bin/sh
set -eu

writers=$(rg -l -- '->balance[[:space:]]*=' src || true)
if [ "$writers" != "src/ledger/lxp_apply.c" ]; then
    echo "ERR_BALANCE_BYPASS: lx_account.balance writers: $writers" >&2
    exit 1
fi

if rg -n 'lxp_(apply_transfer|balance_apply_leg|balance_restore_snapshot)' \
    src/modules >/dev/null 2>&1; then
    echo "ERR_BALANCE_BYPASS: module links an internal ledger writer" >&2
    exit 1
fi

if rg -n 'lxp_(apply_transfer|balance_apply_leg)|balance_(set|write)' \
    include/layerx/lxp_module.h src/protocol/lxp_module_ctx.c >/dev/null 2>&1; then
    echo "ERR_BALANCE_BYPASS: module context exposes a balance writer" >&2
    exit 1
fi

exit 0
