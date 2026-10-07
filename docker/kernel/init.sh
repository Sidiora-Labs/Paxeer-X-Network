#!/bin/bash
# Root init of the kernel box (docker/kernel/layerx-kernel.service), the node pod
# of platform/hosted/node/deployment.yaml in one container. It replays the pod's
# init containers on the volume, mounts the pod's memory volumes, and runs each
# container command under its uid through setpriv and the runtime clock,
# restarting one that exits. A service starts once every file it waits on
# exists; /run/layerx/init/<service> reads "<uid> running <pid>" or
# "<uid> waiting <genesis|path>".
#
# Volume layout (/data):
#   /data/layerx/node         layerxd --data-dir; never holds a key, the genesis
#                             metadata or the run directory (bootstrap.sh refuses them)
#   /data/layerx/keys         every key: sequencer.key and treasury.key, tokens/
#                             (program, replica, backend-admin, gateway-component,
#                             gateway-authority, webhooks-component, webhooks-authority),
#                             checkpoint-authority/key.pem, checkpoint-submitter/key,
#                             publication/, human-authority/
#   /data/layerx/genesis      metadata.lxgb of tools/bringup/kernel-genesis.sh
#   /data/layerx/guarantor-*  the pod's guarantor storage
#   /data/layerx/settlement   settlement.env and checkpoint-settlement.json
#   /data/layerx/mirror       the mirror publisher's state directory
#   /data/layerx/core, /data/layerx/agent-boundary  the boundaries' state
#   /data/human-state         the pod's human-state volume
#   /data/tls/<service>       identities of tools/bringup/ca.sh issue <service>
set -euo pipefail
umask 077

kernel_profile=${LAYERX_KERNEL_PROFILE:-full}
case "$kernel_profile" in
    full|native) ;;
    *) printf 'kernel-init: LAYERX_KERNEL_PROFILE must be full or native\n' >&2; exit 1 ;;
esac
if [ "$kernel_profile" = native ]; then
    for variable in LAYERX_AUTHORITY_HUMAN_AGENT_TOKEN_FILE LAYERX_AUTHORITY_HUMAN_AGENT_TENANT \
        LAYERX_AUTHORITY_HUMAN_AGENT_PRINCIPAL LAYERX_AUTHORITY_PRINCIPAL_POLICY_FILE \
        LAYERX_AUTHORITY_MODULE_REGISTRY_FILE LAYERX_AUTHORITY_CORE_CLOCK_HORIZON \
        LAYERX_AUTHORITY_STATE_ROOT LAYERX_AUTHORITY_IDENTITY_BINDING_SOCKET \
        LAYERX_AUTHORITY_IDENTITY_BINDING_UID LAYERX_AUTHORITY_IDENTITY_BINDING_GID; do
        if [[ -v "$variable" ]]; then
            printf 'kernel-init: native profile refuses configured %s\n' "$variable" >&2
            exit 1
        fi
    done
fi

layerx=/data/layerx
node_data=$layerx/node
keys=$layerx/keys
genesis=$layerx/genesis
human_state=/data/human-state
trust_history_file=$human_state/trust-history
if [ "$kernel_profile" = native ]; then
    trust_history_file=$layerx/trust/history
fi
tls=${LAYERX_TLS_DIR:-/data/tls}
run=/run/layerx
status=$run/init
settlement_env=$layerx/settlement/settlement.env
genesis_files="$genesis/metadata.lxgb $keys/sequencer.key $genesis/asset-id $genesis/replica-id $keys/publication/binding-policy.json $keys/publication/authorization.json"

# The layerx-node-config ConfigMap of the pod, and the precompile addresses of
# its layerxd container. The network id is kernel_network_id of the spec, set
# in the box env file; the asset id (the PAX record of the custody
# asset map) and the replica id are the ones tools/bringup/kernel-genesis.sh
# wrote beside the genesis metadata.
: "${LAYERX_NODE_NETWORK_ID:?the kernel network id is set in the app env}"
export LAYERX_NODE_NETWORK_ID
export LAYERX_NODE_RESET_STAGED_GENERATIONS=1
export LAYERX_NODE_GENERATION_TRANSPORT=1
export LAYERX_NODE_PAXEER_RELAY_PORT=18545
export LAYERX_NODE_PAXEER_CHAIN_ID=125
export LAYERX_NODE_PAXEER_RPC_URL=http://127.0.0.1:$LAYERX_NODE_PAXEER_RELAY_PORT
export LAYERX_NODE_REGISTRY_PRECOMPILE=0x0000000000000000000000000000000000001004
export LAYERX_NODE_CUSTODY_PRECOMPILE=0x0000000000000000000000000000000000001013
export LAYERX_NODE_ANCHOR_PRECOMPILE=0x0000000000000000000000000000000000001014

log() { printf 'kernel-init: %s\n' "$*" >&2; }

# Required app env, checked before any service starts.
#   LAYERX_KERNEL_PAXEER_RPC_NAMES  two different serving Paxeer RPC names,
#       space separated, first one first; start_paxeer fronts each with a
#       paxeer boundary and relays the first to layerxd on
#       $LAYERX_NODE_PAXEER_RPC_URL, the only chain URL layerxd accepts.
# Full profile only, for layerx-agentd in the human-owner service (the app's
# 9454 passthrough), its server identity being tools/bringup/ca.sh issue
# agentd-rpc under $tls/agentd-rpc (cert.pem, key.pem, ca.pem; the CA also
# verifies the gateway's client certificate):
#   LAYERX_NODE_NETWORK_NAME                    the network name agentd answers for
#   LAYERX_KERNEL_AGENTD_RPC_PEER               the one DNS name of the gateway
#                                               client certificate agentd accepts
#   LAYERX_KERNEL_AGENTD_RPC_DAEMON_SEQUENCES   idempotency retention in daemon
#                                               sequences
#   LAYERX_KERNEL_AGENTD_RPC_PROTOCOL_SEQUENCES idempotency retention in protocol
#                                               sequences, at most the daemon one
#   LAYERX_KERNEL_AGENTD_RPC_LISTEN             optional, default [::]:9454
require_env() {
	local variable
	for variable in "$@"; do
		[ -n "${!variable:-}" ] || {
			log "$variable is required in the app env"
			exit 1
		}
	done
}

require_env LAYERX_KERNEL_PAXEER_RPC_NAMES
read -r -a paxeer_rpc_names <<<"$LAYERX_KERNEL_PAXEER_RPC_NAMES"
if [ "${#paxeer_rpc_names[@]}" -ne 2 ] || [ "${paxeer_rpc_names[0]}" = "${paxeer_rpc_names[1]}" ]; then
	log "LAYERX_KERNEL_PAXEER_RPC_NAMES must hold two different serving RPC names"
	exit 1
fi
for k in 0 1; do
	case "${paxeer_rpc_names[$k]}" in
	api[1-9].mainnet-beta.paxeer.network | api1[0-6].mainnet-beta.paxeer.network) ;;
	*)
		log "LAYERX_KERNEL_PAXEER_RPC_NAMES entry $((k + 1)) is not a public RPC name"
		exit 1
		;;
	esac
done

if [ "$kernel_profile" = full ]; then
	require_env LAYERX_NODE_NETWORK_NAME LAYERX_KERNEL_AGENTD_RPC_PEER \
		LAYERX_KERNEL_AGENTD_RPC_DAEMON_SEQUENCES LAYERX_KERNEL_AGENTD_RPC_PROTOCOL_SEQUENCES
	for variable in LAYERX_KERNEL_AGENTD_RPC_DAEMON_SEQUENCES LAYERX_KERNEL_AGENTD_RPC_PROTOCOL_SEQUENCES; do
		[[ "${!variable}" =~ ^[1-9][0-9]{0,18}$ ]] || {
			log "$variable must be a positive integer"
			exit 1
		}
	done
	[ "$LAYERX_KERNEL_AGENTD_RPC_PROTOCOL_SEQUENCES" -le "$LAYERX_KERNEL_AGENTD_RPC_DAEMON_SEQUENCES" ] || {
		log "LAYERX_KERNEL_AGENTD_RPC_PROTOCOL_SEQUENCES must not exceed LAYERX_KERNEL_AGENTD_RPC_DAEMON_SEQUENCES"
		exit 1
	}
	agentd_rpc_listen=${LAYERX_KERNEL_AGENTD_RPC_LISTEN:-[::]:9454}
fi

