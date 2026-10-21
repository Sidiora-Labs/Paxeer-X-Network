from __future__ import annotations
import argparse
from contextlib import contextmanager
from datetime import datetime, timezone
import json
from pathlib import Path
import sqlite3
import subprocess
import sys
from . import v1
from .attestations import SignatureVerifier
from .evidence import failing_report, verify_bundle
from .providers import Kubernetes, connected_plan, safe_diagnostics, validate_connected_plan


def utcnow():
    return datetime.now(timezone.utc).isoformat().replace('+00:00', 'Z')


class Journal:
    def __init__(self, path):
        path = Path(path)
        v1.need(not path.is_symlink(), 'journal symlink rejected')
        path.parent.mkdir(parents=True, exist_ok=True)
        self.connection = sqlite3.connect(path, timeout=5, isolation_level=None)
        self.connection.row_factory = sqlite3.Row
        self.connection.execute('PRAGMA journal_mode=WAL')
        self.connection.execute('PRAGMA synchronous=FULL')
        self.connection.executescript('''
CREATE TABLE IF NOT EXISTS topology_proposals(
 revision INTEGER PRIMARY KEY, state TEXT NOT NULL CHECK(state IN ('proposed','validated','activated','rejected')),
 actor TEXT NOT NULL, plan TEXT NOT NULL, config TEXT NOT NULL, evidence_digest TEXT,
 validation TEXT, created_at TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS topology_active(singleton INTEGER PRIMARY KEY CHECK(singleton=1),
 revision INTEGER NOT NULL, deployment_digest TEXT NOT NULL);
INSERT OR IGNORE INTO topology_active VALUES(1,0,'');
CREATE TABLE IF NOT EXISTS topology_intent(singleton INTEGER PRIMARY KEY CHECK(singleton=1),
 revision INTEGER NOT NULL, expected_active INTEGER NOT NULL, deployment_digest TEXT NOT NULL,
 authorization_digest TEXT NOT NULL, actor TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS topology_rehearsal(
 id TEXT PRIMARY KEY, state TEXT NOT NULL, request TEXT NOT NULL, prepared TEXT NOT NULL,
 deployment_digest TEXT NOT NULL, actor TEXT NOT NULL, observations TEXT);
CREATE TABLE IF NOT EXISTS topology_audit(sequence INTEGER PRIMARY KEY AUTOINCREMENT,
 revision INTEGER NOT NULL, actor TEXT NOT NULL, at TEXT NOT NULL,
 previous_state TEXT, state TEXT NOT NULL, detail TEXT NOT NULL);
''')

    def close(self):
        self.connection.close()

    @contextmanager
    def transaction(self):
        self.connection.execute('BEGIN IMMEDIATE')
        try:
            yield self.connection
        except BaseException:
            self.connection.execute('ROLLBACK')
            raise
        else:
            self.connection.execute('COMMIT')

    def audit(self, db, revision, actor, before, after, detail):
        db.execute('INSERT INTO topology_audit(revision,actor,at,previous_state,state,detail) VALUES(?,?,?,?,?,?)',
                   (revision, actor, utcnow(), before, after, v1.canonical(detail).decode()))

    def proposal(self, revision):
        row = self.connection.execute('SELECT * FROM topology_proposals WHERE revision=?', (revision,)).fetchone()
        v1.need(row is not None, 'proposal not found')
        return dict(row)

    def propose(self, config, plan, actor):
        v1.text(actor, 'proposal actor')
        revision = config['revision']
        with self.transaction() as db:
            maximum = db.execute('SELECT COALESCE(MAX(revision),0) FROM topology_proposals').fetchone()[0]
            v1.need(revision > maximum, 'configuration revision must increase monotonically')
            db.execute('INSERT INTO topology_proposals VALUES(?,?,?,?,?,?,?,?)',
                       (revision, 'proposed', actor, v1.canonical(plan).decode(), v1.canonical(config).decode(),
                        None, None, utcnow()))
            self.audit(db, revision, actor, None, 'proposed', safe_diagnostics(plan))
        return self.proposal(revision)

    def validate(self, revision, plan, result, dry_run, actor):
        with self.transaction() as db:
            row = self.proposal(revision)
            v1.need(row['state'] == 'proposed' and row['plan'] == v1.canonical(plan).decode(),
                    'proposal changed or not proposed')
            v1.need(result['passed'] is True and dry_run['accepted'] is True and
                    dry_run['deploymentDigest'] == plan['deploymentDigest'], 'validation evidence mismatch')
            db.execute('UPDATE topology_proposals SET state=?,evidence_digest=?,validation=? WHERE revision=?',
                       ('validated', result['evidenceDigest'], v1.canonical(dry_run).decode(), revision))
            self.audit(db, revision, actor, 'proposed', 'validated', result)
        return self.proposal(revision)

    def reject(self, revision, actor, reason):
        v1.text(reason, 'rejection reason', 1024)
        with self.transaction() as db:
            row = self.proposal(revision)
            v1.need(row['state'] in ('proposed', 'validated'), 'terminal proposal cannot transition')
            v1.need(db.execute('SELECT 1 FROM topology_intent WHERE revision=?', (revision,)).fetchone() is None,
                    'partially applied activation must reconcile, not reject')
            db.execute('UPDATE topology_proposals SET state=? WHERE revision=?', ('rejected', revision))
            self.audit(db, revision, actor, row['state'], 'rejected', {'reason': reason})
        return self.proposal(revision)

    def begin_activation(self, revision, expected_active, plan, evidence_digest, authorization_digest, actor, resume=False):
        with self.transaction() as db:
            v1.need(db.execute("SELECT 1 FROM topology_rehearsal WHERE state!='recovered'").fetchone() is None,
                    'interrupted rehearsal must recover before activation')
            row = self.proposal(revision)
            active = db.execute('SELECT revision FROM topology_active WHERE singleton=1').fetchone()[0]
            v1.need(row['state'] == 'validated' and row['plan'] == v1.canonical(plan).decode() and
                    row['evidence_digest'] == evidence_digest, 'activation does not match validated revision')
            v1.need(active == expected_active and revision > active, 'active configuration compare-and-swap failed')
            intent = db.execute('SELECT * FROM topology_intent WHERE singleton=1').fetchone()
            if intent is not None:
                v1.need(intent['revision'] == revision and intent['expected_active'] == expected_active and
                        intent['deployment_digest'] == plan['deploymentDigest'] and intent['actor'] == actor,
                        'unreconciled activation blocks another deployment')
                v1.need(resume or intent['authorization_digest'] == authorization_digest,
                        'activation authority changed without explicit resume')
                if intent['authorization_digest'] != authorization_digest:
                    db.execute('UPDATE topology_intent SET authorization_digest=? WHERE singleton=1',
                               (authorization_digest,))
                    self.audit(db,revision,actor,'validated','validated',
                               {'resumed':True,'authorizationDigest':authorization_digest})
                return
            v1.need(not resume,'resume requires an existing activation intent')
            db.execute('INSERT INTO topology_intent VALUES(1,?,?,?,?,?)',
                       (revision, expected_active, plan['deploymentDigest'], authorization_digest, actor))
            self.audit(db, revision, actor, 'validated', 'validated',
                       {'activationIntent': True, 'expectedActiveRevision': expected_active,
                        'deploymentDigest': plan['deploymentDigest'], 'authorizationDigest': authorization_digest})

    def activated(self, revision, receipt, actor):
        with self.transaction() as db:
            intent = db.execute('SELECT * FROM topology_intent WHERE singleton=1').fetchone()
            v1.need(intent is not None and intent['revision'] == revision and intent['actor'] == actor,
                    'activation intent missing')
            row = self.proposal(revision)
            v1.need(row['state'] == 'validated', 'activation proposal no longer validated')
            v1.need(receipt['applied'] is True and receipt['deploymentDigest'] == intent['deployment_digest'],
                    'activation receipt identity mismatch')
            changed = db.execute('UPDATE topology_active SET revision=?,deployment_digest=? WHERE singleton=1 AND revision=?',
                                 (revision, intent['deployment_digest'], intent['expected_active'])).rowcount
            v1.need(changed == 1, 'active configuration compare-and-swap failed')
            db.execute('UPDATE topology_proposals SET state=? WHERE revision=? AND state=?',
                       ('activated', revision, 'validated'))
            db.execute('DELETE FROM topology_intent WHERE singleton=1')
            self.audit(db, revision, actor, 'validated', 'activated', receipt)
        return self.proposal(revision)


