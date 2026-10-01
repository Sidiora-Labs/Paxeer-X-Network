#!/usr/bin/env python3
import argparse
import copy
import datetime
import json
import os
from pathlib import Path
import re
import stat
import subprocess
import sys

SCHEMA = 'paxeer-x.candidate.v1'
UNKNOWN = 'unknown'
MAX_BYTES = 4 * 1024 * 1024
HEX = re.compile(r'[0-9a-f]{40}|[0-9a-f]{64}')
DIGEST = re.compile(r'sha256:[0-9a-f]{64}')
REF = re.compile(r'(?:private:/|secure://)[A-Za-z0-9_./:#@+-]+')
STATES = {'running', 'started', 'stopped', 'created', 'destroyed', 'absent', 'pending', 'unknown'}
BINDING_FIELDS = ('source_revision', 'image_digest', 'config_digest', 'config_ref',
                  'owner_ref', 'authority_ref', 'membership_ref', 'storage_ref',
                  'data_ref', 'abi_ref', 'providers_ref', 'foreign_chains_ref',
                  'funded_accounts_ref', 'roles_ref', 'certificates_ref', 'policy_ref')
FOUNDATION = {'chain_id': 125, 'nodes': 'preserved', 'validators': 'preserved',
              'rpc_records': ['api' + str(i) for i in range(1, 17)],
              'api1_original_host': 'preserved', 'wallet_authentication': 'Supabase',
              'wallet_placement': 'existing-docker-host', 'relocation': False}


class Invalid(ValueError):
    pass


def require(condition, message):
    if not condition:
        raise Invalid(message)


def keys(value, expected, label):
    require(isinstance(value, dict) and set(value) == set(expected), label + ': invalid fields')


def duplicate_free(pairs):
    result = {}
    for key, value in pairs:
        require(key not in result, 'duplicate JSON field')
        result[key] = value
    return result


def protected_path(path):
    path = Path(path)
    require(not any(part == '.env' or part.startswith('.env.') for part in path.parts),
            'environment files are forbidden')
    return path


def load_private(path):
    path = protected_path(path)
    fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
    try:
        info = os.fstat(fd)
        require(stat.S_ISREG(info.st_mode) and info.st_nlink == 1 and
                info.st_uid == os.geteuid() and info.st_mode & 0o077 == 0,
                'input must be an owned private regular file with one link')
        require(info.st_size <= MAX_BYTES, 'input exceeds size bound')
        with os.fdopen(fd, 'r', encoding='utf-8') as stream:
            fd = -1
            value = json.load(stream, object_pairs_hook=duplicate_free)
        require(isinstance(value, dict), 'input must be an object')
        return value
    finally:
        if fd >= 0:
            os.close(fd)


def write_private(path, value):
    path = protected_path(path)
    require(path.parent.is_dir(), 'output parent must exist')
    fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
    with os.fdopen(fd, 'w', encoding='utf-8') as stream:
        json.dump(value, stream, indent=2, sort_keys=True)
        stream.write('\n')
        stream.flush()
        os.fsync(stream.fileno())


def run_bounded(argv, cwd=None, timeout=20):
    require(0 < timeout <= 60, 'timeout outside 0..60 seconds')
    try:
        result = subprocess.run(argv, cwd=cwd, stdin=subprocess.DEVNULL,
                                stdout=subprocess.PIPE, stderr=subprocess.DEVNULL,
                                timeout=timeout, check=False)
    except (OSError, subprocess.TimeoutExpired):
        raise Invalid('read-only command unavailable or timed out') from None
    require(result.returncode == 0, 'read-only command failed')
    require(len(result.stdout) <= MAX_BYTES, 'command output exceeds size bound')
    return result.stdout.decode('utf-8')


def git(repo, *args):
    return run_bounded(['git', '--no-optional-locks', '-C', str(repo), *args]).strip()


def source_identity(repo, mainline):
    revision = git(repo, 'rev-parse', '--verify', 'HEAD^{commit}')
    tree = git(repo, 'rev-parse', '--verify', 'HEAD^{tree}')
    main = git(repo, 'rev-parse', '--verify', mainline + '^{commit}')
    dirty = bool(git(repo, 'status', '--porcelain=v1', '--untracked-files=normal'))
    ancestors = git(repo, 'rev-list', main).splitlines()
    return {'revision': revision, 'tree': tree, 'mainline_revision': main,
            'integrated': revision in ancestors, 'dirty': dirty,
            'release_credit': False}