# identity_generation <mode> [arguments...]: the identity generation of the
# volume's persisted bindings. The generation is the kernel registry
# generation of the genesis (network, sequencer key, replica id, asset id,
# genesis metadata), recorded in $layerx/identity/current.json once the
# trust history, the receipt-authority replica bindings and the Human role
# material agree with it. plan prints the compatibility plan and its sha256;
# gate refuses Human consumers whose persisted bindings belong to another
# generation and records the generation of a compatible volume; retire and
# rotated are the journaled halves of kernel-genesis.sh keys rotate, which
# moves the kernel identity material into $layerx/identity/generations/<id>;
# migrate completes the migration the plan sha256 authorizes, retaining the
# old bindings beside the retired generation and rebinding the role material.
identity_generation() {
	python3 - "$layerx" "$keys" "$genesis" "$node_data" "$human_state" "$trust_history_file" "$LAYERX_NODE_NETWORK_ID" "$@" <<'PY_IDENTITY'
import contextlib
import fcntl
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import stat
import struct
import subprocess
import sys

layerx, keys, genesis, node_data, human_state, history_path = map(Path, sys.argv[1:7])
network = int(sys.argv[7])
mode, arguments = sys.argv[8], sys.argv[9:]
volume = Path('/data')
root = layerx / 'identity'
record_path = root / 'current.json'
rotation_path = root / 'rotation.json'
migration_path = root / 'migration.json'
MAGIC = b'LayerX/sequencer-trust-history/v1\0'
RECORD_SCHEMA = 'layerx.kernel.identity-generation.v1'
HUMAN_DURABLE = [human_state / name for name in ('components', 'identity', 'security', 'movement', 'agent', 'authority', 'kms')]
KERNEL_DURABLE = [keys / 'treasury.key', keys / 'tokens'] + [layerx / name for name in (
    'settlement', 'core', 'agent-boundary', 'mirror', 'guarantor-1', 'guarantor-2')]
ARCHIVED = ('trust-history', 'registry-material', 'receipt-authority-replica', 'authority-graph')
sys.path.insert(0, '/usr/local/lib/layerx-human')


class Refused(Exception):
    pass


def digest(data):
    return hashlib.sha256(data).hexdigest()


def canonical(value):
    return json.dumps(value, sort_keys=True, separators=(',', ':')).encode()


def read(path, limit=1048576):
    fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
    with os.fdopen(fd, 'rb') as source:
        info = os.fstat(source.fileno())
        if not stat.S_ISREG(info.st_mode) or not 0 < info.st_size <= limit:
            raise Refused('protected bounded regular file required: ' + str(path))
        return source.read(limit + 1)


def sync(path):
    fd = os.open(path, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
    try:
        os.fsync(fd)
    finally:
        os.close(fd)


def write_atomic(path, data):
    pending = path.with_name('.' + path.name + '.pending')
    if os.path.lexists(pending):
        os.unlink(pending)
    fd = os.open(pending, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
    with os.fdopen(fd, 'wb') as output:
        output.write(data)
        output.flush()
        os.fsync(output.fileno())
    os.replace(pending, path)
    sync(path.parent)


def make_parents(path):
    missing = []
    while not os.path.lexists(path):
        missing.append(path)
        path = path.parent
    for directory in reversed(missing):
        os.mkdir(directory, 0o700)
        os.chown(directory, 0, 0)
        os.chmod(directory, 0o700)
        sync(directory.parent)


def file_digest(path):
    value = hashlib.sha256()
    fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
    with os.fdopen(fd, 'rb') as source:
        for chunk in iter(lambda: source.read(1 << 20), b''):
            value.update(chunk)
    return value.hexdigest()


def tree(path):
    entries = {}
    items = [path]
    if path.is_dir() and not path.is_symlink():
        for directory, names, files in os.walk(path):
            items += [Path(directory) / name for name in sorted(names + files)]
    for item in items:
        info = item.lstat()
        meta = [info.st_uid, info.st_gid, stat.S_IMODE(info.st_mode)]
        name = str(item.relative_to(path))
        if stat.S_ISLNK(info.st_mode):
            entries[name] = ['link', *meta, os.readlink(item)]
        elif stat.S_ISDIR(info.st_mode):
            entries[name] = ['directory', *meta]
        elif stat.S_ISREG(info.st_mode):
            entries[name] = ['file', *meta, info.st_size, file_digest(item)]
        else:
            entries[name] = ['special', *meta, stat.S_IFMT(info.st_mode)]
    return dict(sorted(entries.items()))


def summary(path):
    entries = tree(path)
    return {'entries': len(entries), 'bytes': sum(e[4] for e in entries.values() if e[0] == 'file'),
            'sha256': digest(canonical(entries))}


def material_module():
    import material
    return material


def public_of(seed_path):
    seed = read(seed_path).decode('ascii', 'replace').rstrip('\n')
    if not re.fullmatch('[0-9a-f]{64}', seed):
        raise Refused('the sequencer seed is not 64 hex characters')
    der = subprocess.run(['openssl', 'pkey', '-inform', 'DER', '-pubout', '-outform', 'DER'],
                         input=bytes.fromhex('302e020100300506032b657004220420' + seed),
                         stdout=subprocess.PIPE, stderr=subprocess.DEVNULL, check=True, timeout=10).stdout
    if len(der) != 44 or der[:12] != bytes.fromhex('302a300506032b6570032100'):
        raise Refused('the sequencer public key derivation failed')
    return der[12:].hex()


def genesis_identity():
    paths = (keys / 'sequencer.key', genesis / 'metadata.lxgb', genesis / 'asset-id', genesis / 'replica-id')
    present = [os.path.lexists(path) for path in paths]
    if not any(present):
        return None
    result = {'complete': all(present)}
    if present[0]:
        public = public_of(paths[0])
        result.update(sequencer_public_key=public, sequencer_id=digest(('layerx-sequencer:' + public).encode()))
    if not result['complete']:
        return result
    metadata, asset, replica = (read(path) for path in paths[1:])
    if not re.fullmatch(b'[0-9a-f]{64}\n?', asset) or not re.fullmatch(b'[0-9a-f]{64}\n?', replica):
        raise Refused('the genesis ids are not 64 hex characters')
    if replica.decode().rstrip('\n') != digest(('layerx-authority-replica:' + public).encode()):
        raise Refused('the genesis replica id is not derived from the sequencer key')
    history = (MAGIC + struct.pack('>HH', 1, 0) + struct.pack('>HIQ', 3, network, 1)
               + bytes.fromhex(result['sequencer_id']) + bytes.fromhex(public) + struct.pack('>QQBQ', 1, 1 << 40, 0, 0))
    manifest = {'schema': 'layerx.kernel.registry-generation.v1', 'network_id': network,
                'sequencer_id': result['sequencer_id'], 'sequencer_public_key': public,
                'replica_id': replica.decode().rstrip('\n'), 'genesis_metadata_sha256': digest(metadata),
                'asset_id': asset.decode().rstrip('\n'), 'history_sha256': digest(history), 'replica_sha256': digest(replica)}
    result.update(manifest, generation=digest(canonical(manifest)), asset_sha256=digest(asset))
    del result['schema']
    return result


def record_of(identity):
    return {'schema': RECORD_SCHEMA, **{key: value for key, value in identity.items() if key != 'complete'}}


def load_record():
    if not os.path.lexists(record_path):
        return None
    value = json.loads(read(record_path))
    if (type(value) is not dict or value.get('schema') != RECORD_SCHEMA
            or not re.fullmatch('[0-9a-f]{64}', str(value.get('generation'))) or value.get('network_id') != network):
        raise Refused('the recorded identity generation at ' + str(record_path) + ' is not a ' + RECORD_SCHEMA + ' record of network ' + str(network))
    return value


def trust_claims(path):
    data = read(path)
    size = len(MAGIC) + 4
    if not data.startswith(MAGIC) or len(data) < size + 103:
        raise Refused('trust history framing')
    entry = data[size:size + 103]
    return {'history_sha256': digest(data), 'network_id': struct.unpack('>I', entry[2:6])[0],
            'sequencer_id': entry[14:46].hex(), 'sequencer_public_key': entry[46:78].hex()}


def registry_claims(path):
    if not (path / 'current').is_symlink():
        return None
    manifest = material_module().verify_registry_material(path)['manifest']
    return {key: manifest[key] for key in ('generation', 'network_id', 'sequencer_id', 'sequencer_public_key', 'replica_id',
                                           'asset_id', 'genesis_metadata_sha256', 'history_sha256', 'replica_sha256')}


def projection_claims(path):
    metadata, asset, replica = (read(path / name) for name in ('metadata.lxgb', 'asset-id', 'replica-id'))
    return {'genesis_metadata_sha256': digest(metadata), 'asset_sha256': digest(asset), 'replica_sha256': digest(replica)}


def material_claims(path):
    material_module().verify_material(path)
    replica = read(path / 'receipt-authority-replica-id')
    files = {item['name']: item['sha256'] for item in json.loads(read(path / 'genesis-binding'))['files']}
    if files.get('replica-id') != digest(replica):
        raise Refused('the role material replica id differs from its own genesis binding')
    return {'replica_sha256': digest(replica), 'asset_sha256': files['asset-id'], 'genesis_metadata_sha256': files['metadata.lxgb']}


def authority_claims(path):
    if not (path / 'current').is_symlink():
        return None
    return {'generation': material_module().verify_authority_material(path)['registry_generation']}


BINDINGS = (('trust-history', history_path, trust_claims),
            ('registry-material', layerx / 'registry-material', registry_claims),
            ('receipt-authority-replica', human_state / 'genesis-binding', projection_claims),
            ('role-material', human_state / 'material', material_claims),
            ('authority-graph', human_state / 'authority-graph', authority_claims))


def bindings():
    found = {}
    for name, path, reader in BINDINGS:
        entry = {'path': str(path), 'state': 'absent'}
        if os.path.lexists(path):
            try:
                claims = reader(path)
            except (OSError, ValueError, KeyError, TypeError, Refused, subprocess.SubprocessError) as error:
                entry.update(state='unreadable', reason=str(error) or type(error).__name__)
            else:
                if claims is not None:
                    entry.update(state='bound', claims=claims)
        found[name] = entry
    return found


def mismatches(found, target):
    result = []
    for name, entry in found.items():
        for field, value in sorted(entry.get('claims', {}).items()):
            if field in target and target[field] != value:
                result.append(name + ' at ' + entry['path'] + ' is bound to ' + field + ' ' + str(value) + ', not ' + str(target[field]))
    return result


def conflicts(found):
    seen, result = {}, []
    for name, entry in found.items():
        for field, value in sorted(entry.get('claims', {}).items()):
            if field not in seen:
                seen[field] = (name, value)
            elif seen[field][1] != value:
                result.append(name + ' ' + field + ' ' + str(value) + ' disagrees with ' + seen[field][0] + ' ' + field + ' ' + str(seen[field][1]))
    return result, {field: value for field, (_, value) in seen.items()}


def retire_paths(listed):
    paths = []
    for raw in listed:
        path = Path(raw)
        if not path.is_absolute() or path.relative_to(volume).parts[:2] == ('layerx', 'identity'):
            raise Refused('retired path outside the kernel identity material: ' + raw)
        paths.append(path)
    if os.path.isdir(node_data) and not os.path.islink(node_data):
        paths += sorted(node_data.iterdir())
    return paths


def survey(listed=None, durable=False):
    value = {'schema': 'layerx.kernel.identity-plan.v1', 'network_id': network, 'refusals': []}
    refusals = value['refusals']
    errors = []
    try:
        current = genesis_identity()
    except (OSError, Refused, subprocess.SubprocessError) as error:
        current = None
        errors.append('the kernel genesis is unreadable: ' + (str(error) or type(error).__name__))
    try:
        recorded = load_record()
    except (OSError, ValueError, Refused) as error:
        recorded = None
        errors.append(str(error) or type(error).__name__)
    found = bindings()
    errors += [name + ' at ' + entry['path'] + ' is unreadable: ' + entry['reason']
               for name, entry in found.items() if entry['state'] == 'unreadable']
    value.update(genesis=current, recorded=recorded, bindings=found)
    for name, path, _ in BINDINGS:
        if name in found and found[name]['state'] == 'bound':
            found[name]['compatible'] = current is not None and current.get('complete', False) and not mismatches({name: found[name]}, current)
    conflicting, _ = conflicts(found)
    if os.path.lexists(rotation_path):
        verdict = 'rotation-in-progress'
        refusals.append('an identity rotation is pending at ' + str(rotation_path) + '; kernel-genesis.sh keys rotate resumes it')
    elif os.path.lexists(migration_path):
        verdict = 'migration-in-progress'
        refusals.append('an identity migration is pending at ' + str(migration_path) + '; kernel-genesis.sh migrate with its plan resumes it')
    elif errors:
        verdict = 'unreadable'
        refusals += errors
    elif conflicting:
        verdict = 'inconsistent'
        refusals += conflicting
    elif current is None or not current['complete']:
        verdict = 'genesis-incomplete'
        refusals.append('the kernel genesis of this volume is incomplete')
    elif recorded is not None and recorded['generation'] != current['generation']:
        verdict = 'migration-required'
        refusals.append('the volume identity generation ' + recorded['generation'] + ' differs from the genesis identity generation '
                        + current['generation'] + '; Human consumers stay refused until kernel-genesis.sh migrate completes an authorized migration')
    else:
        different = mismatches(found, current)
        if different and recorded is not None:
            verdict = 'incompatible'
            refusals += different
        elif different:
            verdict = 'inferred-mismatch'
            refusals += ['inferred old-genesis binding: ' + line for line in different]
        elif recorded is None and not any(entry['state'] == 'bound' for entry in found.values()):
            verdict = 'fresh'
        else:
            verdict = 'compatible'
    value['verdict'] = verdict
    if durable:
        value['durable'] = {str(path): summary(path) for path in HUMAN_DURABLE if os.path.lexists(path)}
    if listed is not None:
        retained = [str(path) for path in HUMAN_DURABLE + KERNEL_DURABLE if os.path.lexists(path)]
        bound = [entry['path'] for entry in found.values() if entry['state'] == 'bound']
        value['disposition'] = {
            'rotation': {'retire': [str(path) for path in retire_paths(listed)[:len(listed)] if os.path.lexists(path)]
                         + ([str(node_data) + '/*'] if os.path.isdir(node_data) else []),
                         'retain': retained + [path for path in bound if path != str(layerx / 'registry-material')]},
            'migration': {'archive': [found[name]['path'] for name in ARCHIVED
                                      if found[name]['state'] == 'bound' and not found[name]['compatible']],
                          'rebind': [found['role-material']['path']]
                          if found['role-material']['state'] == 'bound' and not found['role-material']['compatible'] else [],
                          'retain': retained}}
        generations = root / 'generations'
        value['retained_generations'] = sorted(path.name for path in generations.iterdir()) if generations.is_dir() else []
    return value


@contextlib.contextmanager
def locked(*paths):
    if not os.path.lexists(root):
        make_parents(root)
    info = os.lstat(root)
    if not stat.S_ISDIR(info.st_mode) or (info.st_uid, stat.S_IMODE(info.st_mode)) != (0, 0o700):
        raise Refused('the identity directory ' + str(root) + ' is not a root 0700 directory')
    handles = []
    try:
        for path in (*paths, root / '.lock'):
            handle = os.fdopen(os.open(path, os.O_RDWR | os.O_CREAT | os.O_NOFOLLOW, 0o600), 'r+b')
            handles.append(handle)
            fcntl.flock(handle, fcntl.LOCK_EX)
        yield
    finally:
        for handle in reversed(handles):
            handle.close()


def move(source, target):
    if os.path.lexists(source):
        if os.path.lexists(target):
            raise Refused('both ' + str(source) + ' and its retained copy ' + str(target) + ' exist')
        make_parents(target.parent)
        os.rename(source, target)
        sync(source.parent)
        sync(target.parent)
    elif not os.path.lexists(target):
        raise Refused('neither ' + str(source) + ' nor its retained copy ' + str(target) + ' exists')


def retained(archive):
    manifest = archive / 'manifest.json'
    if not os.path.lexists(manifest):
        return
    for item in json.loads(read(manifest, 1 << 26))['entries']:
        if not os.path.lexists(item['target']) or tree(Path(item['target'])) != item['tree']:
            raise Refused('the retained generation at ' + str(archive) + ' no longer matches its manifest: ' + item['target'])


def gate():
    with locked():
        value = survey()
        if value['verdict'] not in ('fresh', 'compatible'):
            raise Refused('identity generation refused (' + value['verdict'] + '): ' + '; '.join(value['refusals']))
        if value['recorded'] is None:
            write_atomic(record_path, canonical(record_of(value['genesis'])) + b'\n')
            print('kernel-init: identity generation ' + value['genesis']['generation'] + ' recorded for this volume', file=sys.stderr)


def retire():
    with locked(layerx / 'registry-material.lock'):
        if os.path.lexists(migration_path):
            raise Refused('rotation refused: an identity migration is pending at ' + str(migration_path))
        if os.path.lexists(rotation_path):
            journal = json.loads(read(rotation_path, 1 << 24))
        else:
            value = survey()
            verdict, current = value['verdict'], value['genesis']
            bound = [name for name, entry in value['bindings'].items() if entry['state'] == 'bound']
            if verdict not in ('fresh', 'compatible', 'migration-required', 'genesis-incomplete') or (verdict == 'genesis-incomplete' and bound):
                reasons = value['refusals'] + (['Human bindings ' + ', '.join(bound) + ' exist while the genesis is incomplete'] if verdict == 'genesis-incomplete' else [])
                raise Refused('rotation preflight refused (' + verdict + '): ' + '; '.join(reasons))
            if verdict == 'compatible' and value['recorded'] is None:
                write_atomic(record_path, canonical(record_of(current)) + b'\n')
            sources = [path for path in retire_paths(arguments) if os.path.lexists(path)]
            if not sources:
                print(json.dumps({'retired': None}))
                return
            if current is not None and current['complete']:
                name = current['generation']
            elif current is not None and 'sequencer_public_key' in current:
                name = 'keys-' + digest(current['sequencer_public_key'].encode())
            else:
                name = 'partial-' + digest(canonical(sorted(map(str, sources))))
            archive = root / 'generations' / name
            if os.path.lexists(archive):
                raise Refused('rotation refused: a retained generation already exists at ' + str(archive))
            journal = {'schema': 'layerx.kernel.identity-rotation.v1', 'phase': 'retiring', 'generation': name,
                       'archive': str(archive), 'moves': [[str(path), str(archive / 'files' / path.relative_to(volume))] for path in sources]}
            make_parents(root / 'generations')
            write_atomic(rotation_path, canonical(journal) + b'\n')
        archive = Path(journal['archive'])
        if journal['phase'] == 'retiring':
            for source, target in journal['moves']:
                move(Path(source), Path(target))
            manifest = {'schema': 'layerx.kernel.retained-generation.v1', 'generation': journal['generation'],
                        'entries': [{'source': source, 'target': target, 'tree': tree(Path(target))} for source, target in journal['moves']]}
            write_atomic(archive / 'manifest.json', canonical(manifest) + b'\n')
            journal['phase'] = 'retired'
            write_atomic(rotation_path, canonical(journal) + b'\n')
        elif journal['phase'] == 'retired':
            retained(archive)
            for source, _ in journal['moves']:
                path = Path(source)
                if path.is_relative_to(keys) and os.path.lexists(path) and stat.S_ISREG(os.lstat(path).st_mode):
                    os.unlink(path)
                    sync(path.parent)
        else:
            raise Refused('rotation journal phase ' + str(journal['phase']))
        print(json.dumps({'retired': journal['generation'], 'archive': journal['archive']}))


def rotated():
    with locked():
        if not os.path.lexists(rotation_path):
            return
        journal = json.loads(read(rotation_path, 1 << 24))
        if journal['phase'] != 'retired':
            raise Refused('the rotation journal is in phase ' + str(journal['phase']) + ', not retired')
        for raw in arguments:
            if not os.path.isfile(raw) or os.path.getsize(raw) == 0:
                raise Refused('the rotated key ' + raw + ' is absent')
        journal['phase'] = 'complete'
        write_atomic(Path(journal['archive']) / 'rotation.json', canonical(journal) + b'\n')
        os.unlink(rotation_path)
        sync(root)


def rebind_material(journal):
    module = material_module()
    current, stage = human_state / 'material', human_state / '.material-migrating'
    target = Path(journal['archive']) / 'files' / current.relative_to(volume)
    if os.path.lexists(current) and not os.path.lexists(target):
        if os.path.lexists(stage):
            shutil.rmtree(stage)
        shutil.copytree(current, stage, symlinks=True)
        os.unlink(stage / 'material-manifest.json')
        replica = read(genesis / 'replica-id')
        binding = {'schema': 'layerx.human.genesis-binding.v1',
                   'files': [module.entry(name, read(genesis / name)) for name in ('metadata.lxgb', 'asset-id', 'replica-id')]}
        for name, data in (('receipt-authority-replica-id', replica), ('genesis-binding', (json.dumps(binding, sort_keys=True) + '\n').encode())):
            os.unlink(stage / name)
            module.write_bytes(stage / name, data)
        module.seal_material(stage)
        module.verify_material(stage)
        make_parents(target.parent)
        os.rename(current, target)
        sync(current.parent)
        sync(target.parent)
    if not os.path.lexists(current):
        if not os.path.lexists(stage) or not os.path.lexists(target):
            raise Refused('the role material migration lost its staged or retained copy')
        module.verify_material(stage)
        os.rename(stage, current)
        sync(current.parent)


def migrate():
    if len(arguments) < 1 or not re.fullmatch('[0-9a-f]{64}', arguments[0]):
        raise Refused('usage: migrate PLAN_SHA256 RETIRED_PATH...')
    authorization, listed = arguments[0], arguments[1:]
    with locked(layerx / 'registry-material.lock'):
        if os.path.lexists(rotation_path):
            raise Refused('migration refused: an identity rotation is pending at ' + str(rotation_path))
        if os.path.lexists(migration_path):
            journal = json.loads(read(migration_path, 1 << 24))
            if journal['plan_sha256'] != authorization:
                raise Refused('migration refused: the pending migration was authorized by plan ' + journal['plan_sha256'])
        else:
            value = survey(listed, durable=True)
            plan = digest(canonical(value))
            if authorization != plan:
                raise Refused('migration refused: the authorization ' + authorization + ' is not the current plan ' + plan
                              + '; review kernel-genesis.sh plan and pass its plan_sha256')
            verdict, current, recorded, found = value['verdict'], value['genesis'], value['recorded'], value['bindings']
            if verdict not in ('migration-required', 'inferred-mismatch'):
                raise Refused('migration refused: no migration applies (' + verdict + '): ' + '; '.join(value['refusals']))
            stale = {name: entry for name, entry in found.items() if entry['state'] == 'bound' and mismatches({name: entry}, current)}
            if recorded is not None:
                third = mismatches(stale, recorded)
                if third:
                    raise Refused('migration refused: bindings belong to neither generation: ' + '; '.join(third))
                old = recorded['generation']
            else:
                _, claims = conflicts(stale)
                old = claims.get('generation') or 'inferred-' + digest(canonical(claims))
            archive = root / 'generations' / old
            retained(archive)
            moves = [[found[name]['path'], str(archive / 'files' / Path(found[name]['path']).relative_to(volume))]
                     for name in ARCHIVED if name in stale]
            for _, target in moves:
                if os.path.lexists(target):
                    raise Refused('migration refused: a retained copy already exists at ' + target)
            journal = {'schema': 'layerx.kernel.identity-migration.v1', 'plan_sha256': authorization, 'from': old,
                       'to': current['generation'], 'archive': str(archive), 'moves': moves,
                       'rebind': 'role-material' in stale, 'record': record_of(current),
                       'retained': value.get('durable', {})}
            make_parents(archive)
            write_atomic(migration_path, canonical(journal) + b'\n')
        for source, target in journal['moves']:
            move(Path(source), Path(target))
        if journal['rebind']:
            rebind_material(journal)
        for path, expected in journal['retained'].items():
            if not os.path.lexists(path) or summary(Path(path)) != expected:
                raise Refused('migration refused: durable Human state changed during migration: ' + path)
        write_atomic(Path(journal['archive']) / ('migration-' + journal['to'] + '.json'), canonical(journal) + b'\n')
        write_atomic(record_path, canonical(journal['record']) + b'\n')
        os.unlink(migration_path)
        sync(root)
        print(json.dumps({'from': journal['from'], 'to': journal['to'], 'archive': journal['archive']}))


try:
    if mode == 'plan':
        text = canonical(survey(arguments, durable=True))
        print(text.decode())
        print('plan_sha256=' + digest(text))
    elif mode == 'gate':
        gate()
    elif mode == 'retire':
        retire()
    elif mode == 'rotated':
        rotated()
    elif mode == 'migrate':
        migrate()
    else:
        raise Refused('usage: --identity-generation plan|gate|retire|rotated|migrate')
except (OSError, ValueError, KeyError, TypeError, Refused, subprocess.SubprocessError) as error:
    raise SystemExit('kernel-init: ' + (str(error) or type(error).__name__))
PY_IDENTITY
}

if [ "${1:-}" = --identity-generation ]; then
	shift
	identity_generation "$@"
	exit
fi

# fresh <file> <owner> <mode> <command...>: writes the command's output to the
# file unless it holds something already.
fresh() {
	local file=$1 owner=$2 mode=$3
	shift 3
	[ -s "$file" ] || { "$@" >"$file.new" && mv "$file.new" "$file"; }
	chown "$owner" "$file"
	chmod "$mode" "$file"
}

evm_key() {
	local key
	while :; do
		key="0x$(openssl rand -hex 32)"
		python3 /opt/layerx/paxeer/evm.py address /dev/stdin <<<"$key" >/dev/null 2>&1 && break
	done
	printf '%s' "$key"
}

memory() {
	if [ -L "$1" ] || { [ -e "$1" ] && [ ! -d "$1" ]; }; then
		log "private runtime mount refused: $1"
		return 1
	fi
	mkdir -p "$1"
	mountpoint -q "$1" || mount -t tmpfs -o "nosuid,nodev,mode=$2" tmpfs "$1"
}

# The mirror-signer and mirror-publisher containers' secrets, in the box env
# file, each base64: the Ethereum secp256k1 publisher key, the
# Solana ed25519 publisher keypair, the config interop/deploy/mirror/
# render-config.py rendered, and a tar of the <backend>.ca.der and
# <backend>.token files its RPC endpoints name under $mirror_run/rpc.
# mirror_inputs writes each present one to memory for uid 4021 and drops it
# from the environment every service inherits.
mirror_material=/run/mirror-material
mirror_run=/run/mirror-publisher

mirror_input() {
	local name=$1 file=$2
	[ -n "${!name:-}" ] || return 0
	base64 -d <<<"${!name}" >"$file" || {
		log "$name is not base64"
		exit 1
	}
	chown 4021:4020 "$file"
	chmod 0400 "$file"
}

mirror_inputs() {
	mirror_input LAYERX_KERNEL_MIRROR_ETHEREUM_KEY "$mirror_material/ethereum.key"
	mirror_input LAYERX_KERNEL_MIRROR_SOLANA_KEYPAIR "$mirror_material/solana.json"
	mirror_input LAYERX_KERNEL_MIRROR_CONFIG "$mirror_run/config.json"
	if [ -n "${LAYERX_KERNEL_MIRROR_RPC_CREDENTIALS:-}" ]; then
		install -d -o 4021 -g 4020 -m 0700 "$mirror_run/rpc"
		base64 -d <<<"$LAYERX_KERNEL_MIRROR_RPC_CREDENTIALS" | tar -x -C "$mirror_run/rpc" --no-same-owner --no-same-permissions || {
			log "LAYERX_KERNEL_MIRROR_RPC_CREDENTIALS is not a base64 tar"
			exit 1
		}
		chown -R 4021:4020 "$mirror_run/rpc"
		find "$mirror_run/rpc" -type f -exec chmod 0400 {} +
	fi
	unset LAYERX_KERNEL_MIRROR_ETHEREUM_KEY LAYERX_KERNEL_MIRROR_SOLANA_KEYPAIR LAYERX_KERNEL_MIRROR_CONFIG \
		LAYERX_KERNEL_MIRROR_RPC_CREDENTIALS
}

memory "$run" 0755
memory /run/authority-private 0700
if [ "$kernel_profile" = full ]; then
if [ -e /run/human-private ] && [ "$(stat -c '%u:%g:%a' /run/human-private)" != 0:0:755 ]; then
	log "private runtime directory refused: /run/human-private owner or mode"
	exit 1
fi
memory /run/human-private 0755
fi
memory /run/mirror-signer 0700
memory "$mirror_material" 0700
memory "$mirror_run" 0700
memory /tmp 1777
chown 4020:4020 "$run"
chmod 2775 "$run"
chown 4021:4020 /run/authority-private /run/mirror-signer "$mirror_material" "$mirror_run"
mirror_inputs
mkdir -p "$status" "$run/clock"
install -d -o 4020 -g 4020 -m 0750 "$run/node"
chmod 0755 "$status"
echo "$$" >"$status/pid"

install -d -o 0 -g 4020 -m 2775 "$layerx" "$layerx/settlement"
install -d -o 0 -g 4020 -m 0750 "$genesis"
chmod g-s "$genesis"
# The guarantors' settlement inputs: the chain id, the relayed Paxeer RPC URL
# and the three precompiles, unless the ceremony already wrote them.
fresh "$settlement_env" 0:4020 0440 printf '%s=%s\n' \
	LAYERX_NODE_PAXEER_CHAIN_ID "$LAYERX_NODE_PAXEER_CHAIN_ID" \
	LAYERX_NODE_PAXEER_RPC_URL "$LAYERX_NODE_PAXEER_RPC_URL" \
	LAYERX_NODE_REGISTRY_PRECOMPILE "$LAYERX_NODE_REGISTRY_PRECOMPILE" \
	LAYERX_NODE_CUSTODY_PRECOMPILE "$LAYERX_NODE_CUSTODY_PRECOMPILE" \
	LAYERX_NODE_ANCHOR_PRECOMPILE "$LAYERX_NODE_ANCHOR_PRECOMPILE"
if [ "$kernel_profile" = native ]; then
    chmod 3775 "$layerx"
    if [ -L "$layerx/trust" ] || { [ -e "$layerx/trust" ] && [ ! -d "$layerx/trust" ]; }; then
        log "native trust history directory refused"
        exit 1
    fi
    if [ -d "$layerx/trust" ] && [ "$(stat -c '%u:%g:%a' "$layerx/trust")" != 0:4020:750 ]; then
        log "native trust history directory owner or mode refused"
        exit 1
    fi
    install -d -o 0 -g 4020 -m 0750 "$layerx/trust"
fi
install -d -o 0 -g 4020 -m 0750 "$keys" "$keys/tokens"
install -d -o 0 -g 0 -m 0700 "$keys/checkpoint-authority" "$keys/publication"
if [ "$kernel_profile" = full ]; then
    install -d -o 0 -g 0 -m 0700 "$keys/human-authority"
fi
install -d -o 4021 -g 4020 -m 0750 "$keys/checkpoint-submitter"
install -d -o 4021 -g 4020 -m 0700 "$layerx/mirror"
install -d -o 0 -g 4020 -m 0711 "$tls"

# guarantor-storage
install -d -o 4020 -g 4020 -m 2770 "$layerx/guarantor-submitter"
for identity in 1 2; do
	install -d -o 4020 -g 4020 -m 0750 "$layerx/guarantor-$identity" "$layerx/guarantor-$identity/identity"
	install -d -o 4020 -g 4020 -m 2770 "$layerx/guarantor-$identity/state"
done

# human-directories
private_runtime_directories() {
	python3 - <<'PY_PRIVATE'
import os
import stat


def directory(parent, name, uid, gid, mode):
    created = False
    try:
        os.mkdir(name, mode, dir_fd=parent)
        created = True
    except FileExistsError:
        pass
    fd = os.open(name, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW, dir_fd=parent)
    try:
        info = os.fstat(fd)
        if created:
            os.fchown(fd, uid, gid)
            os.fchmod(fd, mode)
        elif (info.st_uid, info.st_gid, stat.S_IMODE(info.st_mode)) != (uid, gid, mode):
            raise ValueError("unexpected owner or mode: " + name)
        return fd
    except BaseException:
        os.close(fd)
        raise


try:
    if os.geteuid() != 0:
        raise ValueError("root initialization required")
    run = os.open("/run", os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
    private = directory(run, "human-private", 0, 0, 0o755)
    for role, uid in (("components", 4020), ("identity", 4020),
                      ("security", 4020), ("movement", 4020),
                      ("agent", 4021), ("kms", 4026), ("service", 4020)):
        os.close(directory(private, role, uid, 4020, 0o700))
    for fd in (private, run):
        os.close(fd)
except (OSError, ValueError) as error:
    raise SystemExit("private runtime directory refused: " + str(error))
PY_PRIVATE
}

if [ "$kernel_profile" = full ]; then
private_runtime_directories
export LAYERX_HUMAN_SERVICE_PRIVATE_DIR=/run/human-private/service
install -d -o 4020 -g 4020 -m 0750 "$run/human"
install -d -o 4021 -g 4020 -m 0750 "$run/human/owner"
install -d -o 4021 -g 4020 -m 0700 "$run/human/authority-clock"
install -d -o 0 -g 4020 -m 0750 "$human_state"
install -d -o 4020 -g 4020 -m 0700 "$human_state/components" "$human_state/identity" "$human_state/security" \
	"$human_state/movement" "$human_state/movement/evidence"
install -d -o 4021 -g 4020 -m 0700 "$human_state/agent" "$human_state/authority"
install -d -o 4026 -g 4020 -m 0700 "$human_state/kms"
# The per-role material of the pod's human-*-material secrets, projected by the
# root prepare step of each role, and the mount points of service().
human_material=$run/human-material
install -d -o 0 -g 4020 -m 0751 "$human_material"
install -d -m 0755 /run/human-material /var/lib/layerx/human
install -d -o 0 -g 0 -m 0700 "$keys/human-policy"
fi

# The internal CA root, bind-mounted read-only by the box unit.
ca_cert_file=${LAYERX_CA_CERT_FILE:-/etc/layerx/trust/ca.crt}
[ -f "$ca_cert_file" ] && [ -s "$ca_cert_file" ] || {
	log "the internal CA root $ca_cert_file is absent; bind-mount it read-only"
	exit 1
}
install -d -m 0755 "$run/trust"
install -m 0444 "$ca_cert_file" "$run/trust/ca.crt"

# Material the pod read from secrets and the machine now makes on the volume.
fresh "$keys/treasury.key" 4020:4020 0400 openssl rand -hex 32
# The kernel bearers: layerxd's program and replica tokens, the core admin
# plane, the router's component and authority bearers and the webhooks
# component and authority bearers. LAYERX_KERNEL_<TOKEN>_TOKEN from the box env
# file (tools/bringup/mint-secrets.sh) replaces the volume copy; a token whose
# variable is unset is generated once on the volume.
for token in program-token replica-token backend-admin gateway-component gateway-authority webhooks-component webhooks-authority; do
	variable=${token%-token}
	variable=LAYERX_KERNEL_${variable^^}
	variable=${variable//-/_}_TOKEN
	if [ -n "${!variable:-}" ]; then
		printf '%s' "${!variable}" >"$keys/tokens/$token.new"
		mv "$keys/tokens/$token.new" "$keys/tokens/$token"
	fi
	unset "$variable"
	fresh "$keys/tokens/$token" 4020:4020 0440 openssl rand -hex 32
done
if [ "$kernel_profile" = full ]; then
    fresh "$keys/human-authority/authority-token" 0:0 0600 openssl rand -hex 32
    fresh "$keys/human-authority/explorer-evidence-read" 0:0 0600 printf %s "$(openssl rand -hex 32)"
fi
install -d -o 4021 -g 4020 -m 0700 "$layerx/core" "$layerx/agent-boundary"
fresh "$keys/checkpoint-submitter/key" 4021:4020 0400 evm_key

# The registry's two bearers, in the box env file of this box and of the
# registry: the agent boundary reads the node bearer
# from registry-component/token and the receipt authority the authority bearer
# from registry-authority/token of LAYERX_AUTHORITY_TOKEN_FILES, where the pod
# mounted the layerx-program-registry-node-client and
# layerx-program-registry-authority-client secrets. Neither reaches a service's
# environment.
native_registry_bearer() {
    local name=$1 source=$keys/tokens/$1 directory=$run/$1
    python3 - "$source" <<'PY_REGISTRY_TOKEN'
import os
import secrets
import stat
import sys

path = sys.argv[1]
try:
    descriptor = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
except FileExistsError:
    pass
else:
    with os.fdopen(descriptor, "wb") as output:
        output.write(secrets.token_hex(32).encode("ascii"))
        output.flush()
        os.fsync(output.fileno())
with os.fdopen(os.open(path, os.O_RDONLY | os.O_NOFOLLOW), "rb") as source:
    info = os.fstat(source.fileno())
    if (not stat.S_ISREG(info.st_mode) or info.st_uid != 0 or info.st_gid != 0
            or stat.S_IMODE(info.st_mode) != 0o600 or info.st_nlink != 1 or info.st_size != 64):
        raise SystemExit("native registry token file is not protected")
    value = source.read(65)
    if len(value) != 64 or any(byte not in b"0123456789abcdef" for byte in value):
        raise SystemExit("native registry token file is invalid")
PY_REGISTRY_TOKEN
    install -d -o 4021 -g 4020 -m 0750 "$directory"
    install -o 4021 -g 4020 -m 0440 "$source" "$directory/token"
}

registry_bearer() {
	local variable=$1 directory=$run/$2
	if [ -z "${!variable:-}" ]; then
        if [ "$kernel_profile" = native ]; then
            native_registry_bearer "$2"
        else
		    log "$variable is unset; the program registry cannot authenticate until its deploy step imports it"
        fi
	else
		install -d -o 4021 -g 4020 -m 0750 "$directory"
		printf '%s' "${!variable}" >"$directory/token"
		chown 4021:4020 "$directory/token"
		chmod 0440 "$directory/token"
	fi
	unset "$variable"
}
registry_bearer LAYERX_REGISTRY_NODE_AUTHORIZATION registry-component
registry_bearer LAYERX_REGISTRY_RECEIPT_AUTHORITY_AUTHORIZATION registry-authority

# checkpoint_authority: the guarantor-checkpoint-authority init container.
checkpoint_authority() {
	local source=$keys/checkpoint-authority/key.pem target=$layerx/guarantor-submitter/checkpoint-authority.pem
	[ -e "$target" ] || install -o 4021 -g 4020 -m 0600 "$source" "$target"
	cmp -s "$source" "$target" || {
		log "the volume holds a different checkpoint authority key than $source"
		return 1
	}
}

# publication_policy: the publication-policy init container.
publication_policy() {
	install -d -o 4020 -g 4020 -m 0700 "$run/publication"
	install -o 4020 -g 4020 -m 0600 "$keys/publication/binding-policy.json" "$run/publication/binding-policy.json"
}

human_evidence_read_material() {
	local source=$keys/human-authority/explorer-evidence-read material=/run/authority-private/material
	if [ -L "$source" ] || [ ! -f "$source" ] || [ "$(stat -c '%u:%g:%a:%h' "$source")" != 0:0:600:1 ]; then
		log "explorer evidence-read token refused: owner, type or mode"
		return 1
	fi
	install -d -o 4021 -g 4020 -m 0700 "$material"
	install -o 4021 -g 4020 -m 0600 "$source" "$material/evidence-read.token"
}

# human_authority_material: the human-authority-material init container.
human_authority_material() {
	local input=$keys/human-authority material=/run/authority-private/material
	human_evidence_read_material || return 1
	install -o 4021 -g 4020 -m 0600 "$input/authority-token" "$material/human-agent.token"
	install -o 4021 -g 4020 -m 0600 "$input/principal-policy.json" "$material/principal-policy.json"
	install -o 4021 -g 4020 -m 0600 "$input/registry.json" "$material/registry.json"
	install -o 4021 -g 4020 -m 0600 "$input/authority.json" "$material/authority.json"
	if [ -e "$input/genesis-handover-trust.lxt" ] || [ -e "$input/handover-finality.conf" ]; then
		install -o 4021 -g 4020 -m 0600 "$input/genesis-handover-trust.lxt" "$material/genesis-handover-trust.lxt"
		install -o 4021 -g 4020 -m 0600 "$input/handover-finality.conf" "$material/handover-finality.conf"
	fi
}

# tls_for <service> <uid>: hands the identity ca.sh wrote as root to the uid.
tls_for() {
	chown -R "$2:4020" "$tls/$1"
	chmod 0711 "$tls"
}

# missing <paths...>: prints the first path that does not exist.
missing() {
	local path
	for path in "$@"; do
		[ -e "$path" ] || {
			printf '%s' "$path"
			return 0
		}
	done
	return 1
}

# service <name> <uid> <waits> <prepare> <clock> -- <command...>: runs the
# command under the uid once every path of the space-separated waits exists,
# after the root prepare step ("-" for none), inside the runtime clock unless
# clock is "-" (a command that enters the clock itself), and restarts it when
# it exits.
service() {
	local name=$1 uid=$2 waits=$3 prepare=$4 clock=$5 absent wrap=() ns=() pid rc mask_one mask_two human_source
	shift 6
	# human_root=<dir> before a call: the pod's per-role mounts, the role's
	# projected material at /run/human-material and <dir> at
	# /var/lib/layerx/human, in the service's own mount namespace.
	# shellcheck disable=SC2016 # the arguments expand in the namespace's shell
	[ -z "${human_root:-}" ] || ns=(unshare --mount --propagation private -- /bin/sh -ec \
		'mount --bind "$1" /run/human-material && mount --bind "$2" /var/lib/layerx/human && shift 2 && exec "$@"' \
		sh "$human_material/$name" "$human_root")
	if [ "$uid" = 4021 ]; then
		mask_one="$layerx/guarantor-1"
		mask_two="$layerx/guarantor-2"
		case "${guarantor_identity:-}" in
		1) mask_one=- ;;
		2) mask_two=- ;;
		"") ;;
		*) return 1 ;;
		esac
		install -d -o 0 -g 0 -m 0700 "$run/generation-isolation"
		human_source=-
		[ -z "${human_root:-}" ] || human_source="$human_material/$name"
		ns=(unshare --mount --pid --fork --kill-child=TERM --propagation private -- /bin/sh -ec \
			'mount -t proc proc /proc
for target in "$2" "$3"; do
    [ "$target" = - ] || { mount --bind "$1" "$target" && mount -o remount,bind,ro "$target"; }
done
if [ "$4" != - ]; then
    mount --bind "$4" /run/human-material
    mount --bind "$5" /var/lib/layerx/human
fi
shift 5
exec "$@"' sh "$run/generation-isolation" "$mask_one" "$mask_two" \
			"$human_source" "${human_root:--}")
	fi
	[ "$clock" = - ] || {
		install -d -o "$uid" -g 4020 -m 0700 "$run/clock/$name"
		wrap=(/usr/local/bin/layerx-runtime-clock --runtime-dir "$run/clock/$name" --)
	}
	(
		trap - TERM INT
		while :; do
			# shellcheck disable=SC2086
			if { [ "$name" = human-security ] && absent="$(human_security_prerequisite)"; } ||
				{ [ "$name" != human-security ] && absent="$(missing $waits)"; }; then
				case " $genesis_files " in
				*" $absent "*) echo "$uid waiting genesis" ;;
				*) echo "$uid waiting $absent" ;;
				esac >"$status/$name"
				sleep 5
				continue
			fi
			if [ "$prepare" != - ] && ! "$prepare"; then
				echo "$uid waiting $prepare" >"$status/$name"
				sleep 5
				continue
			fi
			${ns[@]+"${ns[@]}"} setpriv --reuid="$uid" --regid=4020 --clear-groups --no-new-privs --pdeathsig TERM \
				${wrap[@]+"${wrap[@]}"} "$@" &
			pid=$!
			echo "$uid running $pid" >"$status/$name"
			rc=0
			wait "$pid" || rc=$?
			log "$name exited with status $rc; restarting"
			sleep 2
		done
	) &
}

