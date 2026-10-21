from __future__ import annotations

import base64
import hashlib
import json
import os
from pathlib import Path
import re
import shlex
import shutil
import stat
import subprocess
import tempfile
from urllib.parse import urlsplit


class Unavailable(RuntimeError):
    pass


class ProviderFailure(RuntimeError):
    def __init__(self, operation, exit_code=None):
        self.operation = operation
        self.exit_code = exit_code
        super().__init__(operation + ' failed; provider output withheld')


def regular(path, maximum=None, private=False):
    path = Path(path)
    if not path.is_absolute():
        raise ValueError('absolute provider path required')
    for part in (path, *path.parents):
        if part.is_symlink():
            raise ValueError('symlink provider path rejected')
    info = path.stat()
    if not stat.S_ISREG(info.st_mode) or (maximum is not None and info.st_size > maximum):
        raise ValueError('bounded regular provider file required')
    if private and (info.st_mode & 0o077 or info.st_uid != os.getuid()):
        raise ValueError('provider credential must be owned and mode0600')
    return path


def file_sha(path):
    digest = hashlib.sha256()
    with regular(path).open('rb') as stream:
        while chunk := stream.read(1024 * 1024):
            digest.update(chunk)
    return digest.hexdigest()


def identifier(value):
    if not isinstance(value, str) or not re.fullmatch(r'[a-z][a-z0-9_]{0,62}', value):
        raise ValueError('invalid SQL identifier')
    return '"' + value + '"'


def literal(value):
    if not isinstance(value, str) or len(value) > 1024 * 1024 or '\x00' in value:
        raise ValueError('invalid SQL value')
    return "'" + value.replace("'", "''") + "'"


class BoundTools:
    def __init__(self, tools):
        if not isinstance(tools, dict) or not tools:
            raise Unavailable('pinned executable inventory unavailable')
        self.tools = tools

    def run(self, tool, args, *, env=None, data=None, timeout=120):
        binding = self.tools.get(tool)
        if not isinstance(binding, dict) or set(binding) != {'path', 'sha256'}:
            raise Unavailable('pinned executable unavailable: ' + tool)
        path = regular(binding['path'])
        if file_sha(path) != binding['sha256'] or not os.access(path, os.X_OK):
            raise Unavailable('executable identity changed: ' + tool)
        if not 0 < timeout <= 1800 or not all(isinstance(a, str) and '\x00' not in a for a in args):
            raise ValueError('invalid bounded provider invocation')
        child_env = {'PATH': '/usr/bin:/bin', 'LANG': 'C.UTF-8'}
        if env:
            child_env.update(env)
        with tempfile.TemporaryFile() as stdout, tempfile.TemporaryFile() as stderr:
            try:
                result = subprocess.run([str(path), *args], input=data, env=child_env,
                                        stdout=stdout, stderr=stderr, timeout=timeout, check=False)
            except (OSError, subprocess.TimeoutExpired) as exc:
                raise ProviderFailure(tool) from exc
            if result.returncode:
                raise ProviderFailure(tool, result.returncode)
            if stdout.tell() > 2 * 1024 * 1024:
                raise ProviderFailure(tool + ': oversized response')
            stdout.seek(0)
            return stdout.read()

    def denied(self, args, *, env, expected_error, timeout=120):
        """Only an explicit AWS authorization error qualifies as a denial."""
        binding = self.tools.get('aws')
        if not isinstance(binding, dict) or set(binding) != {'path', 'sha256'}:
            raise Unavailable('pinned AWS executable unavailable')
        path = regular(binding['path'])
        if file_sha(path) != binding['sha256'] or not os.access(path, os.X_OK):
            raise Unavailable('AWS executable identity changed')
        if expected_error not in {'AccessDenied', 'AccessDeniedException'} or not 0 < timeout <= 120:
            raise ValueError('exact bounded authorization denial required')
        if not isinstance(args, list) or not all(isinstance(a, str) and '\x00' not in a for a in args):
            raise ValueError('invalid denial invocation')
        child_env = {'PATH': '/usr/bin:/bin', 'LANG': 'C.UTF-8', **env}
        with tempfile.TemporaryFile() as stdout, tempfile.TemporaryFile() as stderr:
            try:
                result = subprocess.run([str(path), *args], env=child_env, stdout=stdout, stderr=stderr,
                                        timeout=timeout, check=False)
            except (OSError, subprocess.TimeoutExpired) as exc:
                raise ProviderFailure('authorization probe unavailable') from exc
            if result.returncode == 0 or stderr.tell() > 65536 or stdout.tell() != 0:
                raise ProviderFailure('expected authorization denial', result.returncode)
            stderr.seek(0)
            response = stderr.read().decode('utf-8', errors='strict')
            if not re.search(r'An error occurred \(' + re.escape(expected_error) + r'\) when calling ', response):
                raise ProviderFailure('non-authorization provider failure', result.returncode)
            return {'error': expected_error, 'exit_code': result.returncode, 'plaintext_bytes': 0}


