#!/usr/bin/env python3
import csv
import hashlib
import hmac
import io
import shutil
import tarfile
import tempfile
import json
import os
import pathlib
import re
import secrets
import signal
import subprocess
import sys
import time
import uuid

ROOT = pathlib.Path(__file__).resolve().parents[2]


def phase3_restore(docker, ready, port, image, primary, network, password,
                   replication_password, containers, directories):
    recovery_started = time.monotonic()
    store = pathlib.Path(tempfile.mkdtemp(prefix='l07-encrypted-backup-'))
    keys = pathlib.Path(tempfile.mkdtemp(prefix='l07-recovery-key-'))
    directories.extend((store, keys))
    key = keys / 'backup.key'
    key.write_text(secrets.token_hex(32))
    key.chmod(0o600)
    authentication_key = secrets.token_bytes(32)

    def sql(statement):
        return docker('exec', '-i', primary, 'psql', '-X', '-At', '-v',
                      'ON_ERROR_STOP=1', '-U', 'l07_admin', '-d', 'kindle_test', input=statement)

    def crypt(source, destination, decrypt=False):
        command = ['openssl', 'enc', '-aes-256-cbc', '-pbkdf2', '-iter', '200000',
                   '-in', str(source), '-out', str(destination), '-pass', 'file:' + str(key)]
        if decrypt:
            command.append('-d')
        result = subprocess.run(command, capture_output=True, timeout=60)
        if result.returncode:
            raise RuntimeError('backup encryption key unavailable or encrypted payload invalid')
        destination.chmod(0o600)

    def seal(source):
        encrypted = source.with_name(source.name + '.enc')
        crypt(source, encrypted)
        signature = hmac.new(authentication_key, encrypted.read_bytes(), hashlib.sha256).hexdigest()
        source.unlink()
        return encrypted, signature

    def open_sealed(encrypted, signature, destination):
        observed = hmac.new(authentication_key, encrypted.read_bytes(), hashlib.sha256).hexdigest()
        if not hmac.compare_digest(signature, observed):
            raise RuntimeError('encrypted backup/WAL authentication mismatch: ' + encrypted.name)
        crypt(encrypted, destination, True)

    sql('CREATE SCHEMA indexer;CREATE SCHEMA stats;')
    for migration in ('60000.sql', '130000.sql', '80000.sql'):
        sql((ROOT / 'spec/production-system/integration/migrations' / migration).read_text())

    def transaction(number, volume, fee, published):
        transaction_hash = '0x' + f'{number:064x}'
        block_hash = '0x' + f'{100 + number:064x}'
        event = 'recovery-' + str(number)
        state = 'published' if published else 'pending'
        position = str(number * 7) if published else 'NULL'
        published_at = 'clock_timestamp()' if published else 'NULL'
        broker_stream = "'recovery'" if published else 'NULL'
        sql(f"""BEGIN;
INSERT INTO indexer.indexer_raw_logs VALUES(125,'{transaction_hash}',0,{number},0,'{block_hash}','0x{'1' * 40}','[]','\\x01');
INSERT INTO indexer.indexer_ingest_outbox(chain_id,transaction_hash,log_index,event_kind,payload) VALUES(125,'{transaction_hash}',0,'recovery.fixture.v1','{{}}');
INSERT INTO stats.trade_facts VALUES('{event}','recovery-v1','0x{'2' * 40}','0x{'3' * 40}',{number},0,0,1720000000,{volume},{fee},'USDL',6,'retained','buy','{{}}',{number});
INSERT INTO indexer.delivery_outbox(event_id,schema_version,subject,payload_hash,envelope,state,broker_stream,broker_sequence,published_at)
VALUES('{event}',1,'recovery.fixture.v1','{hashlib.sha256(event.encode()).hexdigest()}','\\x01','{state}',{broker_stream},{position},{published_at});
COMMIT;""")

    transaction(2, 100, 2, False)
    docker('exec', '-e', 'PGPASSWORD=' + replication_password, primary,
           'pg_basebackup', '-h', '127.0.0.1', '-U', 'l07_replication', '-D',
           '/tmp/l07-base', '-X', 'none', '-c', 'fast', timeout=60)
    base = store / 'base'
    docker('cp', primary + ':/tmp/l07-base', str(base), timeout=60)
    label = (base / 'backup_label').read_text()
    start_match = re.search(r'START WAL LOCATION: .*\(file ([0-9A-F]{24})\)', label)
    if start_match is None:
        raise RuntimeError('base backup required WAL start is missing')
    start_segment = start_match.group(1)
    archive = store / 'base.tar'
    with tarfile.open(archive, 'w') as payload:
        payload.add(base, arcname='base')
    shutil.rmtree(base)
    encrypted_base, base_signature = seal(archive)
    transaction(1, 9, 1, True)
    source = (ROOT / 'shared/db/production/migrations.go').read_text()
    snapshot_query = source.split('const recoverySnapshotSQL = `', 1)[1].split('`', 1)[0]
    raw = docker('exec', '-i', primary, 'psql', '-X', '--csv', '-t', '-v',
                 'ON_ERROR_STOP=1', '-U', 'l07_admin', '-d', 'kindle_test', input=snapshot_query)
    values = next(csv.reader(io.StringIO(raw)))
    if len(values) != 12 or values[10:] != ['0', '0']:
        raise RuntimeError('pre-target fixture canonical authority invalid')
    expected = dict(canonical_count=int(values[0]), canonical_sha256=hashlib.sha256(values[1].encode()).hexdigest(),
                    trade_count=int(values[2]), volume=values[3], fees=values[4], outbox_count=int(values[5]),
                    outbox_sha256=hashlib.sha256(values[6].encode()).hexdigest(), published=int(values[7]),
                    pending=int(values[8]), broker_sequence=int(values[9]))
    target = sql('SELECT clock_timestamp()').strip()
    sql('SELECT pg_sleep(0.05)')
    transaction(3, 1000, 10, True)
    last_segment = sql("SELECT pg_walfile_name(pg_current_wal_insert_lsn())")
    sql('SELECT pg_switch_wal()')
    archive_deadline = time.monotonic() + 10
    while sql(f"SELECT EXISTS(SELECT 1 FROM pg_ls_dir('/archive') s WHERE s='{last_segment}')") != 't':
        if time.monotonic() >= archive_deadline:
            raise RuntimeError('missing required WAL segment: ' + last_segment)
        time.sleep(0.05)
    wal = store / 'wal'
    wal.mkdir()
    docker('cp', primary + ':/archive/.', str(wal), timeout=60)
    required = sorted(path for path in wal.iterdir() if re.fullmatch(r'[0-9A-F]{24}', path.name))
    if not required or last_segment not in {path.name for path in required}:
        raise RuntimeError('missing required WAL range ending at ' + last_segment)
    if start_segment[:8] != last_segment[:8]:
        raise RuntimeError('backup WAL timeline changed before target')
    first_number = int(start_segment[8:16], 16) * 256 + int(start_segment[16:], 16)
    last_number = int(last_segment[8:16], 16) * 256 + int(last_segment[16:], 16)
    present = {path.name for path in required}
    needed = [start_segment[:8] + f'{number // 256:08X}{number % 256:08X}'
              for number in range(first_number, last_number + 1)]
    missing = [segment for segment in needed if segment not in present]
    if first_number > last_number or missing:
        raise RuntimeError('missing required WAL range: ' + ','.join(missing or [start_segment, last_segment]))
    encrypted_wal = [(path.name, *seal(path)) for path in required]
    restored_base_tar = keys / 'base.tar'
    try:
        open_sealed(encrypted_base, '0' * 64, restored_base_tar)
    except RuntimeError:
        pass
    else:
        raise RuntimeError('unauthenticated encrypted backup accepted')
    open_sealed(encrypted_base, base_signature, restored_base_tar)
    restored_base = keys / 'base'
    with tarfile.open(restored_base_tar) as payload:
        payload.extractall(keys, filter='data')
    restored_base_tar.unlink()
    restored_wal = keys / 'wal'
    restored_wal.mkdir()
    for name, encrypted, signature in encrypted_wal:
        open_sealed(encrypted, signature, restored_wal / name)
    restored = network + '-restored'
    containers.append(restored)
    docker('run', '--detach', '--pull=never', '--name', restored, '--network', network,
           '--memory', '384m', '--cpus', '1', '--pids-limit', '128', '--label', 'kindle.lane=L07',
           '--tmpfs', '/var/lib/postgresql/data:rw', '--tmpfs', '/restore-wal:rw',
           '--publish', '127.0.0.1::5432', image, 'sh', '-c', 'exec sleep 600')
    docker('cp', str(restored_base) + '/.', restored + ':/var/lib/postgresql/data', timeout=60)
    docker('cp', str(restored_wal) + '/.', restored + ':/restore-wal', timeout=60)
    configuration = "\nrestore_command='cp /restore-wal/%f %p'\nrecovery_target_time='" + target + "'\nrecovery_target_action='promote'\narchive_mode=off\n"
    docker('exec', '-i', restored, 'sh', '-c', 'cat >> /var/lib/postgresql/data/postgresql.auto.conf', input=configuration)
    docker('exec', restored, 'sh', '-c', 'touch /var/lib/postgresql/data/recovery.signal; chown -R postgres:postgres /var/lib/postgresql/data /restore-wal; chmod 700 /var/lib/postgresql/data')
    docker('exec', '--detach', restored, 'sh', '-c', 'exec su-exec postgres postgres -D /var/lib/postgresql/data -c shared_buffers=32MB -c max_connections=80')
    ready(restored)
    restored_port = port(restored)
    promoted = docker('exec', restored, 'psql', '-X', '-At', '-U', 'l07_admin', '-d', 'kindle_test', '-c', 'SELECT NOT pg_is_in_recovery()')
    if promoted != 't':
        raise RuntimeError('timestamp recovery did not reach target and promote')
    duration = time.monotonic() - recovery_started
    if duration > 1800:
        raise RuntimeError('restore exceeded thirty-minute recovery limit')
    print(json.dumps({'fixture': 'encrypted_base_backup_wal_timestamp_restore', 'target': target,
                      'requiredWAL': [entry[0] for entry in encrypted_wal], 'restoreSeconds': duration,
                      'encryptedBackupSHA256': hashlib.sha256(encrypted_base.read_bytes()).hexdigest()}), flush=True)
    return dict(L07_PHASE3='1', L07_RESTORE_DSN=f'postgres://l07_admin:{password}@127.0.0.1:{restored_port}/kindle_test?sslmode=disable',
                L07_RECOVERY_TARGET=target, L07_RECOVERY_EXPECTED=json.dumps(expected))