guarantor() {
	local identity=$1 port=$2 peer=$3
	guarantor_identity=$identity service "guarantor-$identity" 4021 \
		"$genesis_files $run/node/generation.sock $layerx/guarantor-$identity/identity/generation.cap $tls/guarantor/cert.pem $keys/checkpoint-authority/key.pem $keys/publication/authorization.json" \
		guarantor_prepare clock -- \
		env \
		LAYERX_GUARANTOR_IDENTITY_DIR="$layerx/guarantor-$identity/identity" \
		LAYERX_GUARANTOR_STATE_DIR="$layerx/guarantor-$identity/state" \
		LAYERX_GUARANTOR_LNI_SOCKET="$run/node/layerxd.lni.sock" \
		LAYERX_GUARANTOR_SETTLEMENT_ENV="$settlement_env" \
		LAYERX_GUARANTOR_SETTLEMENT_FILE="$layerx/settlement/checkpoint-settlement.json" \
		LAYERX_GUARANTOR_SETTLEMENT_DOMAIN=beta \
		LAYERX_GUARANTOR_LISTEN_PORT="$port" \
		LAYERX_GUARANTOR_PEER_URL="https://127.0.0.1:$peer" \
		LAYERX_GUARANTOR_TLS_CA_FILE="$tls/guarantor/ca.pem" \
		LAYERX_GUARANTOR_TLS_CERT_FILE="$tls/guarantor/cert.pem" \
		LAYERX_GUARANTOR_TLS_KEY_FILE="$tls/guarantor/key.pem" \
		LAYERX_GUARANTOR_SUBMITTER_KEY_FILE="$keys/checkpoint-submitter/key" \
		LAYERX_GUARANTOR_SUBMITTER_LOCK_FILE="$layerx/guarantor-submitter/submitter.lock" \
		LAYERX_GUARANTOR_CHECKPOINT_AUTHORITY_KEY_FILE="$layerx/guarantor-submitter/checkpoint-authority.pem" \
		LAYERX_GUARANTOR_PUBLICATION_AUTHORIZATION_SOURCE="$layerx/guarantor-submitter/publication-authorization.json" \
		LAYERX_GUARANTOR_PYTHON=/opt/layerx/guarantor/venv/bin/python3 \
		python3 /opt/layerx/node/generation_client.py identity --socket "$run/node/generation.sock" \
		--capability-file "$layerx/guarantor-$identity/identity/generation.cap" --slot "$identity" \
		--watch-seconds 1 -- /bin/bash /opt/layerx/guarantor.sh
}