def catalogue(path):
    text = Path(path).read_text(encoding='utf-8')
    rows = {}
    for block in re.split(r'(?=^\[)', text, flags=re.M):
        match = re.match(r'\[service\.([a-z0-9-]+)\]\s*\n', block)
        if match:
            fields = {}
            for line in block[match.end():].splitlines():
                found = re.fullmatch(r'(purpose|route|dependencies)\s*=\s*(".*")\s*', line)
                if found:
                    fields[found[1]] = json.loads(found[2])
            keys(fields, ('purpose', 'route', 'dependencies'), 'service declaration')
            require(match[1] not in rows, 'duplicate catalogue service')
            rows[match[1]] = fields
    require(len(rows) == 31, 'expected complete 31-service catalogue')
    return rows


def reference(path, fragment=''):
    result = 'private:' + str(Path(path).resolve()) + fragment
    require(REF.fullmatch(result), 'invalid private reference')
    return result


def unknown_bindings():
    return {field: UNKNOWN for field in BINDING_FIELDS}


def observation(identity, state, image, kind, evidence, exposure=UNKNOWN, storage=UNKNOWN):
    return {'identity': identity, 'state': state if state in STATES else UNKNOWN,
            'image_digest': image if isinstance(image, str) and DIGEST.fullmatch(image) else UNKNOWN,
            'image_kind': kind, 'evidence_ref': evidence,
            'exposure_ref': exposure, 'storage_ref': storage,
            'source_revision': UNKNOWN, 'config_digest': UNKNOWN,
            'readiness': UNKNOWN, 'role_identity': UNKNOWN}


def select_inventory(paths, roster):
    result = {name: [] for name in roster}
    inputs = []
    router_refs = []
    for path in paths:
        data = load_private(path)
        ref = reference(path)
        inputs.append(ref)
        schema = data.get('schema')
        if schema == 'paxeer-x.fly-inventory.v1':
            require(data.get('read_only') is True, 'inventory must be read-only')
            apps = {app['app']: app for app in data['apps']}
            for service in data['services']:
                name = service['service']
                require(name in result, 'unknown inventory service')
                for appname in service['expected_apps_from_source']:
                    app = apps.get(appname)
                    evidence = ref + '#apps/' + appname
                    if app is None:
                        result[name].append(observation(appname, UNKNOWN, UNKNOWN,
                                                       UNKNOWN, evidence))
                        continue
                    metadata = app.get('machines')
                    require(metadata is None or isinstance(metadata, dict),
                            'invalid machine metadata')
                    machines = metadata.get('machines') if metadata is not None else None
                    require(machines is None or isinstance(machines, list),
                            'invalid machine collection')
                    collected = metadata is not None and metadata.get('exit_code') == 0
                    if machines is None or not collected or not machines:
                        state = UNKNOWN
                        if (app.get('deployment_state') == 'absent_from_authenticated_app_list'
                                and app.get('observed_app') is None):
                            state = 'absent'
                        elif app.get('deployment_state') == 'pending':
                            state = 'pending'
                        result[name].append(observation(appname, state, UNKNOWN,
                                                       UNKNOWN, evidence))
                        continue
                    for machine in machines:
                        require(isinstance(machine, dict), 'invalid machine record')
                        image = machine.get('image_ref')
                        require(image is None or isinstance(image, dict), 'invalid image metadata')
                        item = observation(appname + '/' + machine['id'], machine['state'],
                                           image.get('digest') if image is not None else UNKNOWN,
                                           'registry-manifest', evidence,
                                           evidence + '/service_ports', evidence + '/storage')
                        result[name].append(item)
            if 'router' in data:
                router_refs.append(ref + '#router')
        elif schema == 'paxeer-x/docker-inventory/v1':
            require(data.get('mode') == 'read-only', 'inventory must be read-only')
            wallet = data['wallet']
            require(wallet.get('exit_code') == 0, 'wallet inventory read failed')
            for container in wallet['metadata']['containers']:
                item = observation(container['id'], container['status'], container.get('image_id'),
                                   'docker-image-id', ref + '#wallet/metadata/containers',
                                   ref + '#wallet/metadata/containers/ports',
                                   ref + '#wallet/metadata/containers/mounts')
                result['wallet-ui'].append(item)
            result['explorer'].append(observation('public-endpoint', UNKNOWN, UNKNOWN,
                                                  'unknown', ref + '#explorer'))
        elif schema == 'paxeer-x.runtime-selection.v1':
            require(data.get('read_only') is True, 'inventory must be read-only')
            for service in data['services']:
                require(service['id'] in result, 'unknown inventory service')
                result[service['id']].extend(service['observations'])
        else:
            raise Invalid('unsupported sanitized inventory schema')
    for rows in result.values():
        validate_observations(rows)
    return result, inputs, router_refs


