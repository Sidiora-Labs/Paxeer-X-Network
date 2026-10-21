"""Coordinator-only acceptance ledger for historical, live-fence-qualified artifacts.

A signed acceptance retires a live lease. Historical staging verifies this durable
coordinator attestation; it never fabricates a live store or changes task status.
"""
import hashlib
import hmac
import json
import os
from pathlib import Path
import shutil
import tempfile
from . import evidence, patches

class AcceptanceError(ValueError):
    pass

def _sign(body, secret):
    evidence._secret(secret)
    return hmac.new(secret, evidence._json(body), hashlib.sha256).hexdigest()

def _signed_load(path, secret):
    value = json.loads(Path(path).read_bytes())
    signature = value.pop('signature', None)
    if not isinstance(signature, str) or not hmac.compare_digest(signature, _sign(value, secret)):
        raise AcceptanceError('invalid accepted artifact signature')
    return value

def _receipt_path(token):
    attempt_dir = token.get('attempt_dir') or token.get('allocation', {}).get('attempt_dir')
    if not isinstance(attempt_dir, str):
        raise AcceptanceError('coordinator attempt_dir missing')
    return Path(attempt_dir) / 'checks' / 'evidence.json'

def _historical(body, secret, candidate, directory, trusted):
    result = body['evidence']
    raw = dict(result)
    signature = raw.pop('signature', None)
    if not isinstance(signature, str) or not hmac.compare_digest(signature, _sign(raw, secret)):
        raise AcceptanceError('invalid historical receipt signature')
    digest = evidence.source_digest(candidate)
    if digest != result['source_digest'] or digest != result['source_before'] or digest != body['bindings']['candidate']:
        raise AcceptanceError('qualified candidate mismatch')
    if result['bindings'] != body['bindings'] or result['token'] != body['token']:
        raise AcceptanceError('historical token mismatch')
    if body['checks'] != trusted:
        raise AcceptanceError('trusted check registry changed')
    registry = {x['id']: x for x in trusted}
    receipts = result['receipts']
    if not registry or len(registry) != len(trusted) or len(receipts) != len(registry):
        raise AcceptanceError('empty or duplicate historical checks')
    seen = set()
    original = Path(result['root'])
    for receipt in receipts:
        ident = receipt['id']
        if ident in seen or ident not in registry:
            raise AcceptanceError('unregistered historical check')
        seen.add(ident)
        check = registry[ident]
        for field, target, default in [('command','command',None),('timeout','timeout',None),('inputs','input_paths',[]),('outputs','output_paths',[]),('proof','proof_policy',None)]:
            if check.get(field, default) != receipt.get(target):
                raise AcceptanceError('historical check definition changed')
        if receipt.get('executed') is not True or receipt.get('exit_code') != 0 or receipt.get('timed_out') is not False or receipt.get('skipped'):
            raise AcceptanceError('unqualified historical execution')
        if receipt.get('proof') != evidence._proof(receipt, check.get('proof')):
            raise AcceptanceError('historical proof mismatch')
        if not receipt.get('checker'):
            raise AcceptanceError('checker hashes missing')
        for kind in ('inputs','checker'):
            for path, expected in receipt[kind].items():
                path = Path(path)
                try:
                    actual_path = candidate / path.relative_to(original)
                except ValueError:
                    if kind == 'inputs':
                        raise AcceptanceError('historical input outside source')
                    actual_path = path
                if evidence._file(actual_path) != expected:
                    raise AcceptanceError('historical file changed')
        for path, expected in receipt['outputs'].items():
            if evidence._file(directory / 'outputs' / expected) != expected:
                raise AcceptanceError('historical output changed')
    return True

def _fsync_tree(directory):
    # Files first, then directories bottom-up, then containing publication dir.
    paths = list(Path(directory).rglob('*'))
    for path in paths:
        if path.is_file():
            with path.open('rb') as stream:
                os.fsync(stream.fileno())
    for path in sorted([Path(directory)] + [p for p in paths if p.is_dir()], key=lambda p: len(p.parts), reverse=True):
        descriptor = os.open(path, os.O_RDONLY | os.O_DIRECTORY)
        try:
            os.fsync(descriptor)
        finally:
            os.close(descriptor)

