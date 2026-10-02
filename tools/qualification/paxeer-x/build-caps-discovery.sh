#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/../../.."
: "${CAPS_DISCOVERY_BUILD_DIR:?private absolute output directory required}"
: "${CAPS_RUST_TOOLCHAIN:?explicit Rust toolchain directory required}"
: "${CARGO_TARGET_DIR:?private Rust target directory required}"
export RUSTC="$CAPS_RUST_TOOLCHAIN/bin/rustc"
export RUSTDOC="$CAPS_RUST_TOOLCHAIN/bin/rustdoc"
export CARGO_BUILD_JOBS=5
umask 077
python3 - <<'PREPARE'
import os,pathlib,subprocess
out=pathlib.Path(os.environ['CAPS_DISCOVERY_BUILD_DIR'])
if not out.is_absolute() or out.is_symlink():raise SystemExit('private absolute build output required')
if subprocess.check_output(['git','status','--porcelain']):raise SystemExit('clean committed source required')
out.mkdir(parents=True,exist_ok=True);out.chmod(0o700)
if (out/'manifest.json').exists():raise SystemExit('refuse reusing task artifact manifest')
PREPARE
make -j5 BUILD_DIR="$CAPS_DISCOVERY_BUILD_DIR/native" PROGRAMS_CARGO="$CAPS_RUST_TOOLCHAIN/bin/cargo" LXP_REVISION="$(git rev-parse HEAD)" layerxd > "$CAPS_DISCOVERY_BUILD_DIR/native-build.log" 2>&1
cc -std=c17 -pedantic -Werror -Wall -Wextra -Wconversion -Wshadow -Wvla -fno-strict-aliasing -ffp-contract=off -O2 -ffunction-sections -fdata-sections -Iinclude -I"$CAPS_DISCOVERY_BUILD_DIR/native/generated" tests/qualification/lxp_wallet_caps_discovery.c "$CAPS_DISCOVERY_BUILD_DIR/native/liblayerx.a" programs/target/debug/liblayerx_programs_sandbox.a "$CAPS_DISCOVERY_BUILD_DIR/native/liblayerx.a" -Wl,--gc-sections -lcrypto -lsqlite3 -pthread -ldl -lm -o "$CAPS_DISCOVERY_BUILD_DIR/lxp_wallet_caps_discovery" > "$CAPS_DISCOVERY_BUILD_DIR/fixture-build.log" 2>&1
"$CAPS_RUST_TOOLCHAIN/bin/cargo" test --locked --manifest-path agent/Cargo.toml -p layerx-client --test caps_discovery --no-run --message-format=json > "$CAPS_DISCOVERY_BUILD_DIR/client-build.jsonl" 2> "$CAPS_DISCOVERY_BUILD_DIR/client-build.log"
python3 - <<'MANIFEST'
import hashlib,json,os,pathlib,subprocess
out=pathlib.Path(os.environ['CAPS_DISCOVERY_BUILD_DIR'])
rows=[json.loads(line) for line in (out/'client-build.jsonl').read_text().splitlines()]
paths=[r['executable'] for r in rows if r.get('reason')=='compiler-artifact' and r.get('target',{}).get('name')=='caps_discovery' and r.get('executable')]
if len(paths)!=1:raise SystemExit('missing or ambiguous Rust caps test executable')
def git(*args):return subprocess.check_output(['git',*args],text=True).strip()
def artifact(path):
 path=pathlib.Path(path).resolve()
 with path.open('rb') as stream: digest=hashlib.file_digest(stream,'sha256').hexdigest()
 return {'path':str(path),'sha256':digest}
if git('status','--porcelain'):raise SystemExit('source changed during build')
manifest={'version':1,'purpose':'native-complete-caps-discovery','revision':git('rev-parse','HEAD'),'source_tree':git('rev-parse','HEAD^{tree}'),'artifacts':{'layerxd':artifact(out/'native/bin/layerxd'),'native':artifact(out/'lxp_wallet_caps_discovery'),'rust':artifact(paths[0])},'build_command':'timeout 15m bash tools/qualification/paxeer-x/build-caps-discovery.sh','build_logs':['native-build.log','fixture-build.log','client-build.log']}
(out/'manifest.json').write_text(json.dumps(manifest,indent=2)+'\n');(out/'manifest.json').chmod(0o600)
MANIFEST
