#!/bin/sh
set -eu

root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
build_dir=${1:-"$root/build"}
tool_root=${LXP_QUAL_TOOL_ROOT:-"$root/build/qualification/toolchain/root"}
proof_dir="$build_dir/qualification/arith-proof"

resolve_tool()
{
    name=$1
    local_path=$2
    if command -v "$name" >/dev/null 2>&1; then
        command -v "$name"
    elif [ -x "$local_path" ]; then
        printf '%s\n' "$local_path"
    else
        printf 'required qualification tool is unavailable: %s\n' "$name" >&2
        exit 1
    fi
}

cbmc=$(resolve_tool cbmc "$tool_root/usr/bin/cbmc")
z3=$(resolve_tool z3 "$tool_root/usr/bin/z3")
clang=$(resolve_tool clang "$tool_root/usr/lib/llvm-18/bin/clang")
cbmc_library="$tool_root/usr/lib"
clang_resource=/usr/lib/llvm-18/lib/clang/18

if [ ! -d "$clang_resource" ]; then
    clang_resource=$($clang -print-resource-dir)
fi

mkdir -p "$proof_dir"

proof_sources="tests/qualification/lxp_qual_arith.c \
src/protocol/lxp_u128.c src/protocol/lxp_u256.c \
src/protocol/lxp_i128.c src/protocol/lxp_result.c"

for proof in lxp_cbmc_u128_add_sub lxp_cbmc_u256_add; do
    report="$proof_dir/$proof.txt"
    LD_LIBRARY_PATH="$cbmc_library${LD_LIBRARY_PATH:+:$LD_LIBRARY_PATH}" \
        "$cbmc" $proof_sources -I include -D__CPROVER \
        --function "$proof" --bounds-check --pointer-check \
        --signed-overflow-check --undefined-shift-check --div-by-zero-check \
        --unwind 130 --unwinding-assertions >"$report"
    if ! grep -q '^VERIFICATION SUCCESSFUL$' "$report"; then
        printf 'CBMC proof failed: %s\n' "$proof" >&2
        exit 1
    fi
    printf 'cbmc_proof=%s status=verified\n' "$proof"
done

smt_report="$proof_dir/lxp_arith_smt.txt"
"$z3" -smt2 tools/proofs/lxp_arith.smt2 >"$smt_report"
if grep -Eq '^(sat|unknown|\(error)' "$smt_report" ||
   [ "$(grep -c '^unsat$' "$smt_report")" -ne 5 ]; then
    printf '%s\n' "SMT arithmetic proof failed" >&2
    exit 1
fi
printf '%s\n' "smt_obligations=5 status=verified"

if grep -RInE 'no_sanitize|disable_sanitizer_instrumentation|__lsan_ignore|sanitizer_(black|ignore)list' \
    include/layerx src tests/qualification tests/test_qual_arith.c; then
    printf '%s\n' "sanitizer suppression or disabled instrumentation detected" >&2
    exit 1
fi

common_flags="-resource-dir $clang_resource -Iinclude -std=c17 -pedantic \
-Werror -Wall -Wextra -Wconversion -Wshadow -Wvla -fno-strict-aliasing \
-ffp-contract=off -O1 -g -fno-omit-frame-pointer -fno-optimize-sibling-calls \
-fPIE -pie"
test_sources="tests/test_qual_arith.c tests/qualification/lxp_qual_arith.c \
src/protocol/lxp_u128.c src/protocol/lxp_u256.c \
src/protocol/lxp_i128.c src/protocol/lxp_result.c"

build_and_run()
{
    name=$1
    sanitizer=$2
    options_name=$3
    options_value=$4
    executable="$proof_dir/test_qual_arith_$name"
    "$clang" $common_flags -fsanitize="$sanitizer" $test_sources \
        -fsanitize="$sanitizer" -o "$executable"
    env "$options_name=$options_value" "$executable" \
        >"$proof_dir/$name.txt"
    printf 'sanitizer=%s status=clean\n' "$name"
}

build_and_run address address ASAN_OPTIONS \
    abort_on_error=1:detect_leaks=1:strict_string_checks=1
build_and_run undefined undefined UBSAN_OPTIONS halt_on_error=1:print_stacktrace=1
build_and_run thread thread TSAN_OPTIONS halt_on_error=1

msan_executable="$proof_dir/test_qual_arith_memory"
"$clang" $common_flags -fsanitize=memory -fsanitize-memory-track-origins=2 \
    $test_sources -fsanitize=memory -o "$msan_executable"
MSAN_OPTIONS=halt_on_error=1:exit_code=99:poison_in_dtor=1 \
    "$msan_executable" >"$proof_dir/memory.txt"
printf '%s\n' "sanitizer=memory status=clean"

build_and_run leak leak LSAN_OPTIONS exitcode=99:report_objects=1

scripts/ci/no-float-scan.sh "$build_dir"
printf '%s\n' "arithmetic proof and sanitizer qualification complete"