def deployment_decision(service, services):
    bindings = service['bindings']
    if any(value == UNKNOWN for value in bindings.values()):
        return 'unknown'
    if not service['dependency_ids'] and service['dependencies_ref'] == UNKNOWN:
        return 'unknown'
    for name in service['dependency_ids']:
        dependency = services[name]
        if any(dependency['bindings'][field] == UNKNOWN for field in
               ('owner_ref', 'authority_ref', 'membership_ref', 'storage_ref', 'data_ref')):
            return 'unknown'
        if not dependency['observations'] or any(
                row['state'] not in ('started', 'running') or row['readiness'] != 'ready'
                for row in dependency['observations']):
            return 'unknown'
    rows = service['observations']
    if not rows or any(row['state'] == UNKNOWN for row in rows):
        return 'unknown'
    if all(row['state'] == 'absent' for row in rows):
        return 'deploy'
    if any(row['state'] not in ('running', 'started') or row['readiness'] != 'ready'
           or row['role_identity'] == UNKNOWN or row['source_revision'] == UNKNOWN
           or row['config_digest'] == UNKNOWN or row['image_digest'] == UNKNOWN for row in rows):
        return 'unknown'
    if any(row['image_digest'] != bindings['image_digest'] or
           row['source_revision'] != bindings['source_revision'] for row in rows):
        return 'update'
    if any(row['config_digest'] != bindings['config_digest'] for row in rows):
        return 'configure'
    return 'preserved'



def validate_observations(rows):
    identities = set()
    for row in rows:
        keys(row, ('identity', 'state', 'image_digest', 'image_kind', 'evidence_ref',
                   'exposure_ref', 'storage_ref', 'source_revision', 'config_digest',
                   'readiness', 'role_identity'), 'observation')
        require(isinstance(row['identity'], str) and re.fullmatch(r'[A-Za-z0-9_./:-]{1,256}', row['identity'])
                and row['identity'] not in identities, 'invalid or duplicate runtime identity')
        identities.add(row['identity'])
        require(row['state'] in STATES and row['readiness'] in ('ready', 'not-ready', UNKNOWN),
                'invalid runtime state')
        require(row['image_kind'] in ('registry-manifest', 'docker-image-id', UNKNOWN), 'invalid image kind')
        for field in ('image_digest', 'config_digest'):
            require(row[field] == UNKNOWN or DIGEST.fullmatch(row[field]), 'image/configuration must be immutable')
        require(row['source_revision'] == UNKNOWN or HEX.fullmatch(row['source_revision']), 'invalid observed source')
        for field in ('evidence_ref', 'exposure_ref', 'storage_ref', 'role_identity'):
            require((field != 'evidence_ref' and row[field] == UNKNOWN) or REF.fullmatch(row[field]),
                    'invalid observation reference')
        if row['readiness'] == 'ready':
            require(row['state'] in ('started', 'running') and row['role_identity'] != UNKNOWN,
                    'readiness requires a running identified role')

