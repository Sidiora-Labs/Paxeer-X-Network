"""Owned disposable full-mode layerx-agentd fixture.

A full-mode daemon reads program state through a real node program listener, a TLS receipt
authority and a deployment journal whose admissions the program registry verified against the
same sequencer. The fixture therefore composes the source-bound runtime fixture
(tests/daemon/paxeer_x_runtime_fixture.py: paxd chain, layerxd sequencer and authority replica)
with the platform receipt authority and program registry, generates every agentd credential and
path itself under an owner-only directory, and refuses before launching anything when an artifact
it needs is absent or bound to a different source.

Inputs:
  PAXEER_X_RUNTIME_ARTIFACTS          canonical foundation bundle manifest (owner-only)
  PAXEER_X_RUNTIME_CLIENT_MANIFEST    runtime client manifest (owner-only)
  PAXEER_X_AGENTD_UPSTREAM_ARTIFACTS  owner-only manifest of the platform services the full mode
                                      reads: layerx-receipt-authority and layerx-program-registry,
                                      each {path, sha256, source_revision, source_paths}, plus the
                                      registry builder environment {path, tree_digest}
"""

import hashlib
import importlib.util
import json
import os
import re
import secrets
import shutil
import stat
import subprocess
from pathlib import Path

ROOT = Path(__file__).resolve().parents[4]
UPSTREAM = ('layerx-receipt-authority', 'layerx-program-registry')


class FixtureRefused(Exception):
    pass


def refuse(reason):
    raise FixtureRefused('agentd fixture refused: ' + reason)


def _runtime_module():
    spec = importlib.util.spec_from_file_location(
        'paxeer_x_runtime_fixture', ROOT / 'tests/daemon/paxeer_x_runtime_fixture.py')
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def _owner_only_file(path, what):
    if not path:
        refuse(what + ' is required')
    path = Path(path)
    try:
        info = path.lstat()
    except FileNotFoundError:
        refuse(what + ' names no file')
    if (not stat.S_ISREG(info.st_mode) or info.st_uid != os.geteuid()
            or stat.S_IMODE(info.st_mode) & 0o077):
        refuse(what + ' must be an owner-only regular file')
    return path


def _git(*argv):
    return subprocess.run(['git', '-C', str(ROOT), *argv], capture_output=True, text=True,
                          check=True).stdout


def _sha256(path):
    with Path(path).open('rb') as stream:
        return hashlib.file_digest(stream, 'sha256').hexdigest()


def upstream_artifacts(path):
    """Validates the platform service artifacts the full mode reads, bound to this source."""
    document = json.loads(_owner_only_file(path, 'PAXEER_X_AGENTD_UPSTREAM_ARTIFACTS').read_text())
    rows = document.get('artifacts', {})
    if set(rows) != set(UPSTREAM):
        refuse('upstream artifact set must be exactly ' + ', '.join(UPSTREAM))
    for name, row in rows.items():
        target = Path(row.get('path', ''))
        if not (target.is_absolute() and target.is_file() and os.access(target, os.X_OK)):
            refuse('missing executable ' + name)
        revision = row.get('source_revision', '')
        if not re.fullmatch('[0-9a-f]{40}', revision) or row.get('sha256') != _sha256(target):
            refuse('executable source/digest mismatch ' + name)
        paths = row.get('source_paths')
        if not (isinstance(paths, list) and paths
                and all(isinstance(p, str) and p and not p.startswith('/') for p in paths)):
            refuse('missing source binding ' + name)
        if _git('diff', '--name-only', revision, 'HEAD', '--', *paths):
            refuse('selected production dependency differs: ' + name)
    builder = document.get('builder_environment', {})
    if not (Path(builder.get('path', '/')).is_absolute() and Path(builder.get('path', '')).is_dir()
            and re.fullmatch('[0-9a-f]{64}', builder.get('tree_digest', ''))):
        refuse('registry builder environment is missing')
    return document


class AgentdFixture:
    """Disposable full-mode agentd configuration over owned real upstream services."""

    def __init__(self, directory):
        self.directory = Path(directory).resolve()
        if self.directory.exists():
            refuse('fixture directory already exists')
        self.directory.mkdir(mode=0o700, parents=True)

    def local_configuration(self):
        """Generates the daemon-owned credentials and durable paths; never printed."""
        d = self.directory
        for name in ('store', 'session-keys', 'run', 'journal'):
            (d / name).mkdir(mode=0o700)
        secret = d / 'session-operator.secret'
        secret.write_bytes(secrets.token_bytes(32))
        secret.chmod(0o600)
        bearers = {secrets.token_hex(32) for _ in range(3)}
        if len(bearers) != 3:
            refuse('generated credentials collided')
        program, node, authority = sorted(bearers)
        return {
            'LAYERX_AGENT_MODE': 'full',
            'LAYERX_AGENT_PROGRAM_BEARER_TOKEN': program,
            'LAYERX_AGENT_NODE_BEARER_TOKEN': node,
            'LAYERX_AGENT_AUTHORITY_BEARER_TOKEN': authority,
            'LAYERX_AGENT_PROGRAM_MAX_STALENESS_MS': '60000',
            'LAYERX_AGENT_DEPLOYMENT_JOURNAL': str(d / 'journal'),
            'LAYERX_AGENT_HUMAN_STORE': str(d / 'store'),
            'LAYERX_AGENT_HUMAN_SOCKET': str(d / 'run' / 'agent.sock'),
            'LAYERX_AGENT_HUMAN_SESSION_KEY_ROOT': str(d / 'session-keys'),
            'LAYERX_AGENT_HUMAN_SESSION_OPERATOR_SECRET_FILE': str(secret),
            'LAYERX_AGENT_HUMAN_SOCKET_UID': str(os.geteuid()),
            'LAYERX_AGENT_HUMAN_SOCKET_GID': str(os.getegid()),
            'LAYERX_AGENT_HUMAN_SOCKET_MODE': '600',
        }

    def start(self):
        """Validates every artifact before launching anything and returns the daemon config."""
        local = self.local_configuration()
        upstream_artifacts(os.environ.get('PAXEER_X_AGENTD_UPSTREAM_ARTIFACTS', ''))
        runtime = _runtime_module()
        try:
            runtime.artifacts(os.environ.get('PAXEER_X_RUNTIME_ARTIFACTS', ''))
            runtime.client_artifact(os.environ.get('PAXEER_X_RUNTIME_CLIENT_MANIFEST', ''))
        except RuntimeError as error:
            refuse(str(error))
        refuse('no qualified driver brings up layerx-receipt-authority and a program-registry '
               'admission against the disposable sequencer; the full-mode program authority '
               'and deployment journal cannot be produced')
        return local

    def cleanup(self):
        shutil.rmtree(self.directory / 'session-keys', ignore_errors=True)
        secret = self.directory / 'session-operator.secret'
        if secret.exists():
            secret.unlink()