class Operator:
    def __init__(self, journal, config, plan, provider, verifier, phase=2):
        v1.need(phase in (2,3), 'operator phase invalid')
        self.phase = phase
        self.journal, self.config, self.plan = journal, config, plan
        self.provider, self.verifier = provider, verifier
        validate_connected_plan(plan, config)
        v1.need(plan['configurationDigest'] == v1.digest(config), 'operator configuration binding mismatch')

    def propose(self, actor):
        return self.journal.propose(self.config, self.plan, actor)

    def validate(self, bundle, actor, now=None):
        validate_connected_plan(self.plan,self.config)
        revision = self.config['revision']
        v1.need(self.phase != 3 or bundle.get('schemaVersion') == 3, 'phase three evidence required')
        self.verifier.verify(bundle['payload'], bundle['signature'])
        v1.need(actor == bundle['signature']['keyId'], 'validation actor identity mismatch')
        try:
            result = verify_bundle(bundle, self.plan, self.config, self.verifier, now=now)
            dry_run = self.provider.dry_run(self.plan)
        except (v1.Invalid, OSError, ValueError, subprocess.SubprocessError) as exc:
            self.journal.reject(revision, actor, 'validation_failed')
            raise v1.Invalid('validation failed; proposal rejected') from exc
        return self.journal.validate(revision, self.plan, result, dry_run, actor)

    def activate(self, bundle, authorization, now=None):
        now = now or datetime.now(timezone.utc)
        validate_connected_plan(self.plan, self.config)
        v1.need(self.phase != 3 or bundle.get('schemaVersion') == 3, 'phase three evidence required')
        result = verify_bundle(bundle, self.plan, self.config, self.verifier, now=now)
        v1.keys(authorization, ['payload', 'signature'], 'activation authorization')
        payload = authorization['payload']
        v1.keys(payload, ['action', 'revision', 'expectedActiveRevision', 'actor', 'configurationDigest',
                         'deploymentDigest', 'evidenceDigest', 'notBefore', 'expiresAt', 'context', 'namespace'],
                'activation authorization payload')
        self.verifier.verify(payload, authorization['signature'], purpose='activation')
        v1.need(payload['actor'] == authorization['signature']['keyId'], 'activation actor identity mismatch')
        v1.need(payload['action'] in ('activate', 'resume') and type(payload['revision']) is int and
                payload['revision'] == self.config['revision'], 'activation authorization action/revision')
        v1.integer(payload['expectedActiveRevision'], 0, 2**31-1, 'expected active revision')
        for key in ('configurationDigest', 'deploymentDigest'):
            v1.need(payload[key] == self.plan[key], 'activation authorization identity mismatch')
        v1.need(payload['evidenceDigest'] == result['evidenceDigest'], 'activation evidence changed')
        start = v1.timestamp(payload['notBefore'], 'authorization start')
        end = v1.timestamp(payload['expiresAt'], 'authorization expiration')
        v1.need(start <= now < end and (end-start).total_seconds() <= 900, 'activation authority expired')
        v1.need(payload['context'] == self.provider.context and payload['namespace'] == self.provider.namespace,
                'activation cluster target mismatch')
        row = self.journal.proposal(self.config['revision'])
        dry_run = json.loads(row['validation']) if row['validation'] is not None else None
        v1.need(dry_run is not None and dry_run['context'] == self.provider.context and
                dry_run['namespace'] == self.provider.namespace, 'validated cluster target changed')
        if self.config['environment'] == 'production':
            v1.need(self.plan['chainRecordedStatus'] in ('active', 'eligible'),
                    'production chain governance activation incomplete')
        self.provider.dry_run(self.plan)
        self.journal.begin_activation(self.config['revision'], payload['expectedActiveRevision'], self.plan,
                                      result['evidenceDigest'], v1.digest(authorization), payload['actor'],
                                      resume=payload['action'] == 'resume')
        receipt = self.provider.activate(self.plan)
        return self.journal.activated(self.config['revision'], receipt, payload['actor'])

    def reject(self, authorization, now=None):
        now=now or datetime.now(timezone.utc)
        v1.keys(authorization,['payload','signature'],'rejection authorization')
        payload=authorization['payload']
        v1.keys(payload,['action','actor','revision','configurationDigest','deploymentDigest',
                         'notBefore','expiresAt'],'rejection authority')
        self.verifier.verify(payload,authorization['signature'],purpose='activation')
        v1.need(payload['action']=='reject' and payload['actor']==authorization['signature']['keyId'] and
                type(payload['revision']) is int and payload['revision']==self.config['revision'],
                'rejection authority identity mismatch')
        for field in ('configurationDigest','deploymentDigest'):
            v1.need(payload[field]==self.plan[field],'rejection deployment identity changed')
        start=v1.timestamp(payload['notBefore'],'rejection start')
        end=v1.timestamp(payload['expiresAt'],'rejection expiry')
        v1.need(start<=now<end and (end-start).total_seconds()<=900,'rejection authority expired')
        return self.journal.reject(self.config['revision'],payload['actor'],'operator_rejected')

    def rehearse(self, request, authorization, recover=False, now=None):
        v1.need(self.phase==3 and self.config['environment']=='qualification', 'isolated phase three only')
        validate_connected_plan(self.plan,self.config)
        now=now or datetime.now(timezone.utc)
        v1.keys(authorization,['payload','signature'],'rehearsal authorization')
        payload=authorization['payload']
        v1.keys(payload,['action','actor','requestDigest','configurationDigest','deploymentDigest',
                         'context','namespace','notBefore','expiresAt'],'rehearsal authority')
        self.verifier.verify(payload,authorization['signature'],purpose='activation')
        v1.need(payload['actor']==authorization['signature']['keyId'] and
                payload['action']==('recover-rehearsal' if recover else 'rehearse') and
                payload['requestDigest']==v1.digest(request),'rehearsal authority identity mismatch')
        for field in ('configurationDigest','deploymentDigest'):
            v1.need(payload[field]==self.plan[field],'rehearsal deployment identity changed')
        v1.need(payload['context']==self.provider.context and payload['namespace']==self.provider.namespace,
                'rehearsal target changed')
        start=v1.timestamp(payload['notBefore'],'rehearsal authority start')
        end=v1.timestamp(payload['expiresAt'],'rehearsal authority expiry')
        v1.need(start<=now<end and (end-start).total_seconds()<=900,'rehearsal authority expired')
        with self.journal.transaction() as db:
            v1.need(db.execute('SELECT 1 FROM topology_intent').fetchone() is None,
                    'pending activation blocks rehearsal')
            row=db.execute('SELECT * FROM topology_rehearsal WHERE id=?',(request['id'],)).fetchone()
            if row is not None:
                v1.need(recover and row['state']!='recovered' and row['request']==v1.canonical(request).decode()
                        and row['actor']==payload['actor'] and row['deployment_digest']==self.plan['deploymentDigest'],
                        'duplicate or changed rehearsal rejected')
                prepared=json.loads(row['prepared'])
            else:
                v1.need(not recover,'rehearsal recovery intent missing')
                v1.need(db.execute("SELECT 1 FROM topology_rehearsal WHERE state!='recovered'").fetchone() is None,
                        'another rehearsal must recover first')
                prepared=self.provider.prepare_rehearsal(self.plan,request)
                db.execute('INSERT INTO topology_rehearsal VALUES(?,?,?,?,?,?,?)',
                           (request['id'],'in_progress',v1.canonical(request).decode(),v1.canonical(prepared).decode(),
                            self.plan['deploymentDigest'],payload['actor'],None))
        fault=None
        try:
            if not recover:
                fault=self.provider.inject_rehearsal(self.plan,request,prepared)
        finally:
            recovered=self.provider.recover_rehearsal(request,prepared)
            observations={'before':prepared['before'],'fault':fault,'recovered':recovered,
                          'notQualificationEvidence':True}
            with self.journal.transaction() as db:
                db.execute('UPDATE topology_rehearsal SET state=?,observations=? WHERE id=?',
                           ('recovered',v1.canonical(observations).decode(),request['id']))
        return observations