def validate(manifest, roster, repo=None, require_ready=False, mainline='refs/heads/main'):
    keys(manifest, ('schema', 'created_at', 'source', 'foundation', 'services',
                    'evidence_refs', 'router_refs', 'branch_changes'), 'manifest')
    require(manifest['schema'] == SCHEMA, 'unsupported candidate schema')
    require(manifest['foundation'] == FOUNDATION, 'foundation preservation contract changed')
    src = manifest['source']
    keys(src, ('revision', 'tree', 'mainline_revision', 'integrated', 'dirty', 'release_credit'), 'source')
    for field in ('revision', 'tree', 'mainline_revision'):
        require(isinstance(src[field], str) and HEX.fullmatch(src[field]), 'invalid source identity')
    for field in ('integrated', 'dirty', 'release_credit'):
        require(type(src[field]) is bool, 'invalid source state')
    require(src['release_credit'] is False, 'inventory cannot grant release credit')
    if repo is not None:
        actual = source_identity(repo, mainline)
        require(src == actual, 'candidate source does not match checkout')
    for field in ('evidence_refs', 'router_refs'):
        require(isinstance(manifest[field], list), 'invalid reference list')
        for ref in manifest[field]:
            require(isinstance(ref, str) and REF.fullmatch(ref), 'invalid evidence reference')
    require(isinstance(manifest['branch_changes'], list), 'invalid branch change list')
    for change in manifest['branch_changes']:
        keys(change, ('revision', 'evidence_ref', 'integration', 'pending_tasks'), 'branch change')
        require(HEX.fullmatch(change['revision']) and REF.fullmatch(change['evidence_ref']),
                'invalid branch reference')
        require(change['integration'] == 'not-credited' and change['pending_tasks'] and
                all(re.fullmatch(r'24\.[1-9]', task) for task in change['pending_tasks']),
                'branch-only changes must remain pending without mainline credit')
    require(isinstance(manifest['services'], list), 'invalid service list')
    services = {}
    for service in manifest['services']:
        keys(service, ('id', 'declaration', 'bindings', 'observations', 'dependency_ids',
                       'dependencies_ref', 'action', 'mutation_allowed'), 'service')
        name = service['id']
        require(name in roster and name not in services, 'unexpected or duplicate service')
        require(service['declaration'] == roster[name], 'catalogue contract changed')
        services[name] = service
        bindings = service['bindings']
        keys(bindings, BINDING_FIELDS, 'bindings')
        for field, value in bindings.items():
            require(isinstance(value, str), 'invalid binding')
            if value != UNKNOWN:
                pattern = HEX if field == 'source_revision' else DIGEST if field.endswith('digest') else REF
                require(pattern.fullmatch(value), 'invalid binding reference')
        require(isinstance(service['dependency_ids'], list) and
                len(set(service['dependency_ids'])) == len(service['dependency_ids']) and
                all(dep in roster and dep != name for dep in service['dependency_ids']),
                'invalid dependency identities')
        require(service['dependencies_ref'] == UNKNOWN or
                REF.fullmatch(service['dependencies_ref']), 'invalid dependency reference')
        require(service['mutation_allowed'] is False, 'inventory never authorizes mutation')
        require(isinstance(service['observations'], list), 'invalid observations')
        validate_observations(service['observations'])
    require(set(services) == set(roster), 'service catalogue coverage is incomplete')
    visiting, visited = set(), set()

    def visit(name):
        require(name not in visiting, 'cyclic operational dependencies')
        if name in visited:
            return
        visiting.add(name)
        for dependency in services[name]['dependency_ids']:
            visit(dependency)
        visiting.remove(name)
        visited.add(name)

    for name in services:
        visit(name)
    for service in services.values():
        require(service['action'] == deployment_decision(service, services), 'unsafe deployment decision')
    if require_ready:
        require(not src['dirty'] and src['integrated'] and
                all(service['action'] == 'preserved' for service in services.values()),
                'candidate has unresolved readiness or source prerequisites')
    return manifest