# The publication authorization of kernel-genesis.sh, handed from the root-only
# publication directory to the guarantor uid that installs it.
guarantor_prepare() {
	checkpoint_authority && tls_for guarantor 4021 &&
		install -o 4021 -g 4020 -m 0600 "$keys/publication/authorization.json" \
			"$layerx/guarantor-submitter/publication-authorization.json"
}

human_authority_ready() {
	while missing "$keys/human-authority/authority-token" "$keys/human-authority/principal-policy.json" \
		"$keys/human-authority/registry.json" "$keys/human-authority/authority.json" >/dev/null ||
		! identity_generation gate; do
		sleep 5
	done
	human_authority_material
	log "human authority material installed"
}

# The Paxeer side of the pod: two layerx-paxeer-boundary processes on chain
# 125, each fronting its own serving RPC name through its own loopback socat
# hop to port 443 of that name, verified against the system CA with the name
# as SNI, and the pod's paxeer relay on the relay port dialing the first.
# LAYERX_KERNEL_PAXEER_RPC_NAMES holds the two serving RPC names, first one
# first.
paxeer_boundaries=(paxeer-boundary-loopback paxeer-boundary-public)
paxeer_boundary_ports=(9447 9448)
paxeer_hop_ports=(18546 18547)
paxeer_prepares=(paxeer_boundary_loopback_prepare paxeer_boundary_public_prepare)
system_ca=/etc/ssl/certs/ca-certificates.crt

paxeer_boundary_loopback_prepare() {
	tls_for paxeer-boundary-loopback 4020
}

paxeer_boundary_public_prepare() {
	tls_for paxeer-boundary-public 4020
}

start_paxeer() {
	local k name boundary
	for k in 0 1; do
		name="${paxeer_rpc_names[$k]}"
		boundary="${paxeer_boundaries[$k]}"
		service "paxeer-hop-$((k + 1))" 4020 "" - - -- \
			socat -T 120 "TCP4-LISTEN:${paxeer_hop_ports[$k]},bind=127.0.0.1,reuseaddr,fork" \
			"OPENSSL:$name:443,cafile=$system_ca,verify=1,snihost=$name,commonname=$name"
		service "$boundary" 4020 "$tls/$boundary/cert.der $tls/$boundary/key.der $tls/$boundary/ca.pem" \
			"${paxeer_prepares[$k]}" - -- \
			env \
			"LAYERX_PAXEER_BOUNDARY_LISTEN=127.0.0.1:${paxeer_boundary_ports[$k]}" \
			"LAYERX_PAXEER_NODE_URL=http://127.0.0.1:${paxeer_hop_ports[$k]}" \
			"LAYERX_PAXEER_CHAIN_ID=$LAYERX_NODE_PAXEER_CHAIN_ID" \
			"LAYERX_PAXEER_BOUNDARY_TLS_CERT_DER=$tls/$boundary/cert.der" \
			"LAYERX_PAXEER_BOUNDARY_TLS_KEY_DER=$tls/$boundary/key.der" \
			/usr/local/bin/layerx-paxeer-boundary
	done
	service paxeer-relay 4020 "$tls/${paxeer_boundaries[0]}/ca.pem" "${paxeer_prepares[0]}" - -- \
		socat -T 120 "TCP4-LISTEN:$LAYERX_NODE_PAXEER_RELAY_PORT,bind=127.0.0.1,reuseaddr,fork" \
		"OPENSSL:127.0.0.1:${paxeer_boundary_ports[0]},cafile=$tls/${paxeer_boundaries[0]}/ca.pem,verify=1,commonname=localhost"
}

trap 'kill 0' TERM INT

