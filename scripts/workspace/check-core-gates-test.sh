#!/bin/sh
set -eu

cd "$(dirname "$0")/../.."
script=scripts/workspace/check-core-gates.sh
inventory=tools/workspace/core-gates.txt
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT HUP INT TERM

sh "$script" "$inventory" Makefile

grep -v '^test|' "$inventory" > "$work/without-test.txt"
if sh "$script" "$work/without-test.txt" Makefile >/dev/null 2>&1; then
    echo "check-core-gates accepted an inventory that omits the test target" >&2
    exit 1
fi

sed '/^test:/,/[^\\]$/d' Makefile > "$work/Makefile"
if sh "$script" "$inventory" "$work/Makefile" >/dev/null 2>&1; then
    echo "check-core-gates accepted a Makefile without the test target" >&2
    exit 1
fi

echo "check-core-gates regression: ok"
