#!/bin/sh
set -eu

repo_root=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
metadata_file=$(mktemp)
license_file=$(mktemp)
trap 'rm -f -- "$metadata_file" "$license_file"' EXIT HUP INT TERM

cargo metadata --manifest-path "$repo_root/interop/Cargo.toml" \
    --locked --format-version 1 >"$metadata_file"

banned='^(bindgen|libsqlite3-sys|rusqlite|sqlx-sqlite|layerx-core|layerx-protocol-core|layerx-human-service|layerx-paxeer-client|layerx-explorer-index|ctor|inventory)$'
if jq -r '.packages[].name' "$metadata_file" | grep -Eq "$banned"; then
    echo "interop dependency policy: forbidden boundary or global-initialisation crate" >&2
    exit 1
fi

allowlisted_license_token() {
    case "$1" in
        Apache-2.0|BSD-1-Clause|BSD-2-Clause|BSD-3-Clause|CC0-1.0|ISC|MIT|Unicode-3.0|Zlib|LLVM-exception)
            return 0
            ;;
        *)
            return 1
            ;;
    esac
}

jq -r '.packages[] | select(.source != null) | [.name, (.license // "")] | @tsv' \
    "$metadata_file" >"$license_file"
while IFS="$(printf '\t')" read -r package license; do
    if [ -z "$license" ]; then
        echo "interop dependency policy: $package has no SPDX license" >&2
        exit 1
    fi
    case "$license" in
        *" AND "*)
            tokens=$(printf '%s\n' "$license" | sed 's/[()]/ /g; s|/| |g; s/ AND / /g; s/ OR / /g; s/ WITH / /g')
            for token in $tokens; do
                if ! allowlisted_license_token "$token"; then
                    echo "interop dependency policy: $package uses non-allowlisted license $license" >&2
                    exit 1
                fi
            done
            ;;
        *)
            alternatives=$(printf '%s\n' "$license" | sed 's/[()]//g; s|/|\n|g; s/ OR /\n/g')
            satisfied=0
            while IFS= read -r alternative; do
                [ -n "$alternative" ] || continue
                alternative_ok=1
                for token in $(printf '%s\n' "$alternative" | sed 's/ WITH / /g'); do
                    if ! allowlisted_license_token "$token"; then
                        alternative_ok=0
                    fi
                done
                if [ "$alternative_ok" -eq 1 ]; then
                    satisfied=1
                fi
            done <<LICENSE_ALTERNATIVES
$alternatives
LICENSE_ALTERNATIVES
            if [ "$satisfied" -ne 1 ]; then
                echo "interop dependency policy: $package uses non-allowlisted license $license" >&2
                exit 1
            fi
            ;;
    esac
done <"$license_file"

if command -v rg >/dev/null 2>&1; then
    unsafe_hits=$(rg -n --glob '*.rs' '\bunsafe\s+(fn|trait|impl|extern)|\bunsafe\s*\{' "$repo_root/interop/crates" || true)
else
    unsafe_hits=$(grep -rEn --include='*.rs' '\bunsafe[[:space:]]+(fn|trait|impl|extern)|\bunsafe[[:space:]]*\{' "$repo_root/interop/crates" || true)
fi
if [ -n "$unsafe_hits" ]; then
    printf '%s\n' "$unsafe_hits" >&2
    echo "interop unsafe policy: unsafe code is forbidden" >&2
    exit 1
fi

echo "interop dependency and unsafe policies passed"
