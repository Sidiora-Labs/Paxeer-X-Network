#!/usr/bin/env python3
"""Deterministic serial-integrator inputs. Never rewrites bindings or installs tools."""
import argparse, hashlib, json, pathlib, re, time

def sha(b):
    return hashlib.sha256(b).hexdigest()

def canonical(v):
    return json.dumps(v, sort_keys=True, separators=(',', ':')).encode()

def typ(x):
    t = x['type']
    return '(' + ','.join(map(typ, x.get('components', []))) + ')' + t[5:] if t.startswith('tuple') else t

def keccak256(raw):
    mask = (1 << 64) - 1
    rotations = ((0, 36, 3, 41, 18), (1, 44, 10, 45, 2),
                 (62, 6, 43, 15, 61), (28, 55, 25, 21, 56), (27, 20, 39, 8, 14))
    constants = (0x1, 0x8082, 0x800000000000808a, 0x8000000080008000,
                 0x808b, 0x80000001, 0x8000000080008081, 0x8000000000008009,
                 0x8a, 0x88, 0x80008009, 0x8000000a, 0x8000808b,
                 0x800000000000008b, 0x8000000000008089, 0x8000000000008003,
                 0x8000000000008002, 0x8000000000000080, 0x800a,
                 0x800000008000000a, 0x8000000080008081, 0x8000000000008080,
                 0x80000001, 0x8000000080008008)
    def rotate(v, n):
        return ((v << n) | (v >> ((64 - n) % 64))) & mask
    padded = bytearray(raw) + b'\x01'
    padded.extend(bytes((-len(padded)) % 136))
    padded[-1] |= 0x80
    state = [0] * 25
    for start in range(0, len(padded), 136):
        for lane in range(17):
            state[lane] ^= int.from_bytes(padded[start + lane * 8:start + lane * 8 + 8], 'little')
        for constant in constants:
            columns = [state[x] ^ state[x + 5] ^ state[x + 10] ^ state[x + 15] ^ state[x + 20] for x in range(5)]
            for x in range(5):
                delta = columns[(x - 1) % 5] ^ rotate(columns[(x + 1) % 5], 1)
                for y in range(5):
                    state[x + 5 * y] ^= delta
            permuted = [0] * 25
            for x in range(5):
                for y in range(5):
                    permuted[y + 5 * ((2 * x + 3 * y) % 5)] = rotate(state[x + 5 * y], rotations[x][y])
            for x in range(5):
                for y in range(5):
                    state[x + 5 * y] = permuted[x + 5 * y] ^ ((~permuted[(x + 1) % 5 + 5 * y]) & permuted[(x + 2) % 5 + 5 * y])
            state[0] ^= constant
    return b''.join(v.to_bytes(8, 'little') for v in state)[:32]

def reviewed_path(roots, relative, prefix):
    path = pathlib.PurePosixPath(relative)
    if path.is_absolute() or str(path) != relative or '..' in path.parts or not relative.startswith(prefix):
        raise ValueError('path escapes reviewed root')
    selected = roots / relative
    if any(p.is_symlink() for p in [selected, *selected.parents]) or not selected.resolve().is_relative_to(roots.resolve()):
        raise ValueError('path escapes reviewed root')
    return selected.resolve()