def create(args):
    roster = catalogue(args.spec)
    observations, refs, router = select_inventory(args.inventory, roster)
    branches = []
    if args.branch_notes:
        notes = load_private(args.branch_notes)
        require(notes.get('schema') == 'runtime-24-branch-candidates-v1', 'invalid branch metadata schema')
        ref = reference(args.branch_notes)
        refs.append(ref)
        for lead in notes['candidates']:
            branches.append({'revision': lead['HEAD'], 'evidence_ref': ref + '#candidates',
                             'integration': 'not-credited', 'pending_tasks': ['24.' + str(i) for i in range(1, 10)]})
    if args.source_notes:
        notes = load_private(args.source_notes)
        ref = reference(args.source_notes)
        refs.append(ref)
        for lead in notes.get('branch_candidate_leads', []):
            if any(branch['revision'] == lead['tip'] for branch in branches):
                continue
            branches.append({'revision': lead['tip'], 'evidence_ref': ref + '#branch_candidate_leads',
                             'integration': 'not-credited', 'pending_tasks': ['24.' + str(i) for i in range(1, 10)]})
    services = [{'id': name, 'declaration': declaration, 'bindings': unknown_bindings(),
                 'observations': observations[name], 'dependency_ids': [],
                 'dependencies_ref': UNKNOWN, 'action': UNKNOWN, 'mutation_allowed': False}
                for name, declaration in roster.items()]
    manifest = {'schema': SCHEMA, 'created_at': datetime.datetime.now(datetime.timezone.utc).isoformat(),
                'source': source_identity(args.repo, args.mainline), 'foundation': copy.deepcopy(FOUNDATION),
                'services': services, 'evidence_refs': refs, 'router_refs': router, 'branch_changes': branches}
    validate(manifest, roster, args.repo, mainline=args.mainline)
    write_private(args.output, manifest)


def main():
    parser = argparse.ArgumentParser(description='Read-only candidate inventory. Inputs must be sanitized, owned 0600 JSON; never supply environment files or credential values. Unknown evidence blocks action; inventories grant no release or mutation authority.')
    sub = parser.add_subparsers(dest='command', required=True)
    make = sub.add_parser('create', help='Bind the complete service catalogue to Git identity and sanitized collector observations')
    make.add_argument('--repo', type=Path, required=True)
    make.add_argument('--spec', type=Path, required=True)
    make.add_argument('--mainline', default='refs/heads/main', help='Local mainline ref; no fetch is performed')
    make.add_argument('--inventory', action='append', default=[], help='Repeat for sanitized Fly, Docker, or runtime-selection JSON')
    make.add_argument('--branch-notes', help='Private exact branch ancestry and touched-path inventory; retained by secure reference without integration credit')
    make.add_argument('--source-notes', help='Private source contract inventory containing branch_candidate_leads')
    make.add_argument('--output', required=True, help='New private JSON file; existing files are never overwritten')
    check = sub.add_parser('validate', help='Validate complete schema, provenance, immutable bindings and fail-closed decisions')
    check.add_argument('manifest')
    check.add_argument('--repo', type=Path, required=True)
    check.add_argument('--spec', type=Path, required=True)
    check.add_argument('--mainline', default='refs/heads/main')
    check.add_argument('--require-ready', action='store_true', help='Refuse dirty, branch-only or unresolved candidates; does not certify release')
    inventory = sub.add_parser('inventory', help='Select actual observed runtime metadata from sanitized read-only collector records without repeating network probes')
    inventory.add_argument('--input', action='append', required=True)
    inventory.add_argument('--spec', type=Path, required=True)
    inventory.add_argument('--service', action='append', help='Exact catalogue ID; default all services')
    inventory.add_argument('--output', required=True)
    args = parser.parse_args()
    try:
        if args.command == 'create':
            create(args)
        elif args.command == 'validate':
            validate(load_private(args.manifest), catalogue(args.spec), args.repo, args.require_ready, args.mainline)
        else:
            roster = catalogue(args.spec)
            rows, refs, router = select_inventory(args.input, roster)
            selection = args.service or list(roster)
            require(len(set(selection)) == len(selection) and all(name in roster for name in selection),
                    'invalid service selection')
            output = {'schema': 'paxeer-x.runtime-selection.v1', 'read_only': True,
                      'evidence_refs': refs, 'router_refs': router,
                      'services': [{'id': name, 'observations': rows[name]} for name in selection]}
            write_private(args.output, output)
        print('candidate: complete; no runtime mutation or release credit')
        return 0
    except (Invalid, OSError, ValueError, KeyError, TypeError, RecursionError):
        print('candidate: refused invalid, unavailable, or unsafe input; no runtime mutation', file=sys.stderr)
        return 2


if __name__ == '__main__':
    sys.exit(main())
