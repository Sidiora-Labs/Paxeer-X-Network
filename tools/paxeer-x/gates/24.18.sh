#!/usr/bin/env bash
set -euo pipefail
umask 077
root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)
cd "$root"
case "${1:-verify}" in
build)
    : "${PAXEER_X_CORE_BUILD_MANIFEST:?private core manifest output required}"
    : "${PAXEER_X_FOUNDATION_ARTIFACT_MANIFEST:?private native manifest output required}"
    : "${LAYERX_CUSTODY_ARTIFACT_MANIFEST:?private custody manifest output required}"
    test -z "$(git status --porcelain --untracked-files=normal)"
    export REVISION=$(git rev-parse HEAD)
    export PAXEER_X_CORE_SOURCE_TREE=$(git rev-parse HEAD^{tree})
    export PAXEER_X_CORE_BUILD_EVENTS="${PAXEER_X_CORE_BUILD_MANIFEST}.cargo.jsonl"
    python3 - <<'PREPARE'
import os, stat
from pathlib import Path
for key in ('PAXEER_X_CORE_BUILD_MANIFEST', 'PAXEER_X_FOUNDATION_ARTIFACT_MANIFEST',
            'LAYERX_CUSTODY_ARTIFACT_MANIFEST', 'PAXEER_X_CORE_BUILD_EVENTS'):
    path = Path(os.environ[key])
    assert path.is_absolute() and not path.exists(), 'new absolute build output required'
    assert not any(p.is_symlink() for p in (path, *path.parents)), 'symlink output forbidden'
    info = path.parent.stat()
    assert stat.S_ISDIR(info.st_mode) and info.st_uid == os.geteuid() and not info.st_mode & 0o077
PREPARE
    cargo test --locked --manifest-path platform/Cargo.toml -p layerx-platform-core --no-run --message-format=json > "$PAXEER_X_CORE_BUILD_EVENTS"
    make -j5 LXP_REVISION="$REVISION" PAXEER_GO_JOBS=5 layerxd layerx-genesis-build layerx-handover
    bash tools/paxeer-x/gates/24.16.sh build
    python3 - <<'RECORD'
import hashlib, json, os, subprocess
from pathlib import Path
root = Path.cwd()
def git(*args):
    return subprocess.check_output(['git', *args], text=True).strip()
revision, tree = os.environ['REVISION'], os.environ['PAXEER_X_CORE_SOURCE_TREE']
assert not git('status', '--porcelain', '--untracked-files=normal')
assert git('rev-parse', 'HEAD') == revision and git('rev-parse', 'HEAD^{tree}') == tree
rows = [json.loads(line) for line in Path(os.environ['PAXEER_X_CORE_BUILD_EVENTS']).read_text().splitlines() if line.strip()]
assert rows[-1] == {'reason': 'build-finished', 'success': True}, 'actual successful Cargo completion required'
def cargo_target(name, kind, test):
    paths = {row['executable'] for row in rows if row.get('reason') == 'compiler-artifact'
             and row.get('executable') and row['target']['name'] == name
             and kind in row['target']['kind'] and row['profile']['test'] is test}
    assert len(paths) == 1, 'one actual Cargo executable required: ' + name
    return Path(paths.pop())
def executable(path):
    assert path.is_absolute() and not any(p.is_symlink() for p in (path, *path.parents))
    assert path.is_file() and os.access(path, os.X_OK)
    with path.open('rb') as stream:
        assert stream.read(4) == b'\x7fELF', 'real executable ELF required'
        stream.seek(0)
        digest = hashlib.file_digest(stream, 'sha256').hexdigest()
    return {'path': str(path), 'sha256': digest, 'source_revision': revision}
def publish(variable, value):
    path = Path(os.environ[variable])
    temporary = path.with_name(path.name + '.partial')
    with temporary.open('x') as stream:
        json.dump(value, stream, indent=2)
        stream.write('\n')
        stream.flush()
        os.fsync(stream.fileno())
    os.chmod(temporary, 0o600)
    os.link(temporary, path)
    temporary.unlink()
    directory = os.open(path.parent, os.O_DIRECTORY)
    try: os.fsync(directory)
    finally: os.close(directory)
core = executable(cargo_target('layerx-core-boundary', 'bin', False))
boundary = executable(cargo_target('boundary', 'test', True))
common = {'version': 1, 'source_revision': revision, 'source_tree': tree}
publish('PAXEER_X_CORE_BUILD_MANIFEST', dict(common, build_exit=0,
    core_binary_path=core['path'], artifacts={'core': core, 'boundary_tests': boundary}))
publish('PAXEER_X_FOUNDATION_ARTIFACT_MANIFEST', dict(common, build={'exit_code': 0},
    artifacts={name: executable(root / 'build/bin' / name)
               for name in ('layerxd', 'layerx-genesis-build', 'layerx-handover')}))
RECORD
    ;;
verify) exec timeout 15m python3 platform/hosted/core/tests/receipt_retention.py ;;
*) echo 'usage: 24.18.sh build|verify' >&2; exit 2 ;;
esac
