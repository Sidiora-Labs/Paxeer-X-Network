#!/usr/bin/env bash
set -euo pipefail
umask 077
root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)
: "${LAYERX_GENERATION_SOURCE_SNAPSHOT:?isolated complete candidate snapshot required}"
snapshot=$LAYERX_GENERATION_SOURCE_SNAPSHOT
[ "$snapshot" != "$root" ] || [ "${LAYERX_GENERATION_IN_SNAPSHOT:-}" = 1 ] || { echo 'isolated candidate snapshot required' >&2; exit 1; }
if [ "${LAYERX_GENERATION_IN_SNAPSHOT:-}" != 1 ]; then
    export LAYERX_GENERATION_ORIGINAL_ROOT=$root
    export LAYERX_GENERATION_IN_SNAPSHOT=1
    exec bash "$snapshot/tools/paxeer-x/gates/24.20.sh" "${1:-verify}"
fi
cd "$snapshot"
export LAYERX_TEST_NATIVE_BIN_DIR="$snapshot/build/bin"
: "${LAYERX_GENERATION_ARTIFACT_MANIFEST:?private generation artifact manifest required}"
case "${1:-verify}" in
build)
    python3 - <<'PREPARE'
import os, stat, subprocess
from pathlib import Path
root=Path.cwd()
assert root.is_absolute() and str(root)==os.environ['LAYERX_GENERATION_SOURCE_SNAPSHOT']
assert not any(p.is_symlink() for p in (root,*root.parents)), 'snapshot symlink refused'
info=root.stat()
assert info.st_uid==os.geteuid() and stat.S_IMODE(info.st_mode)==0o700, 'private owned snapshot required'
assert not subprocess.check_output(['git','status','--porcelain','--untracked-files=all']), 'complete clean candidate required'
for key in ('LAYERX_GENERATION_ARTIFACT_MANIFEST',):
 p=Path(os.environ[key]);assert p.is_absolute() and not p.exists()
 assert not any(item.is_symlink() for item in (p,*p.parents))
 assert p.parent.stat().st_uid==os.geteuid() and not p.parent.stat().st_mode&0o077
for name in ('layerxd','layerx-genesis-build','layerx-handover','layerx-guarantor'):
 assert not (root/'build/bin'/name).exists(), 'fresh source build required: '+name
PREPARE
    revision=$(git rev-parse HEAD)
    make -j2 LXP_REVISION="$revision" PAXEER_GO_JOBS=2 layerxd layerx-genesis-build layerx-handover layerx-guarantor
    python3 - <<'RECORD'
import hashlib,json,os,stat,subprocess,time
from pathlib import Path
root=Path.cwd()
paths=['platform/hosted/node/'+name for name in ('generation_transport.py','generation_client.py','supervisor.sh','bootstrap.sh','guarantor.sh','deployment.yaml','tests/test_generation_transport.py','tests/reset_recovery.py')]
paths += ['docker/kernel/init.sh','tools/paxeer-x/gates/24.20.sh']
paths += ['docker/platform-'+role+'/'+name for role in ('core','authority') for name in ('Dockerfile','Dockerfile.dockerignore')]
def git(*args): return subprocess.check_output(['git',*args]).decode().strip()
def digest(p):
 with p.open('rb') as f: return hashlib.file_digest(f,'sha256').hexdigest()
assert not git('status','--porcelain','--untracked-files=all'), 'source changed during build'
for name in ('generation_transport.py','generation_client.py','reset_state.py','data_directory.py','tests/test_generation_transport.py','tests/reset_recovery.py'):
 p=root/'platform/hosted/node'/name;compile(p.read_bytes(),str(p),'exec')
for name in ('supervisor.sh','bootstrap.sh','guarantor.sh'):
 subprocess.run(['bash','-n',str(root/'platform/hosted/node'/name)],check=True)
subprocess.run(['bash','-n',str(root/'docker/kernel/init.sh')],check=True)
executables={}
for name in ('layerxd','layerx-genesis-build','layerx-handover','layerx-guarantor'):
 p=root/'build/bin'/name;info=p.lstat()
 assert stat.S_ISREG(info.st_mode) and info.st_uid==os.geteuid() and not info.st_mode&0o022 and os.access(p,os.X_OK)
 with p.open('rb') as f: assert f.read(4)==b'\x7fELF'
 executables[name]={'path':str(p),'sha256':digest(p),'mtime_ns':info.st_mtime_ns}