def export(reviewed, roots):
    rows = []
    builds = {}
    sources = {}
    for s in reviewed['sourceProvenance']:
        p = reviewed_path(roots, s['path'], 'contracts/')
        if not p.is_relative_to(roots.resolve()):
            raise ValueError('source escapes reviewed root')
        data = p.read_bytes()
        if sha(data) != s['sha256']:
            raise ValueError('source hash drift: ' + s['path'])
        sources[s['path']] = data.decode()
    names = [c['name'] for c in reviewed['contracts']]
    if len(names) != len(set(names)):
        raise ValueError('duplicate reviewed contract family')
    for c in reviewed['contracts']:
        p = reviewed_path(roots, c['artifactPath'], 'artifacts/contracts/')
        if not p.is_relative_to((roots / 'artifacts/contracts').resolve()):
            raise ValueError('artifact escapes reviewed root')
        raw = p.read_bytes()
        if sha(raw) != c['artifactSha256']:
            raise ValueError('artifact hash drift: ' + c['name'])
        dbg = json.loads(p.with_suffix('.dbg.json').read_bytes())
        linked_build = p.parent / dbg['buildInfo']
        if any(part.is_symlink() for part in [linked_build, *linked_build.parents]):
            raise ValueError('build info escapes reviewed root')
        bp = linked_build.resolve()
        if not bp.is_relative_to((roots / 'artifacts/build-info').resolve()):
            raise ValueError('build info escapes reviewed root')
        if bp not in builds:
            br = bp.read_bytes()
            builds[bp] = (sha(br), json.loads(br))
        bh, build = builds[bp]
        if bh != c['buildInfoSha256'] or build['solcLongVersion'] != c['compilerVersion']:
            raise ValueError('compiler/build provenance drift')
        if sha(build['input']['sources'][c['sourceName']]['content'].encode()) != c['sourceSha256']:
            raise ValueError('compiled source drift')
        source = reviewed_path(roots, c['sourceName'], 'contracts/')
        if sha(source.read_bytes()) != c['sourceSha256']:
            raise ValueError('current source hash drift: ' + c['name'])
        a = json.loads(raw)
        if a.get('contractName') != c['name'] or a.get('sourceName') != c['sourceName']:
            raise ValueError('artifact contract identity drift')
        if a['abi'] != c['abi']:
            raise ValueError('ABI differs: ' + c['name'])
        if sha(canonical(a['abi'])) != c['abiSha256']:
            raise ValueError('ABI hash drift: ' + c['name'])
        reviewed_events = {e['signature']: e for e in c['events']}
        reviewed_functions = {f['signature']: f for f in c['functions']}
        if len(reviewed_events) != len(c['events']) or len(reviewed_functions) != len(c['functions']):
            raise ValueError('duplicate reviewed signature: ' + c['name'])
        events = []
        functions = []
        for e in a['abi']:
            if e['type'] not in ('event', 'function'):
                continue
            signature = e['name'] + '(' + ','.join(map(typ, e['inputs'])) + ')'
            if e['type'] == 'function':
                f = reviewed_functions.pop(signature, None)
                if f is None or not re.fullmatch('0x[0-9a-f]{8}', f.get('selector', '')) or f.get('stateMutability') != e['stateMutability'] or f.get('selector') != '0x' + keccak256(signature.encode())[:4].hex():
                    raise ValueError('unmapped function signature: ' + signature)
                functions.append({'signature': signature, 'selector': f['selector'], 'inputs': e['inputs'], 'outputs': e['outputs'], 'stateMutability': e['stateMutability']})
                continue
            event = reviewed_events.pop(signature, None)
            if event is None or not re.fullmatch('0x[0-9a-f]{64}', event.get('topic0', '')) or event.get('anonymous') != e['anonymous'] or event.get('topic0') != '0x' + keccak256(signature.encode()).hex():
                raise ValueError('unmapped event signature: ' + signature)
            sites = [{'source': name, 'line': text[:m.start()].count('\n') + 1} for name, text in sorted(sources.items()) for m in re.finditer('\\bemit\\s+' + re.escape(e['name']) + '\\s*\\(', text)]
            events.append({'signature': signature, 'topic0': event['topic0'], 'anonymous': e['anonymous'], 'inputs': e['inputs'], 'nativeEmissionCandidates': sites, 'note': 'Candidate sites require contract inheritance/caller review; declaration alone is not authority.'})
        if reviewed_events or reviewed_functions:
            raise ValueError('reviewed signature absent from artifact: ' + c['name'])
        rows.append({k: c[k] for k in ['name', 'sourceName', 'sourceSha256', 'artifactPath', 'artifactSha256', 'abiSha256', 'buildInfoSha256', 'compilerVersion']} | {'generatorVersion': 'L02-v2', 'events': sorted(events, key=lambda e: e['signature']), 'functions': sorted(functions, key=lambda f: f['signature'])})
    return {'chainId': 125, 'reviewedHash': sha(canonical(reviewed)), 'bundles': sorted(rows, key=lambda c: c['name'])}