class NativeRehearsalRunner:
    """Run admitted, digest-pinned isolated provider scenario executables once."""
    def __init__(self, configuration, expected_sha256, identity):
        import sys
        sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
        import recovery
        raw = regular(configuration, recovery.MAX_JSON, private=True).read_bytes()
        recovery.digest(expected_sha256)
        recovery.require(hashlib.sha256(raw).hexdigest() == expected_sha256, 'rehearsal authority changed')
        config = recovery.load(raw)
        recovery.exact(config, {'version', 'realm', 'identity', 'tools', 'plans', 'output_directory'})
        recovery.require(type(config['version']) is int and config['version'] == 3 and
                         config['realm'] == 'isolated-recovery-qualification' and
                         config['identity'] == identity.validate().diagnostic(), 'isolated rehearsal identity required')
        recovery.exact(config['plans'], recovery.METRICS)
        self.root = Path(config['output_directory'])
        recovery.require(self.root.is_absolute() and self.root.resolve(strict=True) == self.root and
                         self.root.is_dir() and self.root.stat().st_mode & 0o077 == 0, 'private owned rehearsal output required')
        self.tools = BoundTools(config['tools'])
        self.config, self.identity, self.recovery = config, identity, recovery

    def run(self, requirement):
        import time
        recovery = self.recovery
        recovery.require(requirement in recovery.METRICS, 'unknown rehearsal requirement')
        plan = self.config['plans'][requirement]
        recovery.exact(plan, {'tool', 'configuration', 'configuration_sha256', 'inputs_sha256', 'timeout_seconds'})
        recovery.digest(plan['configuration_sha256']); recovery.digest(plan['inputs_sha256'])
        configuration = regular(plan['configuration'], recovery.MAX_JSON, private=True)
        recovery.require(file_sha(configuration) == plan['configuration_sha256'], 'provider scenario configuration changed')
        recovery.integer(plan['timeout_seconds'], 1, 1800)
        recovery.require(plan['tool'] in self.tools.tools, 'admitted native scenario executable unavailable')
        binding = self.tools.tools[plan['tool']]
        recovery.digest(binding['sha256']); recovery.require(binding['sha256'] != '0'*64, 'native scenario tool identity required')
        args = ['--realm', self.config['realm'], '--configuration', str(configuration),
                '--configuration-sha256', plan['configuration_sha256'], '--requirement', requirement,
                '--source-sha256', self.identity.source_sha256, '--interface-sha256', self.identity.interface_sha256,
                '--deployment-sha256', self.identity.deployment_sha256, '--inventory-sha256', self.identity.inventory_sha256,
                '--inputs-sha256', plan['inputs_sha256']]
        path = self.root / (requirement + '.json')
        fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
        started = int(time.time())
        try:
            failure = None
            try:
                raw = self.tools.run(plan['tool'], args, timeout=plan['timeout_seconds'])
                result = recovery.load(raw)
                recovery.adversarial_scenario(requirement, result)
            except (ValueError, OSError, Unavailable, ProviderFailure) as exc:
                failure = exc
                result = {'error_type': type(exc).__name__}
            native = dict(version=3, realm=self.config['realm'], requirement=requirement,
                          identity=self.identity.diagnostic(), inputs_sha256=plan['inputs_sha256'],
                          started_unix=started, ended_unix=int(time.time()), tool_sha256=binding['sha256'],
                          exit_code=0 if failure is None else 1, result=result)
            with os.fdopen(fd, 'wb') as stream:
                fd = None
                stream.write(recovery.canonical(native)); stream.flush(); os.fsync(stream.fileno())
            if failure is not None:
                raise failure
            return {'path': path.name, 'sha256': file_sha(path), 'inputs_sha256': plan['inputs_sha256']}
        finally:
            if fd is not None:
                os.close(fd)