def _indexed(directory, body, store):
    if store is None:
        raise AcceptanceError('trusted coordinator accepted index required')
    record = store.accepted(body['token']['attempt'])
    if not isinstance(record, dict):
        raise AcceptanceError('artifact not durably accepted')
    latest = store.latest_accepted(body['task'])
    if not isinstance(latest, dict) or latest.get('attempt') != body['token']['attempt'] or latest.get('fence') != body['token']['fence']:
        raise AcceptanceError('superseded accepted attempt')
    artifact = directory / 'accepted.json'
    path = record.get('artifact_path', record.get('path'))
    digest = record.get('manifest_sha256')
    if path != str(artifact.resolve()) or digest != evidence._file(artifact):
        raise AcceptanceError('accepted index artifact identity mismatch')
    for field in ('task', 'attempt', 'fence', 'bindings'):
        if record.get(field) != body['token'].get(field):
            raise AcceptanceError('accepted index lease identity mismatch')
    return record

def accept_artifact(store, token, source_root, baseline, patch_manifest, owned, checks, secret, accepted_root):
    """Accept only while live, copy immutable bytes, then atomically retire lease."""
    store.validate(token)
    if evidence.source_digest(baseline) != token['bindings']['baseline']:
        raise AcceptanceError('baseline binding mismatch')
    result = json.loads(_receipt_path(token).read_bytes())
    if result['token'] != token:
        raise AcceptanceError('receipt attempt mismatch')
    evidence.publish(result, secret, store, source_root, checks)
    patch_directory, manifest = patches._load(patch_manifest)
    if sorted(owned) != manifest['owned']:
        raise AcceptanceError('trusted ownership mismatch')
    root = Path(accepted_root)
    patches._ancestors(root)
    root.mkdir(parents=True, exist_ok=True)
    descriptor = os.open(root.parent, os.O_RDONLY | os.O_DIRECTORY)
    try:
        os.fsync(descriptor)
    finally:
        os.close(descriptor)
    attempt = token['attempt']
    if not isinstance(attempt, str) or not attempt or any(c not in 'abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789._-' for c in attempt) or attempt in ('.','..'):
        raise AcceptanceError('invalid attempt id')
    final = root / attempt
    if final.exists():
        raise AcceptanceError('attempt already accepted')
    temporary = Path(tempfile.mkdtemp(prefix='.accept-', dir=root))
    reserved = False
    try:
        shutil.copytree(patch_directory, temporary / 'patch')
        (temporary / 'outputs').mkdir()
        for receipt in result['receipts']:
            for path, digest in receipt['outputs'].items():
                data = Path(path).read_bytes()
                if hashlib.sha256(data).hexdigest() != digest:
                    raise AcceptanceError('output changed during acceptance')
                (temporary / 'outputs' / digest).write_bytes(data)
        candidate = temporary / 'reconstructed'
        patches.stage_patches(baseline, [temporary/'patch'], candidate, verify=lambda p: evidence.source_digest(p) == result['source_digest'], ownership={manifest['patch_digest']: owned})
        body = dict(version=1, token=token, bindings=token['bindings'], task=token['task'],
                    patch_digest=manifest['patch_digest'], owned=owned, checks=checks, evidence=result)
        _historical(body, secret, candidate, temporary, checks)
        shutil.rmtree(candidate)
        body['signature'] = _sign(body, secret)
        (temporary / 'accepted.json').write_bytes(evidence._json(body))
        evidence.publish(result, secret, store, source_root, checks)
        _fsync_tree(temporary)
        manifest_sha256 = evidence._file(temporary / 'accepted.json')
        def durable_publish():
            nonlocal reserved
            # Called under Store's write transaction and live-fence validation.
            if evidence.source_digest(source_root) != result['source_digest']:
                raise AcceptanceError('qualified source changed before publication')
            final.mkdir()
            reserved = True
            temporary.rename(final)
            reserved = False
            descriptor = os.open(root, os.O_RDONLY | os.O_DIRECTORY)
            try:
                os.fsync(descriptor)
            finally:
                os.close(descriptor)
        # Filesystem publication precedes retirement while Store holds its live
        # lease transaction. A failed commit leaves an unindexed invalid orphan.
        artifact_path = (final / 'accepted.json').resolve()
        store.accept(token, str(artifact_path), manifest_sha256, publisher=durable_publish)
        return dict(path=str(final), id=attempt, task=token['task'], patch_digest=manifest['patch_digest'], accepted=True)
    except Exception:
        shutil.rmtree(temporary, ignore_errors=True)
        if reserved:
            try:
                final.rmdir()
            except OSError:
                pass
        raise