def export_revisions(current, roots, archives, enabled):
    known = {c['name'] for c in current['contracts']}
    if set(enabled) - known:
        raise ValueError('unmapped enabled families: ' + str(sorted(set(enabled) - known)))
    outputs = [export(current, roots)]
    for raw, expected_sha256 in archives:
        if not re.fullmatch('[0-9a-f]{64}', expected_sha256) or sha(raw) != expected_sha256:
            raise ValueError('unreviewed historical artifact identity')
        historical = json.loads(raw)
        if not historical.get('contracts') or any(c['name'] != 'EventEmitter' for c in historical['contracts']):
            raise ValueError('only reviewed EventEmitter history is admitted')
        outputs.append(export(historical, roots))
    bundles = []
    seen = set()
    for output in outputs:
        for bundle in output['bundles']:
            identity = bundle['name'], bundle['abiSha256']
            if identity in seen:
                raise ValueError('duplicate role/revision identity')
            seen.add(identity)
            bundles.append(bundle)
    return {'chainId': 125, 'schemaVersion': 2, 'generatorVersion': 'L02-revisions-v2',
            'reviewedArtifactHashes': sorted([sha(canonical(current))] + [digest for _, digest in archives]),
            'bundles': sorted(bundles, key=lambda c: (c['name'], c['abiSha256']))}

def revision_diff(before, after):
    old = {(c['name'], c['abiSha256']): c for c in before['bundles']}
    new = {(c['name'], c['abiSha256']): c for c in after['bundles']}
    return [{'role': key[0], 'abiSha256': key[1], 'before': old.get(key), 'after': new.get(key)}
            for key in sorted(old.keys() | new.keys()) if old.get(key) != new.get(key)]

def export_optical_registry(review, roots):
    if set(review) != {'schemaVersion', 'kind', 'bundle'} or review['schemaVersion'] != 1 or review['kind'] != 'reviewed-optical-registry':
        raise ValueError('unsupported optical review')
    bundle = review['bundle']
    if bundle['name'] != 'OpticalRegistry' or bundle['sourceName'] != 'contracts/opticals/OpticalRegistry.sol' or bundle['artifactPath'] != 'artifacts/contracts/opticals/OpticalRegistry.sol/OpticalRegistry.json':
        raise ValueError('wrong optical source identity')
    source_path = roots / bundle['sourceName']
    artifact_path = roots / bundle['artifactPath']
    for path in (source_path, artifact_path):
        if any(p.is_symlink() for p in [path, *path.parents]) or not path.resolve().is_relative_to(roots.resolve()):
            raise ValueError('unsafe optical source path')
    artifact_raw = artifact_path.read_bytes()
    artifact = json.loads(artifact_raw)
    debug = json.loads(artifact_path.with_suffix('.dbg.json').read_bytes())
    build_path = (artifact_path.parent / debug['buildInfo']).resolve()
    if not build_path.is_relative_to((roots / 'artifacts/build-info').resolve()) or build_path.is_symlink():
        raise ValueError('unsafe optical compiler provenance')
    build_raw = build_path.read_bytes()
    build = json.loads(build_raw)
    compiled_source = build['input']['sources'][bundle['sourceName']]['content'].encode()
    if sha(artifact_raw) != bundle['artifactSha256'] or sha(canonical(artifact['abi'])) != bundle['abiSha256'] or artifact['abi'] != bundle['abi']:
        raise ValueError('optical artifact or ABI drift')
    if sha(source_path.read_bytes()) != bundle['sourceSha256'] or sha(compiled_source) != bundle['sourceSha256']:
        raise ValueError('optical compiled source drift')
    if sha(build_raw) != bundle['buildInfoSha256'] or build['solcLongVersion'] != bundle['compilerVersion']:
        raise ValueError('optical compiler provenance drift')
    return review