service treasury-signer 4020 "$keys/treasury.key $keys/publication/binding-policy.json" publication_policy clock -- \
	python3 /opt/layerx/signer/signer.py \
	--socket "$run/node/treasury-signer.sock" \
	--provider file \
	--key-file "$keys/treasury.key" \
	--allowed-uid 4020,4021 \
	--socket-group 4020 \
	--public-key-file "$run/node/treasury-public-key" \
	--binding-policy "$run/publication/binding-policy.json"

# kernel_ids: the generated asset and replica ids, refused when either is not
# 64 hex characters or is the old beta constant.
kernel_ids() {
	local id
	for id in "$(cat "$genesis/asset-id")" "$(cat "$genesis/replica-id")"; do
		case "$id" in
		b5a32b12029f8ddfb905f90f280f664b46390de0fc62770fc197dd87b18cd898 | 6c61796572782d626574612d726563656970742d617574686f726974792d3031)
			log "the genesis ids hold the old beta constant $id; run kernel-genesis.sh rotate"
			return 1
			;;
		esac
		[[ $id =~ ^[0-9a-f]{64}$ ]] || return 1
	done
}

# shellcheck disable=SC2016 # the ids expand when the service starts
service layerxd 4020 "$genesis_files" kernel_ids clock -- \
	/bin/sh -c 'if [ -e '"$genesis"'/custody.registry ]; then set -- "$@" --custody-registry '"$genesis"'/custody.registry; fi; exec /opt/layerx/supervisor.sh "$@" --asset "$(cat '"$genesis"'/asset-id)" --replica-id "$(cat '"$genesis"'/replica-id)"' layerxd \
	--role sequencer --data-dir "$node_data" --run-dir "$run/node" -- \
	--network-id "$LAYERX_NODE_NETWORK_ID" \
	--genesis-metadata "$genesis/metadata.lxgb" \
	--custody-profile "$genesis/custody.profile" \
	--withdrawal-fee 0 \
	--module-fees /opt/layerx/genesis-module-fees.json \
	--sequencer-key "$keys/sequencer.key" \
	--treasury-signer-socket "$run/node/treasury-signer.sock" \
	--program-token-file "$keys/tokens/program-token" \
	--replica-token-file "$keys/tokens/replica-token" \
	--program-port 9401 \
	--replica-port 9402 \
	--lni-uid 4021 \
	--lni-gid 4020

service layerxd-authority 4020 "$genesis_files" - clock -- \
	/opt/layerx/supervisor.sh --role replica --data-dir "$node_data" --run-dir "$run/node"

guarantor 1 9451 9452
guarantor 2 9452 9451

start_paxeer

# The core-boundary, receipt-authority and agent-boundary containers of the
# pod, each on [::] for the private network with the identity tools/bringup/
# ca.sh issued on the volume under its row (pending-core, pending-core-admin,
# receipt-authority, agent-boundary), verifying clients under the internal CA.
# No service of the app's toml exposes 9443 to 9446. LAYERX_NODE_NETWORK_NAME,
# set on the app, is the network name the router's LAYERX_GATEWAY_NETWORK_ID
# expects from these backends.
authority_material=/run/authority-private/material

network_name() {
	[ -n "${LAYERX_NODE_NETWORK_NAME:-}" ] || {
		log "LAYERX_NODE_NETWORK_NAME is unset; the receipt authority and the agent boundary wait for it"
		return 1
	}
}

core_boundary_prepare() {
	tls_for pending-core 4021 && tls_for pending-core-admin 4021
}

