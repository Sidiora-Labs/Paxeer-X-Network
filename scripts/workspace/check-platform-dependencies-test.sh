#!/bin/sh
set -eu

cd "$(dirname "$0")/../.."
script=scripts/workspace/check-platform-dependencies.sh
inventory=tools/workspace/platform-dependency-roots.txt
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT HUP INT TERM

sh "$script" "$inventory"

grep -v '^rust|services/platform/hosted/core/Cargo.toml|' "$inventory" > "$work/missing-root.txt"
if sh "$script" "$work/missing-root.txt" >/dev/null 2>&1; then
    echo "check-platform-dependencies accepted an inventory that omits a manifest" >&2
    exit 1
fi

cp "$inventory" "$work/extra-root.txt"
echo 'rust|services/platform/hosted/absent/Cargo.toml|covered-by-platform-cargo' >> "$work/extra-root.txt"
if sh "$script" "$work/extra-root.txt" >/dev/null 2>&1; then
    echo "check-platform-dependencies accepted an inventory naming an absent manifest" >&2
    exit 1
fi

sed 's#^npm|services/platform/middleware/agent/package.json|covered-by-root-npm$#npm|services/platform/middleware/agent/package.json|locked-npm-independent#' "$inventory" > "$work/unlocked-npm.txt"
if sh "$script" "$work/unlocked-npm.txt" >/dev/null 2>&1; then
    echo "check-platform-dependencies accepted a locked npm root without package-lock.json" >&2
    exit 1
fi

echo "check-platform-dependencies regression: ok"
