#!/bin/sh
set -u

status=0
for optimisation in -O0 -O2; do
    suffix=$(printf '%s' "$optimisation" | tr -d '-')
    if ! make --no-print-directory BUILD_DIR="build-$suffix" \
        OPT_LEVEL="$optimisation" test; then
        status=1
    fi
done
exit "$status"