class BackupAccessRehearsal:
    def __init__(self, application_archive, authoritative_archive, *, principal_arn):
        if not isinstance(application_archive, S3Archive) or not isinstance(authoritative_archive, S3Archive):
            raise ValueError('actual isolated application and backup providers required')
        if not re.fullmatch(r'arn:aws(?:-us-gov|-cn)?:(?:iam|sts)::[0-9]{12}:(?:role|user|assumed-role)/[A-Za-z0-9+=,.@_/-]+', principal_arn):
            raise ValueError('exact requesting IAM principal required')
        if application_archive.environment == authoritative_archive.environment:
            raise ValueError('independent application and backup credentials required')
        if (application_archive.endpoint, application_archive.region, application_archive.bucket) != (
                authoritative_archive.endpoint, authoritative_archive.region, authoritative_archive.bucket):
            raise ValueError('denial probe must target the authoritative backup bucket')
        self.application, self.authority, self.principal = application_archive, authoritative_archive, principal_arn

    def attempt(self, obj, ciphertext, other_key_arn):
        app = self.application
        caller = json.loads(app.tools.run('aws', ['--output', 'json', '--no-cli-pager', 'sts', 'get-caller-identity'], env=app.environment))
        actual = caller.get('Arn')
        if actual != self.principal:
            # STS assumed-role identities are not guessed or silently mapped to IAM roles.
            raise ProviderFailure('application principal identity')
        self.authority.head(obj['key'], obj['version_id'], obj['sha256'], obj['size'])
        safe_key(obj['key']); regular(ciphertext, 65536, private=True)
        if not re.fullmatch(r'arn:(aws|aws-us-gov|aws-cn):kms:[a-z0-9-]+:[0-9]{12}:key/[a-zA-Z0-9-]+', other_key_arn) or other_key_arn == app.kms_key_arn:
            raise ValueError('independent other-environment KMS key required')
        deletion = app.tools.denied(['--endpoint-url', app.endpoint, '--region', app.region, '--output', 'json',
                                   '--no-cli-pager', 's3api', 'delete-object', '--bucket', app.bucket,
                                   '--key', obj['key'], '--version-id', obj['version_id']],
                                  env=app.environment, expected_error='AccessDenied')
        decryption = app.tools.denied(['--region', app.region, '--output', 'json', '--no-cli-pager', 'kms', 'decrypt',
                                     '--key-id', other_key_arn, '--ciphertext-blob', 'fileb://' + str(ciphertext)],
                                    env=app.environment, expected_error='AccessDeniedException')
        self.authority.head(obj['key'], obj['version_id'], obj['sha256'], obj['size'])
        return {'principal_arn': actual, 'delete_error': deletion['error'], 'decrypt_error': decryption['error'],
                'object_before_sha256': obj['sha256'], 'object_after_sha256': obj['sha256'], 'plaintext_bytes': 0,
                'provider_audit_required': True}