def generation_measurement(reviewed, roots, enabled, previous=None, binding_pair=None, reviewed_diff_sha256=None):
    if not enabled or len(enabled) != len(set(enabled)):
        raise ValueError('explicit unique enabled families required')
    known = {c['name'] for c in reviewed['contracts']}
    if set(enabled) - known:
        raise ValueError('unmapped enabled families: ' + str(sorted(set(enabled) - known)))
    started = time.perf_counter_ns()
    first = export(reviewed, roots)
    first_bytes = canonical(first)
    second = export(reviewed, roots)
    second_bytes = canonical(second)
    if first_bytes != second_bytes:
        raise ValueError('canonical ABI export is nondeterministic')
    matrix = []
    for bundle in first['bundles']:
        if bundle['name'] not in enabled:
            continue
        for event in bundle['events']:
            matrix.append({'family': bundle['name'], 'abiSha256': bundle['abiSha256'],
                           'kind': 'event', 'signature': event['signature'],
                           'identity': event['topic0'], 'runtimeCoverage': 'unmeasured'})
        for function in bundle['functions']:
            matrix.append({'family': bundle['name'], 'abiSha256': bundle['abiSha256'],
                           'kind': 'function', 'signature': function['signature'],
                           'identity': function['selector'], 'runtimeCoverage': 'unmeasured'})
    blockers = ['generated_bindings_unmeasured', 'decoder_replay_and_burst_unmeasured']
    binding_measurement = None
    if binding_pair is not None:
        binding_measurement = measure_generated_bindings(reviewed, sha(first_bytes), enabled, binding_pair)
        blockers.remove('generated_bindings_unmeasured')
    diff = None
    if previous is not None:
        diff = revision_diff(previous, first)
        if diff and reviewed_diff_sha256 != sha(canonical(diff)):
            blockers.append('ABI diff requires explicit review of the exact emitted diff digest')
    else:
        blockers.append('previous_reviewed_ABI_export_unavailable')
    if reviewed_diff_sha256 is not None and (diff is None or reviewed_diff_sha256 != sha(canonical(diff))):
        raise ValueError('reviewed ABI diff identity differs')
    return {'schemaVersion': 1, 'measurementKind': 'actual_artifact_export',
            'reviewedHash': sha(canonical(reviewed)), 'firstExportSha256': sha(first_bytes),
            'secondExportSha256': sha(second_bytes), 'byteIdentical': True,
            'elapsedNanoseconds': time.perf_counter_ns() - started,
            'enabledFamilies': sorted(enabled), 'coverageMatrix': matrix,
            'generatedBindings': binding_measurement, 'reviewedAbiDiffSha256': reviewed_diff_sha256, 'abiDiff': diff, 'abiDiffSha256': sha(canonical(diff)) if diff is not None else None,
            'unresolvedBlockers': blockers, 'releaseReady': False}

def read_measurement_artifact(path, maximum=32 * 1024 * 1024):
    with path.open('rb') as source:
        raw = source.read(maximum + 1)
    if len(raw) > maximum:
        raise ValueError('measurement artifact exceeds byte bound')
    return raw

def measure_generated_bindings(reviewed, export_sha256, enabled, pair):
    if len(pair) != 2:
        raise ValueError('two independently reviewed binding generation manifests required')
    expected = {c['name']: c for c in reviewed['contracts'] if c['name'] in enabled}
    runs = []
    for raw, root, approved_sha256 in pair:
        if sha(raw) != approved_sha256 or not re.fullmatch('[0-9a-f]{64}', approved_sha256):
            raise ValueError('unreviewed generated binding manifest')
        document = json.loads(raw)
        if set(document) != {'schemaVersion', 'reviewedExportSha256', 'bundles'} or document['schemaVersion'] != 1 or document['reviewedExportSha256'] != export_sha256:
            raise ValueError('generated bindings are not bound to current compiled export')
        observed = {}
        for entry in document['bundles']:
            if set(entry) != {'family', 'abiSha256', 'goPath', 'frontendPath'} or entry['family'] not in expected or entry['family'] in observed:
                raise ValueError('unmapped or duplicate generated binding family')
            bundle = expected[entry['family']]
            if entry['abiSha256'] != bundle['abiSha256']:
                raise ValueError('generated binding ABI revision differs')
            go_path = reviewed_path(root, entry['goPath'], '')
            frontend_path = reviewed_path(root, entry['frontendPath'], '')
            if go_path.suffix != '.go' or frontend_path.suffix != '.json':
                raise ValueError('actual Go and frontend JSON binding artifacts required')
            go_raw, frontend_raw = read_measurement_artifact(go_path, 16 * 1024 * 1024), read_measurement_artifact(frontend_path, 16 * 1024 * 1024)
            if len(go_raw) > 16 * 1024 * 1024 or len(frontend_raw) > 16 * 1024 * 1024:
                raise ValueError('generated binding artifact exceeds bounds')
            go_source = go_raw.decode()
            if 'Code generated - DO NOT EDIT.' not in go_source:
                raise ValueError('actual generated Go binding provenance missing')
            quoted = re.findall(r'ABI:\s*("(?:[^"\\]|\\.)*")', go_source)
            constants = re.findall(r'\b\w+ABI\s*=\s*("(?:[^"\\]|\\.)*")', go_source)
            declarations = quoted + constants
            if len(declarations) != 1 or json.loads(json.loads(declarations[0])) != bundle['abi']:
                raise ValueError('generated Go ABI differs from compiled artifact')
            frontend = json.loads(frontend_raw)
            if isinstance(frontend, dict):
                if frontend.get('contractName') != bundle['name']:
                    raise ValueError('frontend binding contract identity differs')
                frontend = frontend.get('abi')
            if frontend != bundle['abi']:
                raise ValueError('frontend ABI differs from compiled artifact')
            observed[entry['family']] = {'goSha256': sha(go_raw), 'frontendSha256': sha(frontend_raw), 'abiSha256': bundle['abiSha256']}
        if set(observed) != set(expected):
            raise ValueError('missing enabled generated binding family')
        runs.append(observed)
    if runs[0] != runs[1]:
        raise ValueError('generated Go/frontend bytes differ across runs')
    return {'byteIdentical': True, 'reviewedExportSha256': export_sha256,
            'firstManifestSha256': pair[0][2], 'secondManifestSha256': pair[1][2],
            'bundles': runs[0]}

