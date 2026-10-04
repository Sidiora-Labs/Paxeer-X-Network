#!/usr/bin/env bash
set -euo pipefail
umask 077
root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)
cd "$root"
: "${LAYERX_MULTI_ASSET_BUILD_MANIFEST:?private source-bound build manifest required}"
case "${1:-verify}" in
build)
    : "${LAYERX_MULTI_ASSET_CORE_TARGET_DIR:?existing private core target cache required}"
    : "${LAYERX_MULTI_ASSET_PROBE_TARGET_DIR:?existing private probe target cache required}"
    python3 - <<'PREPARE'
import os, stat, subprocess
from pathlib import Path
assert not subprocess.check_output(['git','status','--porcelain','--untracked-files=all']), 'complete clean candidate required'
for name in ('LAYERX_MULTI_ASSET_CORE_TARGET_DIR','LAYERX_MULTI_ASSET_PROBE_TARGET_DIR'):
    path=Path(os.environ[name]);info=path.stat()
    assert path.is_absolute() and not any(p.is_symlink() for p in (path,*path.parents))
    assert stat.S_ISDIR(info.st_mode) and info.st_uid==os.geteuid() and not info.st_mode&0o077, 'private existing target cache required'
path=Path(os.environ['LAYERX_MULTI_ASSET_BUILD_MANIFEST'])
assert path.is_absolute() and Path.cwd() not in path.parents and not path.exists() and not any(p.is_symlink() for p in (path,*path.parents))
assert path.parent.stat().st_uid==os.geteuid() and not path.parent.stat().st_mode&0o077
PREPARE
    export LAYERX_MULTI_ASSET_BUILD_REVISION=$(git rev-parse HEAD)
    export LAYERX_MULTI_ASSET_CORE_EVENTS="${LAYERX_MULTI_ASSET_BUILD_MANIFEST}.core.jsonl"
    export LAYERX_MULTI_ASSET_PROBE_EVENTS="${LAYERX_MULTI_ASSET_BUILD_MANIFEST}.probe.jsonl"
    CARGO_TARGET_DIR="$LAYERX_MULTI_ASSET_CORE_TARGET_DIR" cargo test --locked --manifest-path platform/Cargo.toml -p layerx-platform-core --test custody_asset_send --no-run --message-format=json > "$LAYERX_MULTI_ASSET_CORE_EVENTS"
    CARGO_TARGET_DIR="$LAYERX_MULTI_ASSET_PROBE_TARGET_DIR" cargo build --locked --release --manifest-path platform/hosted/node/tests/probe/Cargo.toml --message-format=json > "$LAYERX_MULTI_ASSET_PROBE_EVENTS"
    python3 - <<'RECORD'
import hashlib,json,os,subprocess
from pathlib import Path
def git(*args):return subprocess.check_output(['git',*args]).decode().strip()
revision=os.environ['LAYERX_MULTI_ASSET_BUILD_REVISION']
assert not git('status','--porcelain','--untracked-files=all') and git('rev-parse','HEAD')==revision
artifacts={}
for key,event,name,kind in (
    ('custody_asset_send','LAYERX_MULTI_ASSET_CORE_EVENTS','custody_asset_send','test'),
    ('probe','LAYERX_MULTI_ASSET_PROBE_EVENTS','layerx-node-probe','bin')):
    rows=[json.loads(line) for line in Path(os.environ[event]).read_text().splitlines() if line.strip()]
    assert rows[-1]=={'reason':'build-finished','success':True},'actual successful Cargo completion required'
    paths={r['executable'] for r in rows if r.get('reason')=='compiler-artifact' and r.get('executable') and r['target']['name']==name and kind in r['target']['kind']}
    assert len(paths)==1,'one actual executable required: '+name
    path=Path(paths.pop())
    assert path.is_absolute() and not any(p.is_symlink() for p in (path,*path.parents)) and path.is_file() and os.access(path,os.X_OK)
    with path.open('rb') as stream:
        assert stream.read(4)==b'\x7fELF';stream.seek(0);digest=hashlib.file_digest(stream,'sha256').hexdigest()
    artifacts[key]={'path':str(path),'sha256':digest}
value={'version':1,'source_revision':revision,'source_tree':git('rev-parse','HEAD^{tree}'),'build_exit':0,'artifacts':artifacts}
path=Path(os.environ['LAYERX_MULTI_ASSET_BUILD_MANIFEST'])
fd=os.open(path,os.O_WRONLY|os.O_CREAT|os.O_EXCL|os.O_NOFOLLOW,0o600)
with os.fdopen(fd,'w') as stream:json.dump(value,stream,sort_keys=True);stream.write('\n');stream.flush();os.fsync(stream.fileno())
fd=os.open(path.parent,os.O_DIRECTORY)
try:os.fsync(fd)
finally:os.close(fd)
print('PAXEER_X_MULTI_ASSET_BUILD revision='+revision+' exit=0 evidence='+str(path))
RECORD
    ;;
verify) exec python3 tests/bridge/paxeer_x_multiasset_credit.py --connected ;;
*) echo 'usage: 5.1.sh build|verify' >&2; exit 2 ;;
esac