class PostgreSQL:
    def __init__(self, tools, service_file, service, expected_role):
        self.tools = tools
        self.service_file = regular(service_file, 65536, private=True)
        if not re.fullmatch(r'[a-z][a-z0-9-]{1,62}', service):
            raise ValueError('invalid PostgreSQL service')
        identifier(expected_role)
        self.service = service
        self.role = expected_role
        self.environment = {'PGSERVICEFILE': str(self.service_file), 'PGSERVICE': service,
                            'PGCONNECT_TIMEOUT': '5', 'PGOPTIONS': '-c statement_timeout=20000 -c lock_timeout=2000'}

    def query(self, sql, *, timeout=30):
        auth = ('DO $identity$ BEGIN IF session_user <> ' + literal(self.role) +
                ' OR current_user <> session_user THEN RAISE EXCEPTION ' +
                "'recovery coordinator identity mismatch'; END IF; END $identity$;\n")
        return self.tools.run('psql', ['-X', '--no-password', '-qAt', '-v', 'ON_ERROR_STOP=1'],
                              env=self.environment, data=(auth + sql).encode(), timeout=timeout).decode().strip()

    def document(self, sql):
        value = self.query(sql)
        if not value:
            raise ProviderFailure('PostgreSQL missing result')
        return json.loads(value)

    def physical_backup(self, destination):
        destination = Path(destination)
        if not destination.is_absolute() or destination.is_symlink() or destination.exists():
            raise ValueError('fresh isolated backup destination required')
        parent = destination.parent.resolve(strict=True)
        if parent != destination.parent or not parent.is_dir():
            raise ValueError('owned backup parent required')
        destination.mkdir(mode=0o700)
        self.tools.run('pg_basebackup', ['--format=plain', '--wal-method=stream', '--checkpoint=spread',
                       '--manifest-checksums=SHA256', '--no-password', '--pgdata=' + str(destination)],
                       env=self.environment, timeout=1800)
        self.tools.run('pg_verifybackup', ['--exit-on-error', str(destination)], timeout=300)
        backup = regular(destination / 'backup_manifest', 16 * 1024 * 1024)
        return {'directory': str(destination), 'manifest_sha256': file_sha(backup)}

    def restore_physical(self, base, destination, *, target_time, timeline, port, wal_command,
                         expected_system_id, expected_manifest_sha256):
        from datetime import datetime
        base, destination = Path(base), Path(destination)
        if (not base.is_absolute() or base.is_symlink() or not base.is_dir() or
                not destination.is_absolute() or destination.exists() or destination.is_symlink() or
                destination.parent.resolve(strict=True) != destination.parent):
            raise ValueError('isolated physical restore directories required')
        if not re.fullmatch(r'[1-9][0-9]{0,19}', expected_system_id):
            raise ValueError('PostgreSQL system identity required')
        if type(port) is not int or not 1024 <= port <= 65535 or type(timeline) is not int or not 1 <= timeline <= 2**32 - 1:
            raise ValueError('explicit owned port and timeline required')
        target = datetime.fromisoformat(target_time.replace('Z', '+00:00'))
        if target.tzinfo is None:
            raise ValueError('absolute recovery timestamp required')
        if not isinstance(wal_command, list) or not wal_command or len(wal_command) > 16:
            raise ValueError('pinned WAL restore invocation required')
        binding = self.tools.tools.get(wal_command[0])
        if not binding or file_sha(binding['path']) != binding['sha256']:
            raise Unavailable('pinned WAL recovery tool unavailable')
        if any(not isinstance(a, str) or '\x00' in a or '%' in a for a in wal_command):
            raise ValueError('unsafe WAL recovery invocation')
        if file_sha(base / 'backup_manifest') != expected_manifest_sha256:
            raise ProviderFailure('physical backup manifest changed')
        for item in base.rglob('*'):
            if item.is_symlink() or (not item.is_file() and not item.is_dir()):
                raise ValueError('physical restore base contains unowned links or special files')
        self.tools.run('pg_verifybackup', ['--exit-on-error', str(base)], timeout=300)
        control = self.tools.run('pg_controldata', [str(base)]).decode()
        match = re.search(r'^Database system identifier:\s+([0-9]+)\s*$', control, re.M)
        if not match or match.group(1) != expected_system_id:
            raise ProviderFailure('physical backup system identity')
        shutil.copytree(base, destination)
        os.chmod(destination, 0o700)
        for stale in ('postmaster.pid', 'standby.signal', 'recovery.signal'):
            candidate = destination / stale
            if candidate.exists():
                candidate.unlink()
        socket_dir = destination / 'owned-socket'
        socket_dir.mkdir(mode=0o700)
        command = ' '.join(shlex.quote(a) for a in [binding['path'], *wal_command[1:]]) + ' %f %p'
        quote = lambda value: "'" + value.replace("'", "''") + "'"
        configuration = {'listen_addresses': "''", 'port': str(port),
                         'unix_socket_directories': quote(str(socket_dir)), 'archive_mode': 'off',
                         'hot_standby': 'on', 'restore_command': quote(command),
                         'recovery_target_time': quote(target.isoformat()), 'recovery_target_timeline': str(timeline),
                         'recovery_target_inclusive': 'on', 'recovery_target_action': "'pause'"}
        (destination / 'postgresql.conf').write_text('\n'.join(k + '=' + v for k, v in configuration.items()) + '\n')
        (destination / 'postgresql.auto.conf').write_text('')
        (destination / 'pg_hba.conf').write_text('local all all peer\n')
        (destination / 'recovery.signal').touch(mode=0o600)
        self.tools.run('pg_ctl', ['--pgdata=' + str(destination), '--wait', '--timeout=60',
                                 '--log=' + str(destination / 'recovery.log'), 'start'], timeout=90)
        return {'directory': str(destination), 'socket_directory': str(socket_dir), 'port': port,
                'system_identifier': expected_system_id, 'target_time': target.isoformat(),
                'timeline': timeline, 'recovery_target_action': 'pause', 'traffic_cutover_allowed': False}