def main():
    if sys.argv[1:] not in ([], ['--phase3']):
        raise RuntimeError('only --phase3 fixture selection is supported')
    phase3 = sys.argv[1:] == ['--phase3']
    directories = []
    image = os.environ.get('L07_POSTGRES_IMAGE', '')
    if not re.fullmatch(r'postgres:16-alpine@sha256:[0-9a-f]{64}', image):
        raise RuntimeError('parent-approved PostgreSQL16 image digest required')
    readers = {}
    for version in ('OLD', 'NEW'):
        path = pathlib.Path(os.environ.get(f'L07_{version}_READER_BIN', ''))
        expected = os.environ.get(f'L07_{version}_READER_SHA256', '')
        if not path.is_absolute() or not path.is_file() or not os.access(path, os.X_OK):
            raise RuntimeError('parent-built independent old/new reader binaries required')
        actual = hashlib.sha256(path.read_bytes()).hexdigest()
        if not re.fullmatch(r'[0-9a-f]{64}', expected) or actual != expected:
            raise RuntimeError('parent-built reader binary digest mismatch')
        readers[version.lower()] = actual
    suffix = uuid.uuid4().hex
    network = 'kindle-l07-phase2-' + suffix
    primary = network + '-primary'
    standby = network + '-standby'
    password = secrets.token_hex(24)
    replication_password = secrets.token_hex(24)
    reader_password = secrets.token_hex(24)
    containers = []
    network_created = False
    process = None
    result = 1

    def docker(*args, input=None, timeout=30):
        completed = subprocess.run(['docker', *args], input=input, text=True,
                                   capture_output=True, timeout=timeout)
        if completed.returncode:
            raise RuntimeError('private PostgreSQL fixture operation failed')
        return completed.stdout.strip()

    def interrupted(signum, _frame):
        raise SystemExit(128 + signum)

    for signum in (signal.SIGINT, signal.SIGTERM):
        signal.signal(signum, interrupted)

    def ready(name):
        deadline = time.monotonic() + 30
        while time.monotonic() < deadline:
            check = subprocess.run(['docker', 'exec', name, 'pg_isready', '-U', 'l07_admin', '-d', 'kindle_test'],
                                   stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, timeout=5)
            if check.returncode == 0:
                return
            time.sleep(.1)
        raise RuntimeError('private PostgreSQL fixture readiness deadline exceeded')

    def port(name):
        binding = docker('port', name, '5432/tcp')
        host, value = binding.rsplit(':', 1)
        if host != '127.0.0.1' or not value.isdecimal() or not 1 <= int(value) <= 65535:
            raise RuntimeError('fixture must publish only one loopback port')
        return value

    try:
        docker('image', 'inspect', image)
        docker('network', 'create', '--internal', network)
        network_created = True
        containers.append(primary)
        docker('run', '--detach', '--pull=never', '--name', primary,
               '--network', network, '--network-alias', 'primary',
               '--memory', '384m', '--cpus', '1', '--pids-limit', '128',
               '--label', 'kindle.lane=L07', '--tmpfs', '/var/lib/postgresql/data:rw',
               '--tmpfs', '/archive:rw',
               '--publish', '127.0.0.1::5432', '-e', 'POSTGRES_USER=l07_admin',
               '-e', 'POSTGRES_PASSWORD=' + password, '-e', 'POSTGRES_DB=kindle_test',
               image, 'postgres', '-c', 'max_connections=80', '-c', 'shared_buffers=32MB',
               '-c', 'wal_level=replica', '-c', 'max_wal_senders=4', '-c', 'synchronous_commit=on',
               '-c', 'archive_mode=on', '-c', 'archive_timeout=60s',
               '-c', 'archive_command=test -f /archive/%f || cp %p /archive/%f')
        docker('exec', primary, 'sh', '-c', 'chown postgres:postgres /archive')
        ready(primary)
        setup = f"CREATE ROLE l07_replication LOGIN REPLICATION NOSUPERUSER NOCREATEDB NOCREATEROLE PASSWORD '{replication_password}';\n"
        docker('exec', '-i', primary, 'psql', '-X', '-v', 'ON_ERROR_STOP=1', '-U', 'l07_admin', '-d', 'kindle_test', input=setup)
        docker('exec', '-i', primary, 'sh', '-c', 'cat >> "$PGDATA/pg_hba.conf"',
               input='host replication l07_replication all scram-sha-256\n')
        docker('exec', primary, 'psql', '-X', '-v', 'ON_ERROR_STOP=1', '-U', 'l07_admin', '-d', 'kindle_test', '-c', 'SELECT pg_reload_conf()')
        containers.append(standby)
        startup = '''set -eu
mkdir -p "$PGDATA"
chown postgres:postgres "$PGDATA"
chmod 700 "$PGDATA"
su-exec postgres pg_basebackup -h primary -U l07_replication -D "$PGDATA" -R -X stream
printf 'primary:5432:*:l07_replication:%s\n' "$PGPASSWORD" > "$PGDATA/replication.pass"
chown postgres:postgres "$PGDATA/replication.pass"
chmod 600 "$PGDATA/replication.pass"
exec su-exec postgres postgres -D "$PGDATA" -c hot_standby=on -c shared_buffers=32MB -c max_connections=80 -c 'primary_conninfo=host=primary user=l07_replication passfile=/var/lib/postgresql/data/replication.pass' 
'''
        docker('run', '--detach', '--pull=never', '--name', standby, '--network', network,
               '--memory', '384m', '--cpus', '1', '--pids-limit', '128',
               '--label', 'kindle.lane=L07', '--tmpfs', '/var/lib/postgresql/data:rw',
               '--publish', '127.0.0.1::5432', '-e', 'PGPASSWORD=' + replication_password,
               '-e', 'PGDATA=/var/lib/postgresql/data', image, 'sh', '-c', startup)
        ready(standby)
        primary_port, standby_port = port(primary), port(standby)
        version = docker('exec', primary, 'postgres', '--version')
        if not version.startswith('postgres (PostgreSQL) 16.'):
            raise RuntimeError('fixture database major differs from PostgreSQL16')
        recovery = docker('exec', standby, 'psql', '-X', '-At', '-U', 'l07_admin', '-d', 'kindle_test', '-c', 'SELECT pg_is_in_recovery()')
        if recovery != 't':
            raise RuntimeError('fixture replica is not a real physical standby')
        env = dict(os.environ, GOWORK='off', GOMAXPROCS='2', GOFLAGS='-mod=readonly',
                   L07_POSTGRES_DSN=f'postgres://l07_admin:{password}@127.0.0.1:{primary_port}/kindle_test?sslmode=disable',
                   L07_REPLICA_DSN=f'postgres://l07_admin:{password}@127.0.0.1:{standby_port}/kindle_test?sslmode=disable',
                   L07_READER_PASSWORD=reader_password)
        print(json.dumps({'fixture': 'real_primary_physical_standby', 'image': image,
                          'postgresVersion': version, 'readerBinarySHA256': readers}), flush=True)
        if phase3:
            env.update(phase3_restore(docker, ready, port, image, primary, network, password,
                                     replication_password, containers, directories))
        process = subprocess.Popen(['go', 'test', '-p', '1', '-count=1', '-json', './db', './db/production'],
                                   cwd=ROOT / 'shared', env=env, start_new_session=True)
        result = process.wait(timeout=600)
    finally:
        if process is not None and process.poll() is None:
            os.killpg(process.pid, signal.SIGTERM)
            try:
                process.wait(timeout=5)
            except subprocess.TimeoutExpired:
                os.killpg(process.pid, signal.SIGKILL)
                process.wait(timeout=5)
        cleanup_failed = False
        for name in reversed(containers):
            try:
                docker('rm', '--force', name)
            except (RuntimeError, subprocess.SubprocessError, OSError):
                cleanup_failed = True
        if network_created:
            try:
                docker('network', 'rm', network)
            except (RuntimeError, subprocess.SubprocessError, OSError):
                cleanup_failed = True
        for directory in directories:
            shutil.rmtree(directory)
        if cleanup_failed:
            raise RuntimeError('private PostgreSQL fixture cleanup failed')
    return result


if __name__ == '__main__':
    try:
        sys.exit(main())
    except subprocess.TimeoutExpired:
        print('L07 phase2 bounded fixture timed out', file=sys.stderr)
        sys.exit(124)
    except (RuntimeError, OSError) as failure:
        print('L07 phase2: ' + str(failure), file=sys.stderr)
        sys.exit(1)