def stage_accepted(baseline, accepted_paths, ownership, checks_registry, secret, stage, verify, store=None):
    """Verify durable accepted artifacts and reconstruct each before merged staging."""
    if not accepted_paths:
        raise AcceptanceError('no accepted artifacts')
    manifests, trusted_ownership, tasks = [], {}, set()
    scratch = Path(tempfile.mkdtemp(prefix='accepted-review-'))
    try:
        for index, value in enumerate(accepted_paths):
            directory = Path(value)
            patches._tree(directory)
            body = _signed_load(directory/'accepted.json', secret)
            _indexed(directory, body, store)
            task = body['task']
            if evidence.source_digest(baseline) != body['bindings']['baseline']:
                raise AcceptanceError('accepted baseline mismatch')
            if body.get('version') != 1 or task in tasks or task not in ownership or task not in checks_registry:
                raise AcceptanceError('duplicate or unknown accepted task')
            tasks.add(task)
            if body['owned'] != ownership[task]:
                raise AcceptanceError('accepted ownership differs from trusted ownership')
            _, manifest = patches._load(directory/'patch')
            if manifest['patch_digest'] != body['patch_digest']:
                raise AcceptanceError('accepted patch changed')
            candidate = scratch / str(index)
            patches.stage_patches(baseline, [directory/'patch'], candidate, verify=lambda p: evidence.source_digest(p) == body['bindings']['candidate'], ownership={manifest['patch_digest']: ownership[task]})
            _historical(body, secret, candidate, directory, checks_registry[task])
            manifests.append(directory/'patch')
            trusted_ownership[manifest['patch_digest']] = ownership[task]
        result = patches.stage_patches(baseline, manifests, stage, verify=verify, ownership=trusted_ownership)
        result['accepted_tasks'] = sorted(tasks)
        return result
    finally:
        shutil.rmtree(scratch, ignore_errors=True)

def verify_accepted(baseline, accepted_path, owned, checks, secret, output_dir, store=None):
    """Reconstruct one durably accepted candidate without claiming merge success.

    A trusted coordinator Store/index is mandatory; signed filesystem orphans are
    not acceptance. Caller owns the new output_dir and its eventual cleanup.
    """
    directory = Path(accepted_path).resolve()
    patches._tree(directory)
    body = _signed_load(directory/'accepted.json', secret)
    _indexed(directory, body, store)
    if body.get('version') != 1 or body['owned'] != owned or body['checks'] != checks:
        raise AcceptanceError('trusted acceptance definitions mismatch')
    if evidence.source_digest(baseline) != body['bindings']['baseline']:
        raise AcceptanceError('accepted baseline binding mismatch')
    _, manifest = patches._load(directory/'patch')
    if manifest['patch_digest'] != body['patch_digest']:
        raise AcceptanceError('accepted patch changed')
    candidate = Path(output_dir)
    try:
        patches.stage_patches(baseline, [directory/'patch'], candidate,
                              verify=lambda p: evidence.source_digest(p) == body['bindings']['candidate'],
                              ownership={manifest['patch_digest']: owned})
        _historical(body, secret, candidate, directory, checks)
    except Exception:
        if candidate.exists():
            shutil.rmtree(candidate)
        raise
    return dict(task=body['task'], lane=body['token']['lane'], owner=body['token']['owner'],
                proofs={r['id']: r.get('proof') for r in body['evidence']['receipts']},
                attempt=body['token']['attempt'], fence=body['token']['fence'],
                bindings=body['bindings'], checks=checks, owned=owned,
                candidate_path=str(candidate.resolve()), patch_digest=body['patch_digest'],
                individually_verified=True, combined_qualified=False)
