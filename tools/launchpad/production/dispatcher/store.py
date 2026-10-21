"""Transactional, restart-fenced local dispatcher resource ledger."""
import hashlib
import json
import math
import re
import sqlite3
import socket
import os
import stat
import time
import uuid
from pathlib import Path
from contextlib import contextmanager


class StoreError(ValueError):
    pass


CLASSES = ('cpu', 'memory', 'pids', 'disk', 'executor', 'go_build', 'container_start')
BINDINGS = ('baseline', 'candidate', 'interface', 'layout')


class Store:
    def __init__(self, db_path, capacity: dict, boot_id=None, mode="ai", total_slots=31, host_boot_id=None):
        self.capacity = self._resources(capacity, complete=True)
        if mode not in ('ai', 'process-harness') or isinstance(total_slots, bool) or not isinstance(total_slots, int) or total_slots < 2:
            raise StoreError('invalid execution mode or total slots')
        if mode == 'ai' and self.capacity['executor'] > total_slots - 1:
            raise StoreError('AI capacity must reserve one coordinator slot')
        self.mode = mode
        self.total_slots = total_slots
        if self.capacity['executor'] < 1:
            raise StoreError('executor capacity must be positive')
        self.boot_id = boot_id or str(uuid.uuid4())
        self.host_boot_id = host_boot_id if host_boot_id is not None else Path('/proc/sys/kernel/random/boot_id').read_text().strip()
        if not isinstance(self.host_boot_id, str) or not self.host_boot_id.strip():
            raise StoreError('host boot identity required')
        self.db = sqlite3.connect(str(db_path), timeout=30, isolation_level=None)
        self.db.row_factory = sqlite3.Row
        self.db.execute('PRAGMA journal_mode=WAL')
        self.db.execute('PRAGMA busy_timeout=30000')
        self.db.executescript('''
        CREATE TABLE IF NOT EXISTS meta (key TEXT PRIMARY KEY, value TEXT NOT NULL);
        CREATE TABLE IF NOT EXISTS counters (task TEXT PRIMARY KEY, fence INTEGER NOT NULL);
        CREATE TABLE IF NOT EXISTS accepted (attempt TEXT PRIMARY KEY, record TEXT NOT NULL);
        CREATE TABLE IF NOT EXISTS leases (
          task TEXT PRIMARY KEY, lane TEXT NOT NULL, owner TEXT NOT NULL,
          attempt TEXT NOT NULL, fence INTEGER NOT NULL, boot TEXT NOT NULL,
          bindings TEXT NOT NULL, resources TEXT NOT NULL, expires REAL NOT NULL,
          state TEXT NOT NULL, allocation TEXT NOT NULL);
        CREATE UNIQUE INDEX IF NOT EXISTS live_lane ON leases(lane) WHERE state='active';
        ''')
        with self._transaction():
            previous = self.db.execute("SELECT value FROM meta WHERE key='boot'").fetchone()
            previous_host = self.db.execute("SELECT value FROM meta WHERE key='host_boot'").fetchone()
            same_boot = bool(previous and previous['value'] == self.boot_id and previous_host and previous_host['value'] == self.host_boot_id)
            if not same_boot:
                self.db.execute("UPDATE leases SET state='invalidated' WHERE state='active'")
            self.db.execute("INSERT OR REPLACE INTO meta VALUES ('boot',?)", (self.boot_id,))
            self.db.execute("INSERT OR REPLACE INTO meta VALUES ('host_boot',?)", (self.host_boot_id,))
            encoded = json.dumps({'capacity': self.capacity, 'mode': self.mode, 'total_slots': self.total_slots}, sort_keys=True)
            existing = self.db.execute("SELECT value FROM meta WHERE key='capacity'").fetchone()
            if same_boot and existing and existing['value'] != encoded:
                raise StoreError('capacity disagreement within boot')
            self.db.execute("INSERT OR REPLACE INTO meta VALUES ('capacity',?)", (encoded,))

    @staticmethod
    def _resources(value, complete=False):
        if not isinstance(value, dict) or set(value) - set(CLASSES):
            raise StoreError('unknown resource class')
        if complete and set(value) != set(CLASSES):
            raise StoreError('all seven capacity classes required')
        result = {}
        for key in CLASSES:
            number = value.get(key, 0)
            if isinstance(number, bool) or not isinstance(number, (int, float)) or not math.isfinite(number) or number < 0:
                raise StoreError('resources must be finite nonnegative numbers')
            if key in ('pids', 'executor', 'go_build', 'container_start') and int(number) != number:
                raise StoreError('discrete resources must be integral')
            result[key] = number
        return result

    @staticmethod
    def _ttl(ttl):
        if isinstance(ttl, bool) or not isinstance(ttl, (int, float)) or not math.isfinite(ttl) or not 0 < ttl <= 3600:
            raise StoreError('TTL must be finite and within (0, 3600] seconds')
        return ttl

    @staticmethod
    def _binding(binding):
        if not isinstance(binding, dict) or set(binding) != set(BINDINGS):
            raise StoreError('all four binding hashes required')
        if any(not isinstance(v, str) or re.fullmatch('[0-9a-f]{64}', v) is None for v in binding.values()):
            raise StoreError('bindings must be lowercase SHA256')
        return dict(binding)

    @contextmanager
    def _transaction(self):
        self.db.execute('BEGIN IMMEDIATE')
        try:
            yield
            self.db.execute('COMMIT')
        except Exception:
            self.db.execute('ROLLBACK')
            raise

    def _boot(self):
        row = self.db.execute("SELECT value FROM meta WHERE key='boot'").fetchone()
        host = self.db.execute("SELECT value FROM meta WHERE key='host_boot'").fetchone()
        if not row or row['value'] != self.boot_id or not host or host['value'] != self.host_boot_id:
            raise StoreError('store belongs to superseded boot')

    def _expire(self):
        self.db.execute("UPDATE leases SET state='expired' WHERE state='active' AND expires<=?", (time.monotonic(),))

    @staticmethod
    def _token(row):
        return {k: row[k] for k in ('task', 'lane', 'owner', 'attempt', 'fence', 'boot', 'expires', 'state')} | {
            'bindings': json.loads(row['bindings']), 'resources': json.loads(row['resources']),
            'allocation': json.loads(row['allocation'])}

    def claim(self, task, lane, owner, binding: dict, resources: dict, ttl=1800):
        if any(not isinstance(v, str) or not v.strip() or len(v) > 512 for v in (task, lane, owner)):
            raise StoreError('nonempty bounded task, lane and owner required')
        lane_match = re.fullmatch(r'L(0[1-9]|[1-4][0-9]|50)', lane)
        if not lane_match:
            raise StoreError('lane must be L01 through L50')
        lane_number = int(lane_match.group(1))
        binding = self._binding(binding)
        resources = self._resources(resources)
        if any(resources[k] <= 0 for k in ('cpu', 'memory', 'pids', 'disk')):
            raise StoreError('positive CPU, memory, PID and disk reservation required')
        if resources['executor'] != 1:
            raise StoreError('each lease must reserve exactly one executor')
        ttl = self._ttl(ttl)
        with self._transaction():
            self._boot()
            self._expire()
            if self.db.execute("SELECT 1 FROM leases WHERE state='active' AND (task=? OR lane=?)", (task, lane)).fetchone():
                raise StoreError('task or lane already owned')
            used = {k: 0 for k in CLASSES}
            for row in self.db.execute("SELECT resources FROM leases WHERE state='active'"):
                for k, v in json.loads(row['resources']).items():
                    used[k] += v
            if any(used[k] + resources[k] > self.capacity[k] for k in CLASSES):
                raise StoreError('resource capacity exhausted')
            self.db.execute('INSERT INTO counters VALUES (?,1) ON CONFLICT(task) DO UPDATE SET fence=fence+1', (task,))
            fence = self.db.execute('SELECT fence FROM counters WHERE task=?', (task,)).fetchone()['fence']
            attempt = str(uuid.uuid4())
            occupied = set()
            for active in self.db.execute("SELECT allocation FROM leases WHERE state='active'"):
                occupied.update(json.loads(active['allocation'])['ports'])
            ports = []
            port_start = 20000 + 100 * lane_number
            offset = ((fence - 1) * 4) % 100
            for index in range(100):
                port = port_start + (offset + index) % 100
                if port in occupied:
                    continue
                with socket.socket() as probe:
                    try:
                        probe.bind(('127.0.0.1', port))
                    except OSError:
                        continue
                ports.append(port)
                if len(ports) == 4:
                    break
            if len(ports) != 4:
                raise StoreError('loopback port allocation exhausted')
            namespace = 'kd-' + hashlib.sha256((self.boot_id+task+attempt).encode()).hexdigest()[:24]
            allocation = {
                'ports': ports, 'service_namespace': namespace,
                'subject_namespace': namespace, 'bucket_namespace': namespace,
                'schema_namespace': namespace.replace('-', '_'),
                'migration_range': [lane_number * 10000, lane_number * 10000 + 9999],
            }
            self.db.execute('INSERT OR REPLACE INTO leases VALUES (?,?,?,?,?,?,?,?,?,?,?)',
                            (task, lane, owner, attempt, fence, self.boot_id,
                             json.dumps(binding, sort_keys=True), json.dumps(resources, sort_keys=True), time.monotonic()+ttl, 'active', json.dumps(allocation, sort_keys=True)))
            return self._token(self.db.execute('SELECT * FROM leases WHERE task=?', (task,)).fetchone())

    def _validate(self, token):
        self._boot()
        if not isinstance(token, dict):
            raise StoreError('invalid lease token')
        row = self.db.execute('SELECT * FROM leases WHERE task=?', (token.get('task'),)).fetchone()
        if not row or row['state'] != 'active' or row['expires'] <= time.monotonic():
            raise StoreError('lease is not active')
        current = self._token(row)
        for key in ('task', 'lane', 'owner', 'attempt', 'fence', 'boot', 'bindings', 'resources', 'allocation'):
            if json.dumps(token.get(key), sort_keys=True) != json.dumps(current[key], sort_keys=True):
                raise StoreError('stale or altered lease token')
        return current

    def validate(self, token):
        with self._transaction():
            return self._validate(token)

    def bind_candidate(self, token, candidate_digest):
        """Coordinator rebind after owned patch review; invalidates prior tokens.

        Access to this method must be restricted by the coordinator process;
        the local SQLite ledger is not an authentication boundary.
        """
        if not isinstance(candidate_digest, str) or re.fullmatch('[0-9a-f]{64}', candidate_digest) is None:
            raise StoreError('candidate binding must be lowercase SHA256')
        with self._transaction():
            current = self._validate(token)
            bindings = dict(current['bindings'])
            bindings['candidate'] = candidate_digest
            self.db.execute('UPDATE counters SET fence=fence+1 WHERE task=?', (current['task'],))
            fence = self.db.execute('SELECT fence FROM counters WHERE task=?', (current['task'],)).fetchone()['fence']
            self.db.execute('UPDATE leases SET bindings=?, fence=? WHERE task=?',
                            (json.dumps(bindings, sort_keys=True), fence, current['task']))
            return self._token(self.db.execute('SELECT * FROM leases WHERE task=?', (current['task'],)).fetchone())

    def heartbeat(self, token, ttl=1800):
        ttl = self._ttl(ttl)
        with self._transaction():
            self._validate(token)
            self.db.execute('UPDATE leases SET expires=? WHERE task=?', (time.monotonic()+ttl, token['task']))
            return self._token(self.db.execute('SELECT * FROM leases WHERE task=?', (token['task'],)).fetchone())

    def _finish(self, token, state):
        with self._transaction():
            self._validate(token)
            self.db.execute('UPDATE leases SET state=? WHERE task=?', (state, token['task']))

    def accept(self, token, artifact_path, manifest_sha256, publisher=None):
        with self._transaction():
            self._validate(token)
            if publisher is not None:
                publisher()
            return self._accept_file(token, artifact_path, manifest_sha256)

    def _accept_file(self, token, artifact_path, manifest_sha256):
        """Publish a durable index only after a regular artifact is flushed.

        The coordinator owns the artifact directory and this database. This is
        local transactional publication, not a worker authentication boundary.
        """
        if not isinstance(manifest_sha256, str) or re.fullmatch('[0-9a-f]{64}', manifest_sha256) is None:
            raise StoreError('manifest hash must be lowercase SHA256')
        path = Path(artifact_path).absolute()
        if any(parent.is_symlink() for parent in (path, *path.parents)):
            raise StoreError('accepted artifact cannot use symlinks')
        try:
            descriptor = os.open(path, os.O_RDONLY | os.O_NOFOLLOW)
        except OSError as error:
            raise StoreError('accepted artifact unavailable') from error
        try:
            info = os.fstat(descriptor)
            if not stat.S_ISREG(info.st_mode) or info.st_nlink != 1:
                raise StoreError('accepted artifact must be a single-link regular file')
            digest = hashlib.sha256()
            while block := os.read(descriptor, 1024 * 1024):
                digest.update(block)
            if digest.hexdigest() != manifest_sha256:
                raise StoreError('accepted artifact hash mismatch')
            os.fsync(descriptor)
            parent_fd = os.open(path.parent, os.O_RDONLY | os.O_DIRECTORY)
            try:
                os.fsync(parent_fd)
            finally:
                os.close(parent_fd)
            current = self._validate(token)
            record = {key: current[key] for key in ('task', 'lane', 'owner', 'attempt', 'fence', 'boot', 'bindings')}
            record.update(artifact_path=str(path), manifest_sha256=manifest_sha256, token=current)
            self.db.execute('INSERT INTO accepted VALUES (?,?)', (current['attempt'], json.dumps(record, sort_keys=True)))
            self.db.execute("UPDATE leases SET state='accepted' WHERE task=?", (current['task'],))
            return record
        finally:
            os.close(descriptor)

    def accepted(self, attempt):
        """Return historical acceptance across restarts; caller checks artifact."""
        if not isinstance(attempt, str) or not attempt:
            raise StoreError('attempt required')
        row = self.db.execute('SELECT record FROM accepted WHERE attempt=?', (attempt,)).fetchone()
        return json.loads(row['record']) if row else None

    def latest_accepted(self, task):
        """Latest durable acceptance by fence, independent of worker claims."""
        if not isinstance(task, str) or not task:
            raise StoreError('task required')
        records = [json.loads(row['record']) for row in self.db.execute('SELECT record FROM accepted')]
        records = [record for record in records if record['task'] == task]
        return max(records, key=lambda record: record['fence']) if records else None

    def release(self, token):
        self._finish(token, 'released')

    def complete(self, token):
        self._finish(token, 'completed')

    def active(self):
        with self._transaction():
            self._boot()
            self._expire()
            return [self._token(row) for row in self.db.execute("SELECT * FROM leases WHERE state='active' ORDER BY task")]

    def close(self):
        self.db.close()