def main(argv=None):
    parser = argparse.ArgumentParser()
    parser.add_argument('action', choices=('render', 'propose', 'validate', 'activate', 'reject', 'diagnostics', 'rehearse', 'recover-rehearsal'))
    parser.add_argument('--phase', type=int, choices=(2,3), default=2)
    parser.add_argument('--inputs', required=True)
    parser.add_argument('--source-root', required=True)
    parser.add_argument('--journal', required=True)
    parser.add_argument('--actor', required=True)
    parser.add_argument('--rehearsal')
    parser.add_argument('--evidence')
    parser.add_argument('--authorization')
    parser.add_argument('--output', required=True)
    args = parser.parse_args(argv)
    journal = None
    plan = None
    try:
        inputs = v1.load(args.inputs)
        v1.keys(inputs, ['schemaVersion', 'topology', 'adapterSHA256', 'documents', 'chainFile', 'chainSHA256',
                         'expectedSourceDigest', 'expectedInterfaceDigest', 'kubectl', 'openssl', 'trust'],
                'operator inputs')
        v1.need(type(inputs['schemaVersion']) is int and inputs['schemaVersion'] == 2, 'operator input version')
        config = v1.validate(inputs['topology'])
        v1.need(config['release']['sourceDigest'] == inputs['expectedSourceDigest'] and
                config['release']['interfaceDigest'] == inputs['expectedInterfaceDigest'], 'prior integrated baseline mismatch')
        plan = connected_plan(config, args.source_root, inputs['adapterSHA256'], inputs['documents'],
                              inputs['chainFile'], inputs['chainSHA256'])
        if args.action in ('render', 'diagnostics'):
            output = plan if args.action == 'render' else safe_diagnostics(plan)
        else:
            journal = Journal(args.journal)
            if args.action == 'propose':
                output = journal.propose(config, plan, args.actor)
            else:
                v1.keys(inputs['kubectl'], ['path', 'sha256', 'context'], 'kubectl input')
                v1.keys(inputs['openssl'], ['path', 'sha256'], 'openssl input')
                provider = Kubernetes(inputs['kubectl']['path'], inputs['kubectl']['sha256'],
                                      inputs['kubectl']['context'], config['namespace'], Path(args.output).parent)
                verifier = SignatureVerifier(inputs['openssl']['path'], inputs['openssl']['sha256'], inputs['trust'])
                operator = Operator(journal, config, plan, provider, verifier, phase=args.phase)
                if args.action == 'reject':
                    v1.need(bool(args.authorization),'signed rejection required')
                    output=operator.reject(v1.load(args.authorization))
                elif args.action in ('rehearse','recover-rehearsal'):
                    v1.need(bool(args.rehearsal) and bool(args.authorization),'signed rehearsal request required')
                    output=operator.rehearse(v1.load(args.rehearsal),v1.load(args.authorization),
                                             recover=args.action=='recover-rehearsal')
                elif args.action == 'validate':
                    v1.need(bool(args.evidence),'signed qualification evidence required')
                    bundle=v1.load(args.evidence)
                    output = operator.validate(bundle, args.actor)
                else:
                    v1.need(bool(args.evidence),'signed qualification evidence required')
                    bundle=v1.load(args.evidence)
                    v1.need(bool(args.authorization), 'signed activation authority required')
                    output = operator.activate(bundle, v1.load(args.authorization))
            output = {key: value for key, value in output.items() if key not in ('plan', 'config')}
        path = Path(args.output)
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(json.dumps(output, sort_keys=True, indent=2) + '\n')
        return 0
    except (v1.Invalid, OSError, ValueError, KeyError, TypeError, sqlite3.Error, subprocess.SubprocessError) as exc:
        path = Path(args.output)
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(json.dumps(failing_report(str(exc), plan), sort_keys=True, indent=2) + '\n')
        print('topology operation rejected; see requirement results', file=sys.stderr)
        return 1
    finally:
        if journal is not None:
            journal.close()


if __name__ == '__main__':
    raise SystemExit(main())
