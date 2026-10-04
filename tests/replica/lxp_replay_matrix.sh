#!/bin/sh
set -eu

root=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
work="$root/build/replay-crossarch"
mkdir -p "$work"

sources="tests/test_replay_crossarch.c \
src/replica/lxp_replay_fixture.c src/protocol/lxp_activity.c \
src/codec/lxp_codec.c src/protocol/lxp_arena.c src/protocol/lxp_u128.c \
src/protocol/lxp_result.c src/crypto/lxp_hash.c src/crypto/lxp_ct.c \
src/protocol/lxp_protocol.c"
flags="-Iinclude -std=c17 -pedantic -Werror -Wall -Wextra -Wconversion \
-Wshadow -Wvla -fno-strict-aliasing -ffp-contract=off"

cd "$root"
for case_name in gcc-o0 gcc-o2 clang-o0 clang-o2; do
    case "$case_name" in
        gcc-o0) compiler=gcc-13; optimisation=-O0 ;;
        gcc-o2) compiler=gcc-13; optimisation=-O2 ;;
        clang-o0) compiler=clang-18; optimisation=-O0 ;;
        clang-o2) compiler=clang-18; optimisation=-O2 ;;
    esac
    command -v "$compiler" >/dev/null 2>&1 || {
        echo "required compiler unavailable: $compiler" >&2
        exit 1
    }
    $compiler $flags "$optimisation" $sources -o "$work/$case_name"
    "$work/$case_name" >"$work/$case_name.digest"
done

if command -v aarch64-linux-gnu-gcc >/dev/null 2>&1 &&
   command -v qemu-aarch64 >/dev/null 2>&1; then
    aarch64-linux-gnu-gcc $flags -O2 $sources -o "$work/aarch64-o2"
    qemu-aarch64 -L /usr/aarch64-linux-gnu "$work/aarch64-o2" \
        >"$work/aarch64-o2.digest"
elif command -v docker >/dev/null 2>&1; then
    docker run --rm --platform linux/amd64 \
        -v "$root:/src:ro" -v "$work:/out" \
        debian:bookworm-slim sh -ec '
            apt-get update >/dev/null
            DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends \
                gcc-aarch64-linux-gnu libc6-dev-arm64-cross qemu-user >/dev/null
            cd /src
            aarch64-linux-gnu-gcc -Iinclude -std=c17 -pedantic -Werror \
                -Wall -Wextra -Wconversion -Wshadow -Wvla \
                -fno-strict-aliasing -ffp-contract=off -O2 \
                tests/test_replay_crossarch.c \
                src/replica/lxp_replay_fixture.c src/protocol/lxp_activity.c \
                src/codec/lxp_codec.c src/protocol/lxp_arena.c \
                src/protocol/lxp_u128.c src/protocol/lxp_result.c \
                src/crypto/lxp_hash.c src/crypto/lxp_ct.c \
                src/protocol/lxp_protocol.c -o /out/aarch64-o2
            qemu-aarch64 -L /usr/aarch64-linux-gnu /out/aarch64-o2 \
                >/out/aarch64-o2.digest
        '
else
    echo "a real non-x86 replay runner requires qemu-aarch64 or docker" >&2
    exit 1
fi

for digest_file in "$work"/*.digest; do
    cmp "$work/gcc-o2.digest" "$digest_file" || {
        echo "replay byte divergence: $digest_file" >&2
        exit 1
    }
done
cat "$work/gcc-o2.digest"