def main():
    p = argparse.ArgumentParser()
    p.add_argument('--contracts-root', type=pathlib.Path, required=True)
    p.add_argument('--output', type=pathlib.Path, required=True)
    p.add_argument('--previous', type=pathlib.Path)
    p.add_argument('--measurement-output', type=pathlib.Path)
    p.add_argument('--bindings-first', metavar='PATH=SHA256')
    p.add_argument('--bindings-second', metavar='PATH=SHA256')
    p.add_argument('--reviewed-diff-sha256')
    p.add_argument('--previous-sha256')
    p.add_argument('--diff', type=pathlib.Path)
    p.add_argument('--enabled', action='append', default=[])
    p.add_argument('--reviewed-revision', action='append', default=[], metavar='PATH=SHA256')
    a = p.parse_args()
    reviewed = json.loads((pathlib.Path(__file__).resolve().parents[1] / 'catalog/reviewed.json').read_text())
    known = {c['name'] for c in reviewed['contracts']}
    if set(a.enabled) - known:
        raise ValueError('unmapped enabled families: ' + str(set(a.enabled) - known))
    if a.reviewed_revision:
        archives = []
        for selected in a.reviewed_revision:
            path, separator, digest = selected.rpartition('=')
            if not separator or not path:
                raise ValueError('reviewed revision requires PATH=SHA256')
            artifact = pathlib.Path(path)
            if any(p.is_symlink() for p in [artifact, *artifact.parents]) or not artifact.is_file():
                raise ValueError('unsafe historical artifact')
            archives.append((artifact.read_bytes(), digest))
        result = export_revisions(reviewed, a.contracts_root, archives, a.enabled)
    else:
        result = export(reviewed, a.contracts_root)
    a.output.write_bytes(canonical(result) + b'\n')
    if a.measurement_output:
        if a.reviewed_revision:
            raise ValueError('historical generator measurement requires serial revision source roots')
        previous = None
        if a.previous:
            previous_raw = read_measurement_artifact(a.previous)
            if not a.previous_sha256 or sha(previous_raw) != a.previous_sha256:
                raise ValueError('previous ABI export requires exact reviewed SHA256')
            previous = json.loads(previous_raw)
        pair = None
        if a.bindings_first or a.bindings_second:
            if not a.bindings_first or not a.bindings_second:
                raise ValueError('both reviewed binding manifests are required')
            pair = []
            for selected in (a.bindings_first, a.bindings_second):
                name, separator, digest = selected.rpartition('=')
                if not separator or not name:
                    raise ValueError('binding manifest requires PATH=SHA256')
                path = pathlib.Path(name)
                if any(part.is_symlink() for part in [path, *path.parents]):
                    raise ValueError('unsafe binding manifest path')
                pair.append((read_measurement_artifact(path), path.resolve().parent, digest))
        measured = generation_measurement(reviewed, a.contracts_root, a.enabled, previous, pair, a.reviewed_diff_sha256)
        a.measurement_output.write_bytes(canonical(measured) + b'\n')
    if a.previous:
        old = json.loads(a.previous.read_text())
        if a.reviewed_revision:
            diff = revision_diff(old, result)
        else:
            before = {c['name']: c for c in old['bundles']}
            after = {c['name']: c for c in result['bundles']}
            diff = {k: {'before': before.get(k), 'after': after.get(k)} for k in sorted(before.keys() | after.keys()) if before.get(k) != after.get(k)}
        if not a.diff:
            raise ValueError('--previous requires --diff')
        a.diff.write_bytes(canonical(diff) + b'\n')
if __name__ == '__main__':
    main()