# The sequencer public key the pod mounted as gateway-authority/
# sequencer-public-key, derived from the sequencer seed as kernel-genesis.sh
# derives it.
kernel_registry_material=$layerx/registry-material
kernel_registry_identity() {
	{ flock 8 && identity_generation gate && python3 /usr/local/lib/layerx-human/material.py --kernel-registry-material-produce \
		"$genesis" "$keys/sequencer.key" "$kernel_registry_material" "$LAYERX_NODE_NETWORK_ID" "$trust_history_file"; } \
		8>"$layerx/registry-material.lock" || return 1
	python3 - "$kernel_registry_material" "$run/node/sequencer-public-key" "$trust_history_file" <<'PY_REGISTRY_IDENTITY'
import os
from pathlib import Path
import stat
import sys
sys.path.insert(0, '/usr/local/lib/layerx-human')
from material import verify_registry_material
source, public, retained = map(Path, sys.argv[1:])
selected = verify_registry_material(source)
manifest = selected['manifest']
for target, raw, mode in ((public, manifest['sequencer_public_key'].encode(), 0o444),
                          (retained, selected['files']['trust-history'], 0o440)):
    if target.exists() or target.is_symlink():
        info = target.lstat()
        if (target.resolve() != target or not stat.S_ISREG(info.st_mode) or info.st_nlink != 1
                or info.st_uid != 0 or info.st_mode & 0o022 or target.read_bytes() != raw):
            raise SystemExit('retained kernel identity differs; preserving reconciliation required')
        continue
    pending = target.with_name(target.name + '.new')
    fd = os.open(pending, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, mode)
    with os.fdopen(fd, 'wb') as output:
        output.write(raw)
        output.flush()
        os.fsync(output.fileno())
    os.chown(pending, 0, 4020)
    os.chmod(pending, mode)
    os.link(pending, target, follow_symlinks=False)
    pending.unlink()
    directory = os.open(target.parent, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
    os.fsync(directory)
    os.close(directory)
PY_REGISTRY_IDENTITY
}

receipt_authority_prepare() {
	network_name && tls_for receipt-authority 4021 || return 1
	kernel_registry_identity
}

receipt_authority_native_prepare() {
    receipt_authority_prepare || return 1
    install -d -o 4021 -g 4020 -m 0700 "$run/authority-clock" || return 1
    if [ -e "$node_data/genesis/genesis-handover-trust.lxt" ]; then
        [ -n "${LAYERX_AUTHORITY_HANDOVER_FINALITY:-}" ] && [ -f "$LAYERX_AUTHORITY_HANDOVER_FINALITY" ] || {
            log "native handover genesis requires an independent finality policy"
            return 1
        }
        if [ -n "${LAYERX_AUTHORITY_GENESIS_TRUST:-}" ]; then
            cmp -s "$node_data/genesis/genesis-handover-trust.lxt" "$LAYERX_AUTHORITY_GENESIS_TRUST" || return 1
        fi
        install -d -o 4021 -g 4020 -m 0700 "$authority_material" || return 1
        install -o 4021 -g 4020 -m 0600 "$node_data/genesis/genesis-handover-trust.lxt" "$authority_material/genesis-handover-trust.lxt" || return 1
        if [ "$LAYERX_AUTHORITY_HANDOVER_FINALITY" != "$authority_material/handover-finality.conf" ]; then
            install -o 4021 -g 4020 -m 0600 "$LAYERX_AUTHORITY_HANDOVER_FINALITY" "$authority_material/handover-finality.conf" || return 1
        fi
        export LAYERX_AUTHORITY_GENESIS_TRUST="$authority_material/genesis-handover-trust.lxt"
        export LAYERX_AUTHORITY_HANDOVER_FINALITY="$authority_material/handover-finality.conf"
    fi
}

agent_boundary_prepare() {
	network_name && tls_for agent-boundary 4021
}

# shellcheck disable=SC2016 # core.env is read when the service starts
service core-boundary 4021 \
	"$genesis_files $run/node/generation.sock $tls/pending-core/cert.der $tls/pending-core/key.der $tls/pending-core/ca.der $tls/pending-core-admin/cert.der $tls/pending-core-admin/key.der" \
	core_boundary_prepare - -- \
	env \
	"LAYERX_CORE_LISTEN=[::]:9443" \
	"LAYERX_CORE_ADMIN_LISTEN=[::]:9444" \
	LAYERX_CORE_NETWORK_ID="$LAYERX_NODE_NETWORK_ID" \
	LAYERX_CORE_TLS_CERT_DER="$tls/pending-core/cert.der" \
	LAYERX_CORE_TLS_KEY_DER="$tls/pending-core/key.der" \
	LAYERX_CORE_ADMIN_TLS_CERT_DER="$tls/pending-core-admin/cert.der" \
	LAYERX_CORE_ADMIN_TLS_KEY_DER="$tls/pending-core-admin/key.der" \
	LAYERX_CORE_CLIENT_CA_DER="$tls/pending-core/ca.der" \
	LAYERX_CORE_LNI_SOCKET="$run/node/layerxd.lni.sock" \
	LAYERX_CORE_SUPERVISOR_SOCKET="$run/node/supervisor.sock" \
	LAYERX_CORE_NODE_URL=http://127.0.0.1:9401 \
	LAYERX_CORE_NODE_BEARER_TOKEN_FILE="$keys/tokens/program-token" \
	LAYERX_CORE_REPLICA_URL=http://127.0.0.1:9402 \
	LAYERX_CORE_REPLICA_BEARER_TOKEN_FILE="$keys/tokens/replica-token" \
	LAYERX_CORE_ADMIN_TOKEN_FILE="$keys/tokens/backend-admin" \
	LAYERX_CORE_RECEIPT_EVENTS_TOKEN_FILE="$keys/tokens/gateway-component" \
	LAYERX_CORE_STATE_DIR="$layerx/core" \
	python3 /opt/layerx/node/generation_client.py core --socket "$run/node/generation.sock" --watch-seconds 1 -- \
	/bin/sh -ec '
: "${LAYERX_CORE_SEQUENCER_ID:?generated sequencer identity is required}"
: "${LAYERX_CORE_TREASURY_ASSET:?generated treasury asset is required}"
: "${LAYERX_CORE_TREASURY_SIGNER_SOCKET:?treasury signer socket is required}"
exec /usr/local/bin/layerx-core-boundary'

# The receipt authority enters the runtime clock itself, as its container did.
# shellcheck disable=SC2016 # core.env and the material are read when the service starts
if [ "$kernel_profile" = full ]; then
service receipt-authority 4021 \
	"$genesis_files $run/node/generation.sock $run/node/layerxd.lni.sock $tls/receipt-authority/cert.der $tls/receipt-authority/key.der $tls/receipt-authority/ca.der $run/registry-authority/token $authority_material/human-agent.token $authority_material/evidence-read.token $authority_material/principal-policy.json $authority_material/registry.json $authority_material/authority.json" \
	receipt_authority_prepare - -- \
	env \
	"LAYERX_AUTHORITY_LISTEN=[::]:9445" \
	LAYERX_AUTHORITY_PROTOCOL_NETWORK_ID="$LAYERX_NODE_NETWORK_ID" \
	LAYERX_AUTHORITY_TLS_CERT_DER="$tls/receipt-authority/cert.der" \
	LAYERX_AUTHORITY_TLS_KEY_DER="$tls/receipt-authority/key.der" \
	LAYERX_AUTHORITY_CLIENT_CA_DER="$tls/receipt-authority/ca.der" \
	LAYERX_AUTHORITY_HUMAN_AGENT_TOKEN_FILE="$authority_material/human-agent.token" \
	LAYERX_AUTHORITY_IDENTITY_BINDING_SOCKET="$run/human/identity-binding.sock" \
	LAYERX_AUTHORITY_IDENTITY_BINDING_UID=4020 \
	LAYERX_AUTHORITY_IDENTITY_BINDING_GID=4020 \
	LAYERX_AUTHORITY_PRINCIPAL_POLICY_FILE="$authority_material/principal-policy.json" \
	LAYERX_AUTHORITY_MODULE_REGISTRY_FILE="$authority_material/registry.json" \
	LAYERX_AUTHORITY_STATE_ROOT="$human_state/authority" \
	LAYERX_AUTHORITY_TOKEN_FILES="$keys/tokens/gateway-authority:$run/registry-authority/token:$keys/tokens/webhooks-authority" \
	LAYERX_AUTHORITY_EVIDENCE_READ_TOKEN_FILE="$authority_material/evidence-read.token" \
	LAYERX_AUTHORITY_REPLICA_URL=http://127.0.0.1:9402 \
	LAYERX_AUTHORITY_REPLICA_BEARER_TOKEN_FILE="$keys/tokens/replica-token" \
	LAYERX_AUTHORITY_LNI_SOCKET="$run/node/layerxd.lni.sock" \
	LAYERX_AUTHORITY_FIRST_BATCH=1 \
	LAYERX_AUTHORITY_LAST_BATCH=18446744073709551615 \
	python3 /opt/layerx/node/generation_client.py core --socket "$run/node/generation.sock" --watch-seconds 1 -- \
	/bin/sh -ec '
: "${LAYERX_CORE_SEQUENCER_ID:?generated sequencer identity is required}"
m='"$authority_material"'
LAYERX_AUTHORITY_NETWORK_ID=$LAYERX_NODE_NETWORK_NAME
: "${LAYERX_AUTHORITY_SEQUENCER_ID:?generation sequencer identity is required}"
: "${LAYERX_AUTHORITY_SEQUENCER_PUBLIC_KEY:?generation sequencer public key is required}"
: "${LAYERX_AUTHORITY_REPLICA_ID:?generation replica identity is required}"
[ "$LAYERX_AUTHORITY_SEQUENCER_ID" = "$LAYERX_CORE_SEQUENCER_ID" ]
LAYERX_AUTHORITY_HUMAN_AGENT_TENANT=$(jq -er .tenant "$m/authority.json")
LAYERX_AUTHORITY_HUMAN_AGENT_PRINCIPAL=$(jq -er .principal "$m/authority.json")
LAYERX_AUTHORITY_CORE_CLOCK_HORIZON=$(jq -er ".\"core-clock-horizon\"" "$m/authority.json")
export LAYERX_AUTHORITY_NETWORK_ID LAYERX_AUTHORITY_SEQUENCER_ID LAYERX_AUTHORITY_SEQUENCER_PUBLIC_KEY LAYERX_AUTHORITY_REPLICA_ID \
	LAYERX_AUTHORITY_HUMAN_AGENT_TENANT LAYERX_AUTHORITY_HUMAN_AGENT_PRINCIPAL LAYERX_AUTHORITY_CORE_CLOCK_HORIZON
if [ -e "$m/genesis-handover-trust.lxt" ]; then
	export LAYERX_AUTHORITY_GENESIS_TRUST="$m/genesis-handover-trust.lxt" LAYERX_AUTHORITY_HANDOVER_FINALITY="$m/handover-finality.conf"
fi
exec /usr/local/bin/layerx-runtime-clock --runtime-dir '"$run"'/human/authority-clock -- /usr/local/bin/layerx-receipt-authority'
else
service receipt-authority 4021 \
	"$genesis_files $run/node/generation.sock $run/node/layerxd.lni.sock $tls/receipt-authority/cert.der $tls/receipt-authority/key.der $tls/receipt-authority/ca.der $run/registry-authority/token" \
	receipt_authority_native_prepare - -- \
	env \
	"LAYERX_AUTHORITY_LISTEN=[::]:9445" \
	LAYERX_AUTHORITY_PROTOCOL_NETWORK_ID="$LAYERX_NODE_NETWORK_ID" \
	LAYERX_AUTHORITY_TLS_CERT_DER="$tls/receipt-authority/cert.der" \
	LAYERX_AUTHORITY_TLS_KEY_DER="$tls/receipt-authority/key.der" \
	LAYERX_AUTHORITY_CLIENT_CA_DER="$tls/receipt-authority/ca.der" \
	LAYERX_AUTHORITY_TOKEN_FILES="$keys/tokens/gateway-authority:$run/registry-authority/token:$keys/tokens/webhooks-authority" \
	LAYERX_AUTHORITY_REPLICA_URL=http://127.0.0.1:9402 \
	LAYERX_AUTHORITY_REPLICA_BEARER_TOKEN_FILE="$keys/tokens/replica-token" \
	LAYERX_AUTHORITY_LNI_SOCKET="$run/node/layerxd.lni.sock" \
	LAYERX_AUTHORITY_FIRST_BATCH=1 \
	LAYERX_AUTHORITY_LAST_BATCH=18446744073709551615 \
	python3 /opt/layerx/node/generation_client.py core --socket "$run/node/generation.sock" --watch-seconds 1 -- \
	/bin/sh -ec '
: "${LAYERX_CORE_SEQUENCER_ID:?generated sequencer identity is required}"
LAYERX_AUTHORITY_NETWORK_ID=$LAYERX_NODE_NETWORK_NAME
: "${LAYERX_AUTHORITY_SEQUENCER_ID:?generation sequencer identity is required}"
: "${LAYERX_AUTHORITY_SEQUENCER_PUBLIC_KEY:?generation sequencer public key is required}"
: "${LAYERX_AUTHORITY_REPLICA_ID:?generation replica identity is required}"
[ "$LAYERX_AUTHORITY_SEQUENCER_ID" = "$LAYERX_CORE_SEQUENCER_ID" ]
export LAYERX_AUTHORITY_NETWORK_ID LAYERX_AUTHORITY_SEQUENCER_ID LAYERX_AUTHORITY_SEQUENCER_PUBLIC_KEY LAYERX_AUTHORITY_REPLICA_ID
exec /usr/local/bin/layerx-runtime-clock --runtime-dir '"$run"'/authority-clock -- /usr/local/bin/layerx-receipt-authority'
fi

# shellcheck disable=SC2016 # the network name is read when the service starts
service agent-boundary 4021 \
	"$genesis_files $run/node/layerxd.lni.sock $tls/agent-boundary/cert.der $tls/agent-boundary/key.der $tls/agent-boundary/ca.der $run/registry-component/token" \
	agent_boundary_prepare - -- \
	env \
	"LAYERX_AGENT_BOUNDARY_LISTEN=[::]:9446" \
	LAYERX_AGENT_BOUNDARY_PROTOCOL_NETWORK_ID="$LAYERX_NODE_NETWORK_ID" \
	LAYERX_AGENT_BOUNDARY_TLS_CERT_DER="$tls/agent-boundary/cert.der" \
	LAYERX_AGENT_BOUNDARY_TLS_KEY_DER="$tls/agent-boundary/key.der" \
	LAYERX_AGENT_BOUNDARY_CLIENT_CA_DER="$tls/agent-boundary/ca.der" \
	LAYERX_AGENT_BOUNDARY_GATEWAY_TOKEN_FILE="$keys/tokens/gateway-component" \
	LAYERX_AGENT_BOUNDARY_WEBHOOK_TOKEN_FILE="$keys/tokens/webhooks-component" \
	LAYERX_AGENT_BOUNDARY_REGISTRY_TOKEN_FILE="$run/registry-component/token" \
	LAYERX_AGENT_BOUNDARY_LNI_SOCKET="$run/node/layerxd.lni.sock" \
	LAYERX_AGENT_BOUNDARY_NODE_URL=http://127.0.0.1:9401 \
	LAYERX_AGENT_BOUNDARY_NODE_BEARER_TOKEN_FILE="$keys/tokens/program-token" \
	LAYERX_AGENT_BOUNDARY_STATE_DIR="$layerx/agent-boundary" \
	/bin/sh -ec 'LAYERX_AGENT_BOUNDARY_NETWORK_ID="$LAYERX_NODE_NETWORK_NAME" exec /usr/local/bin/layerx-agent-boundary'

# The human graph of the pod. human_material_generate runs
# platform/hosted/human/material.py once, as the pod's provisioning did, with
# the network and chain ids above and https://paxportwallet.com as the web
# origin, so the passkey relying party id is paxportwallet.com. Its policy is
# the one material.py --assemble writes from the deploy's evidence, placed at
# $keys/human-policy/policy.json beside its bundle-manifest.json and journal/;
# material.py --verify-bundle validates every declared producer output before
# use; the receipt authority replica id is the genesis replica id. The output
# is kept under $human_state/material and never regenerated: a restart whose
# bundle binding or genesis differs from the retained one is refused as a
# reconciliation requirement.
human_policy=$keys/human-policy/policy.json
human_out=$human_state/material/human

human_policy_bundle_install() {
	python3 /usr/local/lib/layerx-human/material.py --relocate-bundle "${human_policy%/*}" "$1" \
		"$LAYERX_NODE_NETWORK_ID" "$LAYERX_NODE_PAXEER_CHAIN_ID"
}

# human_authority_publish: the producer of the receipt authority's Human role
# material. The policy graph orders the kernel identity generation that
# trust_history publishes under $kernel_registry_material and the assembled
# owner bundle before it; until both are valid the graph status keeps the
# affected roles waiting, and any inconsistent input is a logged refusal. It
# then publishes one typed authority generation under
# $human_state/authority-graph, resumed unchanged on restart, and places
# principal-policy.json, registry.json and authority.json in
# $keys/human-authority, where human_authority_ready consumes them.
human_authority_publish() {
	local status bundle=${human_policy%/*}
	while :; do
		if ! identity_generation gate; then
			sleep 5
			continue
		fi
		status=$(python3 /usr/local/lib/layerx-human/material.py --policy-graph-status "$bundle/inputs" "$bundle/journal" \
			"$bundle/inputs/deployment.json" "$bundle/inputs/module-registry.json" "$bundle" "$kernel_registry_material" \
			"$human_state/authority-graph" "$LAYERX_NODE_NETWORK_ID" "$LAYERX_NODE_PAXEER_CHAIN_ID") || return 1
		if printf '%s' "$status" | jq -e 'any(.nodes[]; .state == "refused")' >/dev/null; then
			log "human policy graph refused: $(printf '%s' "$status" | jq -c '[.nodes | to_entries[] | select(.value.state == "refused") | {(.key): .value.reason}]')"
			return 1
		fi
		if printf '%s' "$status" | jq -e '.nodes["assembled-policy"].state == "ready" and .nodes["registry-material"].state == "ready"' >/dev/null; then
			break
		fi
		sleep 5
	done
	python3 /usr/local/lib/layerx-human/material.py --publish-authority-material "$bundle" "$kernel_registry_material" \
		"$human_state/authority-graph" "$LAYERX_NODE_NETWORK_ID" "$LAYERX_NODE_PAXEER_CHAIN_ID" "$keys/human-authority" >/dev/null || {
		log "human role authority publication refused"
		return 1
	}
	log "human role authority generation published"
}

if [ "$kernel_profile" = full ]; then
	human_authority_publish &
fi

human_genesis_project() {
	{ flock 8 && python3 - "$genesis" "$human_state/genesis-binding" <<'PY_GENESIS_PROJECT'
import ctypes
import os
import shutil
import stat
import sys
import tempfile

source, destination = sys.argv[1:]
names = ('metadata.lxgb', 'asset-id', 'replica-id')
fds = []
pending = None

def identity(info):
    return (info.st_dev, info.st_ino, info.st_uid, info.st_gid, info.st_mode,
            info.st_nlink, info.st_size, info.st_mtime_ns, info.st_ctime_ns)

def directory(path, uid, gid, mode):
    fd = os.open(path, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
    info = os.fstat(fd)
    if (os.path.realpath(path) != path
            or (info.st_uid, info.st_gid, stat.S_IMODE(info.st_mode)) != (uid, gid, mode)):
        os.close(fd)
        raise ValueError('protected directory ownership or mode')
    return fd

try:
    if os.geteuid() != 0:
        raise ValueError('root projection required')
    source_fd = directory(source, 0, 4020, 0o750)
    fds.append(source_fd)
    source_directory = identity(os.fstat(source_fd))
    parent_fd = directory(os.path.dirname(destination), 0, 4020, 0o750)
    fds.append(parent_fd)
    data, snapshots, opened = {}, {}, {}
    for name in names:
        fd = os.open(name, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK, dir_fd=source_fd)
        fds.append(fd)
        info = os.fstat(fd)
        producer = ((info.st_uid, info.st_gid, stat.S_IMODE(info.st_mode)) == (4020, 4020, 0o600)
                    if name == 'metadata.lxgb' else
                    info.st_uid == 0 and info.st_gid in (0, 4020) and stat.S_IMODE(info.st_mode) == 0o444)
        if (not producer or not stat.S_ISREG(info.st_mode) or info.st_nlink != 1
                or not 0 < info.st_size <= 1048576):
            raise ValueError('protected source ownership, type or bounds: ' + name)
        with os.fdopen(os.dup(fd), 'rb') as handle:
            value = handle.read(1048577)
        if len(value) != info.st_size or identity(os.fstat(fd)) != identity(info):
            raise ValueError('source changed during projection: ' + name)
        if identity(os.stat(name, dir_fd=source_fd, follow_symlinks=False)) != identity(info):
            raise ValueError('source path changed during projection: ' + name)
        data[name], snapshots[name], opened[name] = value, identity(info), fd

    def source_unchanged():
        if identity(os.fstat(source_fd)) != source_directory:
            raise ValueError('source directory changed during projection')
        for name in names:
            if (identity(os.fstat(opened[name])) != snapshots[name]
                    or identity(os.stat(name, dir_fd=source_fd, follow_symlinks=False)) != snapshots[name]):
                raise ValueError('source changed during projection: ' + name)

    if os.path.lexists(destination):
        retained_fd = directory(destination, 0, 0, 0o700)
        fds.append(retained_fd)
        if set(os.listdir(retained_fd)) != set(names):
            raise ValueError('retained projection inventory differs')
        for name in names:
            fd = os.open(name, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK, dir_fd=retained_fd)
            fds.append(fd)
            info = os.fstat(fd)
            if (not stat.S_ISREG(info.st_mode) or info.st_nlink != 1
                    or (info.st_uid, info.st_gid, stat.S_IMODE(info.st_mode)) != (0, 0, 0o600)
                    or info.st_size != len(data[name])):
                raise ValueError('retained projection protected file differs: ' + name)
            with os.fdopen(os.dup(fd), 'rb') as handle:
                value = handle.read(1048577)
            if (value != data[name] or identity(os.fstat(fd)) != identity(info)
                    or identity(os.stat(name, dir_fd=retained_fd, follow_symlinks=False)) != identity(info)):
                raise ValueError('retained projection bytes differ: ' + name)
        source_unchanged()
    else:
        pending = tempfile.mkdtemp(prefix='.genesis-binding-', dir=os.path.dirname(destination))
        os.chown(pending, 0, 0)
        os.chmod(pending, 0o700)
        for name in names:
            fd = os.open(pending + '/' + name, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
            with os.fdopen(fd, 'wb') as handle:
                os.fchown(handle.fileno(), 0, 0)
                os.fchmod(handle.fileno(), 0o600)
                handle.write(data[name])
                handle.flush()
                os.fsync(handle.fileno())
        source_unchanged()
        pending_fd = directory(pending, 0, 0, 0o700)
        try:
            os.fsync(pending_fd)
        finally:
            os.close(pending_fd)
        rename = ctypes.CDLL(None, use_errno=True).renameat2
        rename.argtypes = (ctypes.c_int, ctypes.c_char_p, ctypes.c_int, ctypes.c_char_p, ctypes.c_uint)
        rename.restype = ctypes.c_int
        if rename(-100, os.fsencode(pending), -100, os.fsencode(destination), 1) != 0:
            code = ctypes.get_errno()
            raise OSError(code, os.strerror(code))
        pending = None
        os.fsync(parent_fd)
except (OSError, ValueError, AttributeError) as error:
    raise SystemExit('human genesis projection refused: ' + str(error))
finally:
    for fd in reversed(fds):
        os.close(fd)
    if pending is not None:
        shutil.rmtree(pending)
PY_GENESIS_PROJECT
	} 8>"$human_state/genesis-binding.lock"
}

human_material_generate() {
	local work=$human_state/material.new check=$human_state/material.check d
	human_genesis_project || return 1
	if [ -d "$human_state/material" ]; then
		python3 /usr/local/lib/layerx-human/material.py --verify-material "$human_state/material" || return 1
		rm -rf "$check"
		install -d -o 0 -g 0 -m 0700 "$check"
		human_policy_bundle_install "$check/policy-bundle" >"$check/bundle-binding" || return 1
		python3 /usr/local/lib/layerx-human/material.py --genesis-binding "$human_state/genesis-binding" >"$check/genesis-binding" || return 1
		if ! cmp -s "$check/bundle-binding" "$human_state/material/bundle-binding" ||
			! cmp -s "$check/genesis-binding" "$human_state/material/genesis-binding"; then
			echo 'human owner bundle: reconciliation required' >&2
			return 1
		fi
		rm -rf "$check"
		return 0
	fi
	if [ -e "$work" ] || [ -L "$work" ]; then
		echo 'human owner bundle: interrupted material requires reconciliation' >&2
		return 1
	fi
	install -d -o 0 -g 0 -m 0700 "$work" "$work/human"
	for d in components kms config agent-config movement-config authority-config authority identity; do
		install -d -o 0 -g 0 -m 0700 "$work/human/$d"
	done
	install -o 0 -g 0 -m 0600 "$human_state/genesis-binding/replica-id" "$work/receipt-authority-replica-id"
	python3 /usr/local/lib/layerx-human/material.py --genesis-binding "$human_state/genesis-binding" >"$work/genesis-binding" || return 1
	human_policy_bundle_install "$work/policy-bundle" >"$work/bundle-binding" || return 1
	python3 /usr/local/lib/layerx-human/material.py "$work/human" "$LAYERX_NODE_NETWORK_ID" \
		"$LAYERX_NODE_PAXEER_CHAIN_ID" "$work/policy-bundle/policy.json" https://paxportwallet.com || return 1
	python3 /usr/local/lib/layerx-human/material.py --seal-material "$work" || return 1
	mv "$work" "$human_state/material"
	sync "$human_state"
}

# human_project <service> <uid> <source:name>...: the role's material
# directory, readable by its uid only, with each source under its name; a
# source directory becomes env/, one file per variable, which the role's
# command exports before human-entrypoint.
human_project() {
	local dir=$human_material/$1 uid=$2 pair source name
	shift 2
	identity_generation gate || return 1
	{ flock 9 && human_material_generate; } 9>"$human_state/material.lock" || return 1
	install -d -o "$uid" -g 4020 -m 0500 "$dir"
	for pair in "$@"; do
		source=${pair%:*}
		name=${pair##*:}
		if [ -d "$source" ]; then
			install -d -o "$uid" -g 4020 -m 0500 "$dir/$name"
			find "$source" -maxdepth 1 -type f -exec install -o 0 -g 4020 -m 0440 -t "$dir/$name" {} + || return 1
		else
			install -o 0 -g 4020 -m 0440 "$source" "$dir/$name" || return 1
		fi
	done
}

# shellcheck disable=SC2016 # the variables expand in the role's shell
human_env='for f in /run/human-material/env/*; do [ ! -f "$f" ] || export "${f##*/}=$(cat "$f")"; done; exec "$@"'

# The two Paxeer boundaries of start_paxeer, as the pod's paxeer-boundary and
# paxeer-observer-boundary services; both certificates carry localhost and
# 127.0.0.1 under the internal CA.
human_paxeer_urls='["https://localhost:9447","https://127.0.0.1:9448"]'
human_paxeer_ca=$tls/paxeer-boundary-loopback/ca.der

# The five attestor nodes, node id N being the Nth host:port of
# LAYERX_HUMAN_ATTESTOR_NODES in the box env file. The components loader takes
# id=socket-address pairs, so the prepare resolves each host to its first
# address of either family and the role reads the table from its material.
attestor_nodes_input=${LAYERX_HUMAN_ATTESTOR_NODES:-}
unset LAYERX_HUMAN_ATTESTOR_NODES
human_attestor_nodes() {
	local n=0 entry host port address nodes= entries
	IFS=, read -r -a entries <<<"$attestor_nodes_input"
	[ "${#entries[@]}" -eq 5 ] || {
		log "LAYERX_HUMAN_ATTESTOR_NODES must hold five host:port entries; the human components wait for it"
		return 1
	}
	for entry in "${entries[@]}"; do
		n=$((n + 1))
		host=${entry%:*}
		host=${host#[}
		host=${host%]}
		port=${entry##*:}
		[[ -n "$host" && "$port" =~ ^[1-9][0-9]{0,4}$ ]] || {
			log "LAYERX_HUMAN_ATTESTOR_NODES entry $n is not host:port"
			return 1
		}
		address="$(getent ahosts "$host" | awk 'NR == 1 { print $1 }')"
		[ -n "$address" ] || {
			log "attestor $n host $host does not resolve; the human components wait for it"
			return 1
		}
		case "$address" in
		*:*) nodes="$nodes${nodes:+,}$n=[$address]:$port" ;;
		*) nodes="$nodes${nodes:+,}$n=$address:$port" ;;
		esac
	done
	printf '%s' "$nodes" >"$human_material/attestor-nodes.new"
	mv "$human_material/attestor-nodes.new" "$human_material/attestor-nodes"
}

# The settlement fee bounds of the components have no generator in the tree;
# the box env file carries them.
human_evm_bounds() {
	local name
	for name in LAYERX_HUMAN_EVM_GAS_LIMIT LAYERX_HUMAN_EVM_MAX_FEE_PER_GAS LAYERX_HUMAN_EVM_MAX_PRIORITY_FEE_PER_GAS; do
		[ -n "${!name:-}" ] || {
			log "$name is unset; the human components wait for it"
			return 1
		}
	done
}

human_components_prepare() {
	human_evm_bounds && human_attestor_nodes && tls_for human-event-client 4020 &&
		human_project human-components 4020 "$human_out/config:env" "$human_out/components/purpose-catalog.json:purpose-catalog.json" \
			"$human_paxeer_ca:ca.der" "$tls/human-attestor-client/ca.der:attestor-ca.der" "$tls/human-attestor-client/cert.der:attestor-client.der" \
			"$tls/human-attestor-client/key.der:attestor-client-key.der" &&
		install -o 0 -g 4020 -m 0440 "$human_material/attestor-nodes" "$human_material/human-components/env/LAYERX_HUMAN_ATTESTOR_NODES"
}

human_identity_prepare() {
    local canonical_tenant
    human_project human-identity 4020 "$human_out/identity/recovery-policy.json:recovery-policy.json" || return 1
    canonical_tenant=$(cat "$human_out/authority-config/tenant") || return 1
    if [ "${LAYERX_HUMAN_IDENTITY_PROVIDER_BINDING_TENANT+x}" = x ] && \
            [ "$LAYERX_HUMAN_IDENTITY_PROVIDER_BINDING_TENANT" != "$canonical_tenant" ]; then
        log "identity provider tenant differs from admitted Human material"
        return 1
    fi
    install -d -o 4020 -g 4020 -m 0500 "$human_material/human-identity/env" &&
        install -o 0 -g 4020 -m 0440 "$human_out/authority-config/tenant" \
            "$human_material/human-identity/env/LAYERX_HUMAN_IDENTITY_PROVIDER_BINDING_TENANT"
}

# trust_history: the sequencer trust history the security provider and the
# registry read, in the LayerX/sequencer-trust-history/v1 encoding of
# encode_trust_history in platform/hosted/tests/beta-cluster.sh: one current
# protocol 3, epoch 1 entry of the network id, the sequencer id layerxd
# publishes in core.env and the public key receipt_authority_prepare derives
# from the sequencer seed. Written once; an existing history is never rewritten.
trust_history() {
	while missing "$genesis/metadata.lxgb" "$genesis/replica-id" "$genesis/asset-id" "$keys/sequencer.key" >/dev/null; do
		sleep 5
	done
	kernel_registry_identity
}

human_security_waits() {
	printf '%s' "$genesis_files $run/node/core.env $run/node/sequencer-public-key $human_policy ${human_policy%/*}/bundle-manifest.json ${human_policy%/*}/inputs ${human_policy%/*}/journal $human_state/trust-history"
}

human_security_prerequisite() {
	local absent waits
	waits="$(human_security_waits)"
	waits=${waits% "$human_state/trust-history"}
	# shellcheck disable=SC2086
	if absent="$(missing $waits)"; then
		printf '%s' "$absent"
		return 0
	fi
	if ! identity_generation gate 2>/dev/null; then
		printf '%s' 'identity generation refused'
		return 0
	fi
	if ! human_genesis_project; then
		printf '%s' 'genesis protected projection refused'
		return 0
	fi
	if absent="$(python3 - "$human_state/genesis-binding" "${human_policy%/*}" "$human_state/trust-history" "$LAYERX_NODE_NETWORK_ID" "$LAYERX_NODE_PAXEER_CHAIN_ID" <<'PY_SECURITY_PREREQUISITE'
import os
import stat
import sys

sys.path.insert(0, '/usr/local/lib/layerx-human')
try:
    from material import genesis_binding, verify_bundle
except (ImportError, OSError):
    print('owner-policy material validator unavailable', end='')
    raise SystemExit(0)
for label, validate in (
        ('genesis', lambda: genesis_binding(sys.argv[1])),
        ('owner-policy', lambda: verify_bundle(sys.argv[2], int(sys.argv[4]), int(sys.argv[5])))):
    try:
        validate()
    except (OSError, ValueError, KeyError, TypeError) as error:
        print(label + ' ' + str(error), end='')
        raise SystemExit(0)
try:
    path = sys.argv[3]
    fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
    with os.fdopen(fd, 'rb') as source:
        info = os.fstat(source.fileno())
        if (os.path.realpath(path) != path or not stat.S_ISREG(info.st_mode)
                or (info.st_uid, info.st_gid, stat.S_IMODE(info.st_mode)) != (0, 4020, 0o440)
                or info.st_nlink != 1 or not 0 < info.st_size <= 1048576):
            raise ValueError('ownership, type or bounds')
except FileNotFoundError:
    print(path, end='')
    raise SystemExit(0)
except (OSError, ValueError):
    print('trust-history protected material refused', end='')
    raise SystemExit(0)
PY_SECURITY_PREREQUISITE
	)"; then
		[ -n "$absent" ] || return 1
		printf '%s' "$absent"
	else
		printf '%s' 'security prerequisite validator failed'
	fi
}

human_security_prepare() {
	human_project human-security 4020 "$human_state/trust-history:trust-history"
}

# The one Human KMS execution service movement reaches: the listener and
# provider reference the KMS launch below serves, and the server name its
# human-kms certificate carries.
human_kms_listen=127.0.0.1:9450
human_kms_server_name=layerx-human-kms
human_kms_provider=layerx-human-kms

# human_movement_state: the movement journal root under the role's retained
# state, created owner-only once and refused when anything else holds it.
human_movement_state() {
	local root=$human_state/movement/movement
	if [ -e "$root" ] || [ -L "$root" ]; then
		[ ! -L "$root" ] && [ -d "$root" ] && [ "$(stat -c '%u:%g:%a' "$root")" = 4020:4020:700 ]
	else
		install -d -o 4020 -g 4020 -m 0700 "$root"
	fi
}

# human_movement_kms_binding: movement's executor configuration names exactly
# the KMS service above, and the identity it presents is the restricted
# executor the KMS pins; anything else keeps movement waiting.
human_movement_kms_binding() {
	local config=$human_out/movement-config/LAYERX_HUMAN_MOVEMENT_PROVIDER_ pair
	for pair in "KMS_ENDPOINT=$human_kms_listen" "KMS_SERVER_NAME=$human_kms_server_name" \
		"KMS_PROVIDER_REFERENCE=$human_kms_provider" \
		"KMS_CA_DER=/run/human-private/movement/ca.der" \
		"KMS_CLIENT_CERT_DER=/run/human-private/movement/kms-executor.der" \
		"KMS_CLIENT_KEY_DER=/run/human-private/movement/kms-executor-key.der"; do
		[ -f "$config${pair%%=*}" ] && [ ! -L "$config${pair%%=*}" ] &&
			[ "$(cat "$config${pair%%=*}")" = "${pair#*=}" ] || return 1
	done
	cmp -s "$tls/human-kms-executor/cert.der" "$human_kms_out/kms-executor.der" &&
		cmp -s "$tls/human-kms-executor/ca.der" "$human_kms_out/ca.der" &&
		! cmp -s "$tls/human-kms-executor/cert.der" "$human_kms_out/kms-client.der"
}

human_movement_prepare() {
	cmp -s "$human_paxeer_ca" "$tls/human-kms/ca.der" || return 1
	human_movement_state || return 1
	human_project human-movement 4020 "$human_out/movement-config:env" "$human_paxeer_ca:ca.der" \
		"$tls/human-kms-executor/cert.der:kms-executor.der" "$tls/human-kms-executor/key.der:kms-executor-key.der" \
		"$human_out/movement/custody.profile:custody.profile" || return 1
	human_movement_kms_binding
}

human_kms_source=${LAYERX_HUMAN_KMS_REGISTRY_SOURCE:-$keys/human-kms/module-registry.json}
human_kms_out=$human_state/kms-prerequisite/material
human_kms_prepare() {
	local directory=$human_material/human-kms name
	identity_generation gate || return 1
	if [ -e "$human_state/kms-prerequisite" ] || [ -L "$human_state/kms-prerequisite" ]; then
		[ ! -L "$human_state/kms-prerequisite" ] && \
			[ "$(stat -c '%u:%g:%a' "$human_state/kms-prerequisite")" = 0:0:700 ] || return 1
	else
		mkdir -m 0700 "$human_state/kms-prerequisite" || return 1
	fi
	[ ! -L "$human_state/kms-prerequisite.lock" ] || return 1
	{ flock 7 && python3 /usr/local/lib/layerx-human/material.py --kms-prerequisite \
		"$human_kms_source" "$tls" "$human_kms_out" "$LAYERX_NODE_NETWORK_ID" \
		"$(cat "$genesis/asset-id")" "$human_state/kms"; } 7>"$human_state/kms-prerequisite.lock" || return 1
	install -d -o 4026 -g 4020 -m 0500 "$directory"
	for name in kms-server.der kms-server-key.der kms-client.der kms-executor.der ca.der kms-seal registry.json; do
		install -o 0 -g 4020 -m 0440 "$human_kms_out/$name" "$directory/$name" || return 1
	done
}

# The owner's session operator secret, made once on the volume like the
# other bearers.
if [ "$kernel_profile" = full ]; then
    fresh "$keys/human-authority/session-operator" 0:0 0600 openssl rand -hex 32
fi

human_owner_prepare() {
	tls_for agentd-rpc 4021 || return 1
	install -d -o 4021 -g 4020 -m 0700 "$human_state/agent/rpc-idempotency" || return 1
	human_project human-owner 4021 "$human_out/agent-config:env" "$tls/receipt-authority/ca.der:ca.der" \
		"$keys/human-authority/session-operator:session-operator" "$keys/human-authority/authority-token:authority-token" \
		"$keys/tokens/program-token:program-token" "$human_state/trust-history:trust-history" "$human_out/journal:journal"
}

human_tls_prepare() {
	tls_for human 4020
}

if [ "$kernel_profile" = full ]; then
human_root=$human_state/kms service human-kms 4026 \
	"$genesis/asset-id $human_kms_source $tls/human-kms/cert.der $tls/human-kms/key.der $tls/human-kms/ca.der $tls/human-kms-client/cert.der $tls/human-kms-client/ca.der $tls/human-kms-executor/cert.der $tls/human-kms-executor/ca.der" \
	human_kms_prepare - -- env \
	LAYERX_HUMAN_KMS_LISTEN="$human_kms_listen" \
	LAYERX_HUMAN_KMS_PROVIDER_REFERENCE="$human_kms_provider" \
	LAYERX_HUMAN_KMS_STATE_DIR=/var/lib/layerx/human \
	LAYERX_HUMAN_KMS_DEADLINE_SECONDS=5 \
	LAYERX_HUMAN_KMS_REGISTRY_FILE=/run/human-private/kms/registry.json \
	LAYERX_HUMAN_KMS_CLIENT_CA_DER=/run/human-private/kms/ca.der \
	LAYERX_HUMAN_KMS_TLS_CERT_DER=/run/human-private/kms/kms-server.der \
	LAYERX_HUMAN_KMS_TLS_KEY_DER=/run/human-private/kms/kms-server-key.der \
	LAYERX_HUMAN_KMS_CLIENT_CERT_DER=/run/human-private/kms/kms-client.der \
	LAYERX_HUMAN_KMS_EVM_CLIENT_CERT_DER=/run/human-private/kms/kms-executor.der \
	LAYERX_HUMAN_KMS_SEAL_SECRET_FILE=/run/human-private/kms/kms-seal \
	/usr/local/bin/human-entrypoint kms

human_root=$human_state/components service human-components 4020 \
	"$genesis_files $human_policy $human_paxeer_ca $tls/human-event-client/identity.p12 $tls/human-attestor-client/ca.der $tls/human-attestor-client/cert.der $tls/human-attestor-client/key.der /run/secrets/events-journey-token /run/secrets/events-approval-token /run/secrets/events-webhooks-token" \
	human_components_prepare - -- \
	/bin/sh -ec "$human_env" sh env \
	LAYERX_HUMAN_PROTOCOL_VERSION=3 \
	LAYERX_HUMAN_ATTESTOR_SIGNERS=1,2,3,4,5 \
	LAYERX_HUMAN_ATTESTOR_ROOT_CERTIFICATE_DER=/run/human-private/components/attestor-ca.der \
	LAYERX_HUMAN_ATTESTOR_CLIENT_CERTIFICATE_DER=/run/human-private/components/attestor-client.der \
	LAYERX_HUMAN_ATTESTOR_CLIENT_PRIVATE_KEY_DER=/run/human-private/components/attestor-client-key.der \
	LAYERX_HUMAN_ATTESTOR_DEADLINE_SECONDS=10 \
	LAYERX_HUMAN_PAXEER_RPC_URL=https://localhost:9447 \
	LAYERX_HUMAN_PAXEER_RPC_URLS="$human_paxeer_urls" \
	LAYERX_HUMAN_PAXEER_MINIMUM_AGREEMENT=2 \
	LAYERX_HUMAN_COMPONENT_ALLOWED_UID=4020 \
	LAYERX_HUMAN_RECIPIENT_SOCKET="$run/human/recipient.sock" \
	LAYERX_HUMAN_RECIPIENT_CALLER_UID=4021 \
	LAYERX_HUMAN_RECIPIENT_CALLER_GID=4020 \
	LAYERX_HUMAN_RECIPIENT_DEADLINE_SECONDS=10 \
	LAYERX_HUMAN_COMPONENT_WORKERS=4 \
	LAYERX_HUMAN_COMPONENT_QUEUE_CAPACITY=4 \
	LAYERX_HUMAN_MAINTENANCE_INTERVAL_SECONDS=10 \
	LAYERX_HUMAN_MAINTENANCE_MAXIMUM_ITEMS=100 \
	LAYERX_HUMAN_STORE_ROOT=/var/lib/layerx/human/store \
	LAYERX_HUMAN_CUSTODY_ROOT=/var/lib/layerx/human/custody \
	LAYERX_HUMAN_AUTH_INDEX_ROOT=/var/lib/layerx/human/auth-index \
	/usr/local/bin/human-entrypoint components

human_root=$human_state/identity service human-identity 4020 "$genesis_files $human_policy" human_identity_prepare - -- \
	/bin/sh -ec "$human_env" sh env \
	LAYERX_HUMAN_IDENTITY_PROVIDER_SOCKET="$run/human/identity.sock" \
	LAYERX_HUMAN_IDENTITY_PROVIDER_BINDING_SOCKET="$run/human/identity-binding.sock" \
	LAYERX_HUMAN_IDENTITY_PROVIDER_BINDING_ALLOWED_UIDS=4020,4021 \
	LAYERX_HUMAN_IDENTITY_PROVIDER_STATE_ROOT=/var/lib/layerx/human/identity \
	LAYERX_HUMAN_IDENTITY_PROVIDER_ALLOWED_UID=4020 \
	LAYERX_HUMAN_IDENTITY_PROVIDER_RECOVERY_POLICY_FILE=/run/human-private/identity/recovery-policy.json \
	LAYERX_HUMAN_IDENTITY_PROVIDER_DEADLINE_SECONDS=5 \
	/usr/local/bin/human-entrypoint identity

human_root=$human_state/security service human-security 4020 "$(human_security_waits)" human_security_prepare - -- \
	env \
	LAYERX_HUMAN_SECURITY_PROVIDER_SOCKET="$run/human/security.sock" \
	LAYERX_HUMAN_SECURITY_PROVIDER_STATE_ROOT=/var/lib/layerx/human/security \
	LAYERX_HUMAN_SECURITY_PROVIDER_ALLOWED_UID=4020 \
	LAYERX_HUMAN_SECURITY_PROVIDER_TRUST_HISTORY=/run/human-private/security/trust-history \
	LAYERX_HUMAN_SECURITY_PROVIDER_DEADLINE_SECONDS=5 \
	/usr/local/bin/human-entrypoint security

human_root=$human_state/movement service human-movement 4020 \
	"$genesis_files $human_policy $human_paxeer_ca $tls/human-kms/cert.der $tls/human-kms-executor/cert.der $tls/human-kms-executor/key.der $tls/human-kms-executor/ca.der $human_kms_out/kms-seal $human_kms_out/registry.json $human_kms_out/kms-executor.der $human_kms_out/kms-client.der" \
	human_movement_prepare - -- \
	/bin/sh -ec "$human_env" sh env \
	LAYERX_HUMAN_MOVEMENT_PROVIDER_SOCKET="$run/human/movement.sock" \
	LAYERX_HUMAN_MOVEMENT_PROVIDER_STATE_ROOT=/var/lib/layerx/human/movement \
	LAYERX_HUMAN_MOVEMENT_PROVIDER_ALLOWED_UID=4020 \
	LAYERX_HUMAN_MOVEMENT_PROVIDER_PAXEER_RPC_URLS="$human_paxeer_urls" \
	LAYERX_HUMAN_MOVEMENT_PROVIDER_PAXEER_MINIMUM_AGREEMENT=2 \
	/usr/local/bin/human-entrypoint movement

human_root=$human_state/agent service human-owner 4021 \
	"$genesis_files $human_policy $tls/receipt-authority/ca.der $keys/human-authority/authority-token $human_state/trust-history $tls/agentd-rpc/cert.pem $tls/agentd-rpc/key.pem $tls/agentd-rpc/ca.pem" \
	human_owner_prepare - -- \
	/bin/sh -ec "$human_env" sh env \
	LAYERX_AGENT_HUMAN_AUTHORITY_ENDPOINT=https://localhost:9445 \
	LAYERX_AGENT_AUTHORITY_ENDPOINT=https://localhost:9445 \
	LAYERX_NODE_NETWORK_NAME="$LAYERX_NODE_NETWORK_NAME" \
	LAYERX_AGENTD_RPC_LISTEN="$agentd_rpc_listen" \
	LAYERX_AGENTD_RPC_TLS_CERT="$tls/agentd-rpc/cert.pem" \
	LAYERX_AGENTD_RPC_TLS_KEY="$tls/agentd-rpc/key.pem" \
	LAYERX_AGENTD_RPC_TLS_CLIENT_CA="$tls/agentd-rpc/ca.pem" \
	LAYERX_AGENTD_RPC_PEER="$LAYERX_KERNEL_AGENTD_RPC_PEER" \
	LAYERX_AGENTD_RPC_IDEMPOTENCY_ROOT=/var/lib/layerx/human/rpc-idempotency \
	LAYERX_AGENTD_RPC_IDEMPOTENCY_DAEMON_SEQUENCES="$LAYERX_KERNEL_AGENTD_RPC_DAEMON_SEQUENCES" \
	LAYERX_AGENTD_RPC_IDEMPOTENCY_PROTOCOL_SEQUENCES="$LAYERX_KERNEL_AGENTD_RPC_PROTOCOL_SEQUENCES" \
	/usr/local/bin/human-entrypoint agent

# The two human service processes on the one components socket: the plain
# listener of the app's http_service on [::]:8080 from the app env, and the
# TLS listener under the internal CA on [::]:9449 that the journeys and
# approvals event sources of the internal app reach over the private network.
service human 4020 "" - - -- /usr/local/bin/human-entrypoint service

service human-tls 4020 "$tls/human/cert.der $tls/human/key.der" human_tls_prepare - -- \
	env \
	LAYERX_HUMAN_LISTENER=tls \
	"LAYERX_HUMAN_BIND=[::]:9449" \
	LAYERX_HUMAN_TLS_CERT_DER="$tls/human/cert.der" \
	LAYERX_HUMAN_TLS_KEY_DER="$tls/human/key.der" \
	/usr/local/bin/human-entrypoint service

fi

# The mirror-signer and mirror-publisher containers: the signer serves both
# publisher keys on its socket, and the publisher reads the LNI socket and
# answers /readyz and /status on 127.0.0.1:9456, the status_listen the
# rendered config names.
service mirror-signer 4021 "$genesis_files $mirror_material/ethereum.key" - - -- \
	env \
	LAYERX_MIRROR_SIGNER_SOCKET=/run/mirror-signer/signer.sock \
	LAYERX_MIRROR_SIGNER_ETHEREUM_KEY_FILE="$mirror_material/ethereum.key" \
	LAYERX_MIRROR_SIGNER_SOLANA_KEY_FILE="$mirror_material/solana.json" \
	/usr/local/bin/layerx-mirror-signer

service mirror-publisher 4021 \
	"$genesis_files $run/node/layerxd.lni.sock $mirror_run/config.json /run/mirror-signer/signer.sock" - - -- \
	/usr/local/bin/layerx-mirror-publisher "$mirror_run/config.json"

# The kernel perps oracle feeder, full profile only: it signs its activities
# with the oracle key and submits them on the LNI socket, so the prepare
# renders its feeder.json with lni_socket pinned to layerxd's socket.
if [ "$kernel_profile" = full ]; then
	feeder_config=${LAYERX_FEEDER_CONFIG:-$layerx/oracle-feeder/feeder.json}
	feeder_key=${LAYERX_FEEDER_ORACLE_KEY_FILE:-$keys/oracle-feeder/oracle.key}
	feeder_readyz=${LAYERX_FEEDER_READYZ_ADDR:-127.0.0.1:9458}
	unset LAYERX_FEEDER_CONFIG LAYERX_FEEDER_ORACLE_KEY_FILE LAYERX_FEEDER_READYZ_ADDR
	install -d -o 0 -g 4020 -m 0750 "$layerx/oracle-feeder"
	install -d -o 4021 -g 4020 -m 0700 "$layerx/oracle-feeder/state"
	install -d -o 0 -g 0 -m 0700 "$keys/oracle-feeder"

	oracle_feeder_prepare() {
		install -d -o 4021 -g 4020 -m 0700 "$run/oracle-feeder" || return 1
		python3 - "$feeder_config" "$run/node/layerxd.lni.sock" "$run/oracle-feeder/feeder.json" <<'PY_FEEDER' || return 1
import json
import os
import sys

source, socket, target = sys.argv[1:]
with open(source, encoding="utf-8") as handle:
    config = json.load(handle)
if not isinstance(config, dict):
    raise SystemExit("feeder config is not a JSON object")
config["lni_socket"] = socket
with open(target + ".new", "w", encoding="utf-8") as handle:
    json.dump(config, handle)
os.chown(target + ".new", 4021, 4020)
os.chmod(target + ".new", 0o400)
os.replace(target + ".new", target)
PY_FEEDER
		install -o 4021 -g 4020 -m 0400 "$feeder_key" "$run/oracle-feeder/oracle.key"
	}

	service oracle-feeder 4021 "$genesis_files $run/node/layerxd.lni.sock $feeder_config $feeder_key" \
		oracle_feeder_prepare - -- \
		env \
		LAYERX_FEEDER_CONFIG="$run/oracle-feeder/feeder.json" \
		LAYERX_FEEDER_ORACLE_KEY_FILE="$run/oracle-feeder/oracle.key" \
		LAYERX_FEEDER_STATE_DIR="$layerx/oracle-feeder/state" \
		LAYERX_FEEDER_READYZ_ADDR="$feeder_readyz" \
		/usr/local/bin/layerx-oracle-feeder
fi

relay_archive_prepare() {
    local config_dir sequencer_id sequencer_public genesis_digest
    openssl verify -CAfile "$tls/relay-archive/ca.pem" -purpose sslserver "$tls/relay-archive/cert.pem" >/dev/null || return 1
    tls_for relay-archive 4020 || return 1
    install -d -o 4020 -g 4020 -m 0700 "$layerx/relay-archive" "$run/relay-archive" || return 1
    sequencer_id=$(sed -n 's/^LAYERX_CORE_SEQUENCER_ID=//p' "$run/node/core.env")
    sequencer_public=$(tr -d '\r\n' <"$run/node/sequencer-public-key")
    genesis_digest=$(sha256sum "$node_data/genesis/genesis.manifest" | cut -d' ' -f1)
    config_dir=$(mktemp -d "$run/relay-archive/config.XXXXXX") || return 1
    if ! /opt/layerx/relay_archive/install.sh --config-only \
        --config "$config_dir/origin.json" --codec /usr/local/bin/layerx-archive-codec \
        --network-id "$LAYERX_NODE_NETWORK_ID" --genesis-sha256 "$genesis_digest" \
        --sequencer-id "$sequencer_id" --sequencer-public-key "$sequencer_public" \
        --data-dir "$layerx/relay-archive" --listen '[::]:9457' \
        --public-url https://api-mainnet-beta.paxeer.network \
        --genesis-manifest "$node_data/genesis/genesis.manifest" \
        --genesis-snapshot "$node_data/genesis/00000000000000000000.lxs" \
        --source-log "$node_data/checkpoints/da-bodies.log" \
        --submission-upstream https://api-mainnet-beta.paxeer.network/v1/activities \
        --ca-file /etc/ssl/certs/ca-certificates.crt \
        --tls-cert "$tls/relay-archive/cert.pem" --tls-key "$tls/relay-archive/key.pem"; then
        rm -f "$config_dir/origin.json"
        rmdir "$config_dir"
        return 1
    fi
    chown 4020:4020 "$config_dir/origin.json" || return 1
    chmod 0600 "$config_dir/origin.json" || return 1
    mv -f "$config_dir/origin.json" "$run/relay-archive/config.json" || return 1
    rmdir "$config_dir"
}

service relay-archive 4020 \
    "$genesis_files $node_data/genesis/genesis.manifest $node_data/genesis/00000000000000000000.lxs $node_data/checkpoints/da-bodies.log $run/node/core.env $run/node/sequencer-public-key $tls/relay-archive/cert.pem $tls/relay-archive/key.pem $tls/relay-archive/ca.pem" \
    relay_archive_prepare - -- \
    python3 /opt/layerx/relay_archive/runtime.py --config "$run/relay-archive/config.json"

if [ "$kernel_profile" = full ]; then
    human_authority_ready &
fi
trust_history &

wait