value={'version':1,'source_revision':git('rev-parse','HEAD'),'source_tree':git('rev-parse','HEAD^{tree}'),
 'native_build_source_binding':hashlib.sha256(subprocess.check_output(['git','ls-tree','-r','-z','--full-tree','HEAD'])).hexdigest(),
 'candidate_sources':{name:digest(root/name) for name in paths},'executables':executables,'recorded_ns':time.time_ns()}
p=Path(os.environ['LAYERX_GENERATION_ARTIFACT_MANIFEST'])
fd=os.open(p,os.O_WRONLY|os.O_CREAT|os.O_EXCL|os.O_NOFOLLOW,0o600)
with os.fdopen(fd,'w') as f: json.dump(value,f,sort_keys=True);f.write('\n');f.flush();os.fsync(f.fileno())
fd=os.open(p.parent,os.O_RDONLY|os.O_DIRECTORY)
try: os.fsync(fd)
finally: os.close(fd)
RECORD
    ;;
verify)
    python3 - <<'VERIFY'
import hashlib,importlib.util,json,os,stat,subprocess,sys,unittest
from pathlib import Path
root=Path.cwd();p=Path(os.environ['LAYERX_GENERATION_ARTIFACT_MANIFEST']);info=p.lstat()
assert p.is_absolute() and not any(item.is_symlink() for item in (p,*p.parents))
assert stat.S_ISREG(info.st_mode) and info.st_uid==os.geteuid() and stat.S_IMODE(info.st_mode)==0o600 and info.st_nlink==1
value=json.loads(p.read_bytes())
def digest(p):
 with p.open('rb') as f: return hashlib.file_digest(f,'sha256').hexdigest()
def git(*args): return subprocess.check_output(['git',*args]).decode().strip()
assert not git('status','--porcelain','--untracked-files=all')
assert value['version']==1 and value['source_revision']==git('rev-parse','HEAD') and value['source_tree']==git('rev-parse','HEAD^{tree}')
assert value['native_build_source_binding']==hashlib.sha256(subprocess.check_output(['git','ls-tree','-r','-z','--full-tree','HEAD'])).hexdigest()
assert len(value['candidate_sources'])==14
for name,expected in value['candidate_sources'].items(): assert digest(root/name)==expected,'candidate source mismatch: '+name
assert set(value['executables'])=={'layerxd','layerx-genesis-build','layerx-handover','layerx-guarantor'}
for name,row in value['executables'].items():
 target=root/'build/bin'/name
 assert row['path']==str(target) and digest(target)==row['sha256'] and target.stat().st_mtime_ns==row['mtime_ns']<=value['recorded_ns']
sys.dont_write_bytecode=True
path=root/'platform/hosted/node/tests/test_generation_transport.py'
spec=importlib.util.spec_from_file_location('generation_transport_tests',path);module=importlib.util.module_from_spec(spec);spec.loader.exec_module(module)
suite=unittest.defaultTestLoader.loadTestsFromModule(module);count=suite.countTestCases()
assert count>0,'missing actual broker tests'
result=unittest.TextTestRunner(verbosity=2).run(suite)
print('PAXEER_X_GENERATION_BROKER_GATE tests=%d skipped=%d'%(result.testsRun,len(result.skipped)),flush=True)
assert result.wasSuccessful() and not result.skipped and result.testsRun==count,'actual broker corpus failed'
fixture=os.environ.get('LAYERX_CUSTODY_ARTIFACT_MANIFEST')
assert fixture and Path(fixture).is_file(),'genuine matching custody artifact manifest unavailable; actual funded/bootstrap process prerequisite unqualified'
native=os.environ.get('LAYERX_RESET_NATIVE_ARTIFACT_MANIFEST')
assert native,'explicit private native artifact manifest required with genuine custody fixture'
if not Path(native).exists():
 subprocess.run([sys.executable,str(root/'platform/hosted/node/tests/reset_recovery.py'),'--record-native',native],check=True,timeout=30)
subprocess.run([sys.executable,str(root/'platform/hosted/node/tests/reset_recovery.py'),'--generation-transport'],check=True,timeout=750)
VERIFY
    ;;
*) echo 'usage: 24.20.sh build|verify' >&2; exit 2 ;;
esac
