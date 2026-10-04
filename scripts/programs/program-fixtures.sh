#!/bin/sh
# Rebuilds the committed reference program artifacts and every interface and
# registry state value bound to them, and is the only place that sets the
# source-path remapping they are built with.
#
# rustc records the source file of every panic location it keeps, so a build
# that does not remap its paths writes the absolute path of the checkout it ran
# in into the WebAssembly it produces. The remapping maps the checkout onto
# /layerx/source, the virtual source root the hosted registry builder replays
# against, so the artifacts carry no checkout path from any checkout. Rerunning
# this script over the same checkout reproduces them byte for byte. A run from a
# differently located checkout reorders functions without changing a byte of
# their bodies, because cargo derives the crate disambiguator of every path
# package from that package's absolute path and only a build at a fixed source
# root - what the hosted registry builder gets from its bind mount - holds it
# still; the interface and the registry state value are therefore regenerated
# from the artifact in the same run, so the fixture set stays bound together.
#
# The remapping is passed as target.wasm32-unknown-unknown.rustflags and not as
# RUSTFLAGS, because RUSTFLAGS would also reach the host builds of the lint tool
# and of the interface generators below. Cargo replaces, rather than extends,
# that table, so the two codegen flags programs/.cargo/config.toml declares for
# the target are repeated here and have to stay in step with it. Any RUSTFLAGS
# the caller exports would take precedence over the table and is therefore
# cleared.
set -eu

root_dir=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
programs_dir="$root_dir/programs"
target=wasm32-unknown-unknown
CARGO=${CARGO:-cargo}
program_id=5555555555555555555555555555555555555555555555555555555555555555
target_dir=${CARGO_TARGET_DIR:-$programs_dir/target/program-fixtures}
case "$target_dir" in
    /*) ;;
    *) target_dir="$root_dir/$target_dir" ;;
esac

unset RUSTFLAGS CARGO_ENCODED_RUSTFLAGS
CARGO_TARGET_WASM32_UNKNOWN_UNKNOWN_RUSTFLAGS="-C target-feature=-reference-types -C link-arg=--compress-relocations --remap-path-prefix=$root_dir=/layerx/source"
export CARGO_TARGET_WASM32_UNKNOWN_UNKNOWN_RUSTFLAGS

build_program() {
    # build_program EXAMPLE ARTIFACT_STEM FIXTURE
    example=$programs_dir/sdk/rust/examples/$1
    artifact=$target_dir/$target/release/$2.wasm
    (cd "$example" && CARGO_TARGET_DIR="$target_dir" \
        "$CARGO" build --locked --release --target "$target")
    test -s "$artifact"
    if [ -f "$example/layerx-program.json" ]; then
        (cd "$programs_dir" && "$CARGO" run --locked --quiet \
            -p layerx-program-lint --bin layerx-program-lint -- "$example" "$artifact")
    fi
    cp -- "$artifact" "$3"
}

generate_interface() {
    # generate_interface GENERATOR WASM INTERFACE [STATE_VALUE]
    if [ "$#" -eq 4 ]; then
        (cd "$programs_dir" && "$CARGO" run --locked --quiet \
            -p layerx-programs-registry --example "$1" -- "$2" "$program_id" "$3" "$4")
    else
        (cd "$programs_dir" && "$CARGO" run --locked --quiet \
            -p layerx-programs-registry --example "$1" -- "$2" "$program_id" "$3")
    fi
}

naming=$programs_dir/crates/layerx-programs-registry/tests/fixtures/naming
lxt721=$programs_dir/crates/layerx-programs-registry/tests/fixtures/lxt721
pay5=$programs_dir/fixtures/pay5

build_program naming layerx_reference_naming "$naming/naming.wasm"
generate_interface naming_interface "$naming/naming.wasm" "$naming/naming.interface"

build_program nft-lxt721 layerx_nft_lxt721 "$lxt721/nft-lxt721.wasm"
generate_interface lxt721_interface "$lxt721/nft-lxt721.wasm" \
    "$lxt721/nft-lxt721.interface" "$lxt721/nft-lxt721.registry-value"

build_program swap-cpmm layerx_swap_cpmm "$pay5/swap-cpmm.wasm"
generate_interface swap_interface "$pay5/swap-cpmm.wasm" \
    "$pay5/swap-cpmm.interface" "$pay5/swap-cpmm.registry-value"

for fixture in "$naming/naming.wasm" "$lxt721/nft-lxt721.wasm" "$pay5/swap-cpmm.wasm"; do
    if grep -qaF -- "$root_dir" "$fixture"; then
        echo "program fixtures: $fixture carries a checkout path" >&2
        exit 1
    fi
done
sha256sum -- "$naming/naming.wasm" "$naming/naming.interface" \
    "$lxt721/nft-lxt721.wasm" "$lxt721/nft-lxt721.interface" \
    "$lxt721/nft-lxt721.registry-value" "$pay5/swap-cpmm.wasm" \
    "$pay5/swap-cpmm.interface" "$pay5/swap-cpmm.registry-value"
