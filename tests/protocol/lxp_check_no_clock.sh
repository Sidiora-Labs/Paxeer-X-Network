#!/bin/sh
set -eu

if rg -n --glob '*.c' \
    '(^|[^[:alnum:]_])(time|gettimeofday|clock_gettime|rand)[[:space:]]*\(' \
    src/protocol src/state src/ledger src/modules; then
    echo "consensus transition source references a forbidden clock or entropy API" >&2
    exit 1
fi

if rg -n --glob '*.c' '#[[:space:]]*include[[:space:]]*<[[:space:]]*time\.h[[:space:]]*>' \
    src/protocol src/state src/ledger src/modules; then
    echo "consensus transition source includes the operating-system clock API" >&2
    exit 1
fi
