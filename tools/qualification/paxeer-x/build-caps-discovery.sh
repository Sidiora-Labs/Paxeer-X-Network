#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/../../.."
: "${CARGO_TARGET_DIR:?unique task target directory required}"
: "${CAPS_RUST_TOOLCHAIN:?explicit Rust1.91.1 toolchain directory required}"
export RUSTC="$CAPS_RUST_TOOLCHAIN/bin/rustc"
export RUSTDOC="$CAPS_RUST_TOOLCHAIN/bin/rustdoc"
export CARGO_BUILD_JOBS=5
[[ "$("$RUSTC" --version)" == 'rustc 1.91.1 '* ]]
[[ -z "$(git status --porcelain)" ]] || { echo 'build-caps-discovery: dirty source tree' >&2; exit 1; }
revision="$(git rev-parse HEAD)"
out="$(realpath -m "$CARGO_TARGET_DIR/../caps-discovery")"
native_build="$out/native"
mkdir -p "$native_build"
chmod 700 "$out"
make -j5 BUILD_DIR="$native_build" LXP_REVISION="$revision" "$native_build/bin/layerxd" "$native_build/liblayerx.a" programs-build
cc -std=c17 -O2 -ffunction-sections -fdata-sections -Iinclude -Itests/daemon -I"$native_build/generated" \
    tests/qualification/lxp_wallet_caps_discovery.c -Wl,--gc-sections -Wl,--start-group "$native_build/liblayerx.a" \
    programs/target/debug/liblayerx_programs_sandbox.a -Wl,--end-group -lcrypto -lsqlite3 -pthread -ldl -lm \
    -o "$out/lxp_wallet_caps_discovery"
"$CAPS_RUST_TOOLCHAIN/bin/cargo" test --locked --offline --manifest-path agent/Cargo.toml -p layerx-client \
    --test caps_discovery --no-run --message-format=json > "$out/client-build.jsonl"
[[ "$(git rev-parse HEAD)" == "$revision" && -z "$(git status --porcelain)" ]] || { echo 'build-caps-discovery: source changed during build' >&2; exit 1; }
CAPS_OUT="$out" CAPS_REVISION="$revision" python3 - <<'PY'
import hashlib, json, os, pathlib, subprocess
out = pathlib.Path(os.environ['CAPS_OUT'])
def sha(path):
    with open(path, 'rb') as stream:
        return hashlib.file_digest(stream, 'sha256').hexdigest()
matches = [row['executable'] for line in (out / 'client-build.jsonl').read_text().splitlines()
           if (row := json.loads(line)).get('reason') == 'compiler-artifact'
           and row.get('target', {}).get('name') == 'caps_discovery' and row.get('executable')]
if len(matches) != 1:
    raise SystemExit('missing or ambiguous prebuilt executable: caps_discovery')
artifacts = {'layerxd': str(out / 'native/bin/layerxd'), 'fixture': str(out / 'lxp_wallet_caps_discovery'), 'client': matches[0]}
trees = ['agent', 'cmd', 'src', 'include', 'programs', 'tests/daemon', 'tests/qualification', 'tools/qualification/paxeer-x', 'tools/paxeer-x/gates']
manifest = {'version': 1, 'revision': os.environ['CAPS_REVISION'],
            'source_trees': {t: subprocess.check_output(['git', 'rev-parse', 'HEAD:' + t], text=True).strip() for t in trees},
            'artifacts': {name: {'path': path, 'sha256': sha(path)} for name, path in artifacts.items()},
            'rustc': subprocess.check_output([os.environ['RUSTC'], '--version'], text=True).strip()}
path = out / 'manifest.json'
path.write_text(json.dumps(manifest, indent=2) + '\n')
path.chmod(0o600)
print('caps-discovery manifest ' + str(path))
PY