class S3Archive:
    def __init__(self, tools, *, endpoint, region, bucket, profile, credentials_file, kms_key_arn):
        parsed = urlsplit(endpoint)
        if parsed.scheme != 'https' or not parsed.hostname or parsed.username or parsed.password or parsed.query or parsed.fragment or parsed.path not in ('', '/'):
            raise ValueError('explicit HTTPS archive endpoint required')
        for item in (region, bucket, profile):
            if not isinstance(item, str) or not re.fullmatch(r'[a-z0-9][a-z0-9.-]{1,127}', item):
                raise ValueError('invalid archive resource identity')
        if not re.fullmatch(r'arn:(aws|aws-us-gov|aws-cn):kms:[a-z0-9-]+:[0-9]{12}:key/[a-zA-Z0-9-]+', kms_key_arn):
            raise ValueError('exact KMS key ARN required')
        self.tools, self.endpoint, self.region, self.bucket = tools, endpoint, region, bucket
        self.profile, self.kms_key_arn = profile, kms_key_arn
        self.environment = {'AWS_SHARED_CREDENTIALS_FILE': str(regular(credentials_file, 65536, private=True)),
                            'AWS_PROFILE': profile, 'AWS_EC2_METADATA_DISABLED': 'true',
                            'AWS_PAGER': '', 'AWS_CONFIG_FILE': '/dev/null'}

    def call(self, operation, args):
        data = self.tools.run('aws', ['--endpoint-url', self.endpoint, '--region', self.region,
                             '--output', 'json', '--no-cli-pager', 's3api', operation,
                             '--bucket', self.bucket, *args], env=self.environment, timeout=120)
        return json.loads(data or b'{}')

    def verify_policy(self):
        version = self.call('get-bucket-versioning', [])
        locking = self.call('get-object-lock-configuration', []).get('ObjectLockConfiguration', {})
        encryption = self.call('get-bucket-encryption', []).get('ServerSideEncryptionConfiguration', {}).get('Rules', [])
        if version.get('Status') != 'Enabled' or locking.get('ObjectLockEnabled') != 'Enabled':
            raise ProviderFailure('immutable versioned archive policy')
        if not any(r.get('ApplyServerSideEncryptionByDefault', {}).get('SSEAlgorithm') == 'aws:kms' and
                   r.get('ApplyServerSideEncryptionByDefault', {}).get('KMSMasterKeyID') == self.kms_key_arn for r in encryption):
            raise ProviderFailure('independent KMS archive encryption')
        return {'versioning': 'Enabled', 'object_lock': 'Enabled', 'kms_key_arn': self.kms_key_arn,
                'region': self.region, 'bucket': self.bucket}

    def upload(self, source, key, retain_until, *, kind):
        from datetime import datetime, timedelta, timezone
        source = regular(source)
        safe_key(key)
        retained = datetime.fromisoformat(retain_until.replace('Z', '+00:00'))
        retention_days = {'wal': 35, 'daily-base': 35, 'monthly-base': 365, 'raw-chain': 365,
                          'metadata': 35, 'media': 35}
        if kind not in retention_days or retained.tzinfo is None or retained < datetime.now(timezone.utc) + timedelta(days=retention_days[kind]):
            raise ValueError('reviewed immutable retention horizon required')
        self.verify_policy()
        checksum = file_sha(source)
        result = self.call('put-object', ['--key', key, '--body', str(source), '--if-none-match', '*',
                           '--checksum-sha256', base64.b64encode(bytes.fromhex(checksum)).decode(),
                           '--server-side-encryption', 'aws:kms', '--ssekms-key-id', self.kms_key_arn,
                           '--object-lock-mode', 'COMPLIANCE', '--object-lock-retain-until-date', retain_until,
                           '--metadata', 'sha256=' + checksum])
        version = result.get('VersionId')
        if not isinstance(version, str) or not version or version == 'null':
            raise ProviderFailure('durable immutable object version acknowledgment')
        acknowledged = self.head(key, version, checksum, source.stat().st_size)
        retained_until = acknowledged.get('ObjectLockRetainUntilDate')
        if not isinstance(retained_until, str) or datetime.fromisoformat(retained_until.replace('Z', '+00:00')) < retained:
            raise ProviderFailure('durable object retention acknowledgment')
        return {'key': key, 'version_id': version, 'sha256': checksum, 'size': source.stat().st_size}

    def head(self, key, version, checksum, size):
        safe_key(key)
        if not isinstance(version, str) or not 1 <= len(version) <= 1024 or '\x00' in version:
            raise ValueError('pinned version required')
        metadata = self.call('head-object', ['--key', key, '--version-id', version, '--checksum-mode', 'ENABLED'])
        actual = metadata.get('ChecksumSHA256')
        expected = base64.b64encode(bytes.fromhex(checksum)).decode()
        if (metadata.get('VersionId') != version or metadata.get('ContentLength') != size or
                metadata.get('Metadata', {}).get('sha256') != checksum or actual != expected or
                metadata.get('ServerSideEncryption') != 'aws:kms' or metadata.get('SSEKMSKeyId') != self.kms_key_arn or
                metadata.get('ObjectLockMode') != 'COMPLIANCE'):
            raise ProviderFailure('pinned encrypted object identity')
        return metadata

    def download(self, obj, destination):
        self.head(obj['key'], obj['version_id'], obj['sha256'], obj['size'])
        destination = Path(destination)
        if not destination.is_absolute() or destination.exists() or destination.is_symlink():
            raise ValueError('fresh owned download destination required')
        fd = os.open(destination, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
        os.close(fd)
        self.call('get-object', ['--key', obj['key'], '--version-id', obj['version_id'],
                  '--checksum-mode', 'ENABLED', str(destination)])
        if destination.stat().st_size != obj['size'] or file_sha(destination) != obj['sha256']:
            raise ProviderFailure('archive object checksum: ' + obj['key'])
        return destination


def safe_key(key):
    if not isinstance(key, str) or not 1 <= len(key) <= 512 or not re.fullmatch(r'[a-zA-Z0-9._/-]+', key):
        raise ValueError('invalid archive object key')
    if key.startswith('/') or any(p in ('', '.', '..') for p in key.split('/')):
        raise ValueError('unsafe archive object key')
    return key


def main():
    import argparse
    import sys
    parser = argparse.ArgumentParser()
    parser.add_argument('operation', choices=('wal-restore', 'archive-file', 'verify-objects', 'base-backup', 'rehearse'))
    parser.add_argument('--configuration', required=True)
    parser.add_argument('--configuration-sha256', required=True)
    parser.add_argument('--source-sha256', required=True)
    parser.add_argument('--source')
    parser.add_argument('--key')
    parser.add_argument('--retain-until')
    parser.add_argument('--kind', choices=('wal', 'daily-base', 'monthly-base', 'raw-chain', 'metadata', 'media'))
    parser.add_argument('--inventory')
    parser.add_argument('--identity')
    parser.add_argument('--identity-sha256')
    parser.add_argument('--requirement')
    parser.add_argument('wal', nargs='?')
    parser.add_argument('destination', nargs='?')
    args = parser.parse_args()
    if file_sha(Path(__file__).absolute()) != args.source_sha256:
        raise Unavailable('recovery provider source changed')
    if args.operation == 'rehearse':
        from render import load_identity
        if args.identity is None or args.identity_sha256 is None or args.requirement is None:
            raise ValueError('admitted identity and requirement required')
        identity = load_identity(args.identity, args.identity_sha256)
        runner = NativeRehearsalRunner(args.configuration, args.configuration_sha256, identity)
        print(json.dumps(runner.run(args.requirement), sort_keys=True))
        return
    raw = regular(args.configuration, 2 * 1024 * 1024, private=True).read_bytes()
    if hashlib.sha256(raw).hexdigest() != args.configuration_sha256:
        raise Unavailable('recovery provider configuration changed')
    sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
    import recovery
    config = recovery.load(raw)
    recovery.exact(config, {'version', 'tools', 'archive', 'wal_inventory', 'owned_directory', 'postgres'})
    recovery.require(type(config['version']) is int and config['version'] == 2, 'provider version2 required')
    tools = BoundTools(config['tools'])
    archive = S3Archive(tools, **config['archive'])
    owned = Path(config['owned_directory'])
    recovery.require(owned.is_absolute() and owned.is_dir() and owned.resolve() == owned,
                     'isolated owned provider directory required')
    def destination(value):
        recovery.require(isinstance(value, str), 'provider destination required')
        result = Path(value)
        if not result.is_absolute():
            result = owned / result
        recovery.require(result.parent.resolve().is_relative_to(owned) and not result.is_symlink(),
                         'provider output outside isolated directory')
        return result
    if args.operation == 'wal-restore':
        recovery.require(isinstance(args.wal, str) and re.fullmatch(r'(?:[0-9A-F]{24}|[0-9A-F]{8}\.history)', args.wal),
                         'exact WAL archive name required')
        inventory = config['wal_inventory']
        recovery.require(isinstance(inventory, dict) and args.wal in inventory, 'missing required WAL archive object')
        archive.download(inventory[args.wal], destination(args.destination))
        return
    if args.operation == 'archive-file':
        recovery.require(args.source is not None and args.key is not None and args.retain_until is not None and args.kind is not None,
                         'archive source/key/retention required')
        result = archive.upload(regular(args.source), args.key, args.retain_until, kind=args.kind)
    elif args.operation == 'verify-objects':
        recovery.require(args.inventory is not None, 'object inventory required')
        inventory = recovery.load(regular(args.inventory, recovery.MAX_JSON).read_bytes())
        recovery.require(isinstance(inventory, list) and 1 <= len(inventory) <= 10000, 'bounded object inventory required')
        for obj in inventory:
            archive.head(obj['key'], obj['version_id'], obj['sha256'], obj['size'])
        result = {'objects_verified': len(inventory), 'bucket': archive.bucket, 'region': archive.region}
    else:
        pg = PostgreSQL(tools, **config['postgres'])
        result = pg.physical_backup(destination(args.destination))
    print(json.dumps(result, sort_keys=True))


if __name__ == '__main__':
    try:
        main()
    except (ValueError, OSError, Unavailable, ProviderFailure):
        raise SystemExit(1)
