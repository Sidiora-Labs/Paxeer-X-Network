#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/../../.."
: "${CAPS_BUILD_DIR:?private output directory required}"
: "${CAPS_NATIVE_STAGE_DIR:?own non-secret executable staging directory required}"
: "${CAPS_NATIVE_BIN_DIR:?compatible prebuilt layerxd and genesis builder directory required}"
: "${CAPS_NATIVE_SOURCE:?source checkout of compatible prebuilt native libraries required}"
: "${CARGO_TARGET_DIR:?unique task target directory required}"
: "${CAPS_RUST_TOOLCHAIN:?explicit Rust1.91.1 toolchain directory required}"
export RUSTC="$CAPS_RUST_TOOLCHAIN/bin/rustc"
export RUSTDOC="$CAPS_RUST_TOOLCHAIN/bin/rustdoc"
export CARGO_BUILD_JOBS=5
[[ "$("$RUSTC" --version)" == 'rustc 1.91.1 '* ]]
mkdir -p "$CAPS_BUILD_DIR"
chmod 700 "$CAPS_BUILD_DIR"
if [[ "${CAPS_BUILD_NATIVE:-0}" == 1 ]]; then
    bash tools/bringup/build-foundation-artifacts.sh --output "$CAPS_NATIVE_BIN_DIR"
fi
python3 - <<'PY'
import os,subprocess,pathlib,json,hashlib,shutil
root=pathlib.Path.cwd(); source=pathlib.Path(os.environ['CAPS_NATIVE_SOURCE']); out=pathlib.Path(os.environ['CAPS_BUILD_DIR'])
def git(where,*args):return subprocess.check_output(['git','-C',str(where),*args],text=True).strip()
def sha(p):return hashlib.file_digest(open(p,'rb'),'sha256').hexdigest()
for tree in ['src','include','programs','cmd','migrations','contracts/config']:
 if git(root,'rev-parse','HEAD:'+tree)!=git(source,'rev-parse','HEAD:'+tree):raise SystemExit('incompatible native source tree: '+tree)
 subprocess.run(['git','-C',str(source),'diff','--exit-code','HEAD','--',tree],check=True,stdout=subprocess.DEVNULL)
paths=[source/'build/liblayerx.a',source/'programs/target/debug/liblayerx_programs_sandbox.a']
provenance={'revision':git(root,'rev-parse','HEAD'),'native_revision':git(source,'rev-parse','HEAD'),'native_source_trees':{t:git(root,'rev-parse','HEAD:'+t) for t in ['src','include','programs','cmd','migrations','contracts/config']},'native_libraries':{str(p):sha(p) for p in paths},'rustc':subprocess.check_output([os.environ['RUSTC'],'--version'],text=True).strip()}
native=pathlib.Path(os.environ['CAPS_NATIVE_STAGE_DIR']).resolve()
if native == pathlib.Path(os.environ['CAPS_NATIVE_BIN_DIR']).resolve():raise SystemExit('cannot stage over producer binaries')
native.mkdir(parents=True,exist_ok=True);native.chmod(0o755)
provenance['authority_binaries']={}
for name in ['layerxd','layerx-genesis-build']:
 original=pathlib.Path(os.environ['CAPS_NATIVE_BIN_DIR'])/name
 target=native/name
 shutil.copy2(original,target)
 target.chmod(0o755)
 provenance['authority_binaries'][name]={'path':str(target),'sha256':sha(target)}
(out/'native-source.json').write_text(json.dumps(provenance,indent=2)+'\n')
PY
if [[ -n "${CAPS_REUSE_MANIFEST:-}" ]]; then
python3 - <<'REUSE'
import os,pathlib,json,hashlib,subprocess
out=pathlib.Path(os.environ['CAPS_BUILD_DIR'])
old=json.loads(pathlib.Path(os.environ['CAPS_REUSE_MANIFEST']).read_text())
def tree(t):return subprocess.check_output(['git','rev-parse','HEAD:'+t],text=True).strip()
for t in ['agent','programs','src','include','tests/qualification']:
 if old['source_trees'][t]!=tree(t):raise SystemExit('compiled source changed: '+t)
for artifact in old['artifacts'].values():
 with open(artifact['path'],'rb') as f:digest=hashlib.file_digest(f,'sha256').hexdigest()
 if digest!=artifact['sha256']:raise SystemExit('compiled artifact changed')
m=json.loads((out/'native-source.json').read_text())
m['artifacts']=old['artifacts'];m['reuse_from_revision']=old['revision']
m['source_trees']={t:tree(t) for t in old['source_trees']}
(out/'manifest.json').write_text(json.dumps(m,indent=2)+'\n')
os.chmod(out/'manifest.json',0o600)
print('Reused source-identical compiled artifacts; no compilation performed')
REUSE
exit 0
fi
cc -std=c17 -pedantic -Werror -Wall -Wextra -Wconversion -Wshadow -Wvla -fno-strict-aliasing -ffp-contract=off -O2 -ffunction-sections -fdata-sections -Iinclude -I"$CAPS_NATIVE_SOURCE/build/generated" tests/qualification/lxp_wallet_caps_vectors.c "$CAPS_NATIVE_SOURCE/build/liblayerx.a" "$CAPS_NATIVE_SOURCE/programs/target/debug/liblayerx_programs_sandbox.a" "$CAPS_NATIVE_SOURCE/build/liblayerx.a" -Wl,--gc-sections -lcrypto -pthread -ldl -lm -o "$CAPS_BUILD_DIR/lxp_wallet_caps_vectors"
"$CAPS_RUST_TOOLCHAIN/bin/cargo" test --manifest-path agent/Cargo.toml --offline --locked -p layerx-client --test committed_caps --no-run --message-format=json > "$CAPS_BUILD_DIR/client-build.jsonl"
"$CAPS_RUST_TOOLCHAIN/bin/cargo" test --manifest-path agent/Cargo.toml --offline --locked -p layerx-agentd --test budget_schema --no-run --message-format=json > "$CAPS_BUILD_DIR/agentd-build.jsonl"
python3 - <<'PY'
import os,pathlib,json,hashlib,subprocess
out=pathlib.Path(os.environ['CAPS_BUILD_DIR'])
def sha(p):return hashlib.file_digest(open(p,'rb'),'sha256').hexdigest()
artifacts={'native':str(out/'lxp_wallet_caps_vectors')}
for key,file,name in [('client','client-build.jsonl','committed_caps'),('agentd','agentd-build.jsonl','budget_schema')]:
 matches=[r['executable'] for line in (out/file).read_text().splitlines() if (r:=json.loads(line)).get('reason')=='compiler-artifact' and r.get('target',{}).get('name')==name and r.get('executable')]
 if len(matches)!=1:raise SystemExit('missing or ambiguous prebuilt executable: '+name)
 artifacts[key]=matches[0]
m=json.loads((out/'native-source.json').read_text());m['artifacts']={k:{'path':p,'sha256':sha(p)} for k,p in artifacts.items()};m['source_trees']={t:subprocess.check_output(['git','rev-parse','HEAD:'+t],text=True).strip() for t in ['agent','programs','src','include','tests/qualification','tools/qualification/paxeer-x','tools/paxeer-x/gates']}
(out/'manifest.json').write_text(json.dumps(m,indent=2)+'\n')
os.chmod(out/'manifest.json',0o600)
PY
