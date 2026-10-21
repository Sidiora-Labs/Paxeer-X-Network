from __future__ import annotations

from dataclasses import dataclass
import argparse
import hashlib
import hmac
import json
from pathlib import Path
import re
import sys
import time

from providers import BoundTools, PostgreSQL, ProviderFailure, identifier, literal, regular
from render import RecoveryIdentity

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
import recovery


def reconcile_v2(expected, actual):
    for value in (expected, actual):
        recovery.exact(value, {'version', 'checkpoint', 'snapshot_id', 'counts', 'amounts_raw', 'event_identities_sha256'})
        recovery.require(type(value['version']) is int and value['version'] == 2, 'version2 reconciliation required')
        recovery.checkpoint(value['checkpoint'])
        recovery.name(value['snapshot_id'])
        recovery.require(isinstance(value['counts'], dict) and 1 <= len(value['counts']) <= 100,
                         'reconciliation count inventory required')
        recovery.require(isinstance(value['amounts_raw'], dict) and 1 <= len(value['amounts_raw']) <= 100,
                         'reconciliation monetary inventory required')
        for key, count in value['counts'].items():
            recovery.name(key)
            recovery.integer(count)
        for key, amount in value['amounts_raw'].items():
            recovery.name(key)
            recovery.require(isinstance(amount, str) and re.fullmatch(r'0|-?[1-9][0-9]{0,77}', amount),
                             'exact signed raw monetary integer required')
        recovery.digest(value['event_identities_sha256'])
    recovery.require(recovery.canonical(expected) == recovery.canonical(actual), 'restoration reconciliation mismatch')
    return {'reconciled': True, 'sha256': recovery.sha(actual)}


@dataclass(frozen=True)
class RecoveryProposal:
    proposal_id: str
    actor: str
    expected_epoch: int
    revision: int
    checkpoint_sha256: str

    def validate(self):
        recovery.name(self.proposal_id)
        recovery.require(isinstance(self.actor, str) and 1 <= len(self.actor) <= 128 and '\x00' not in self.actor,
                         'authenticated actor required')
        recovery.integer(self.expected_epoch, 1)
        recovery.integer(self.revision, 1)
        recovery.digest(self.checkpoint_sha256)
        recovery.require(self.checkpoint_sha256 != '0' * 64, 'checkpoint provenance required')
        return self


class ObservationArchive:
    def __init__(self, root, signing_key, identity, *, max_age_seconds=300):
        self.root = Path(root).resolve(strict=True)
        recovery.require(self.root.is_dir() and not Path(root).is_symlink(), 'owned observation directory required')
        self.key = regular(signing_key, 4096, private=True).read_bytes()
        recovery.require(32 <= len(self.key) <= 4096, 'independent runner key required')
        self.identity = identity.validate()
        recovery.integer(max_age_seconds, 1, 300)
        self.max_age_seconds = max_age_seconds

    def read(self, path):
        path = regular(path, recovery.MAX_JSON)
        recovery.require(path.resolve().is_relative_to(self.root), 'observation outside admitted directory')
        signed = recovery.load(path.read_bytes())
        recovery.exact(signed, {'payload', 'hmac_sha256'})
        payload = signed['payload']
        signature = hmac.new(self.key, recovery.canonical(payload), hashlib.sha256).hexdigest()
        recovery.require(isinstance(signed['hmac_sha256'], str) and
                         hmac.compare_digest(signature, signed['hmac_sha256']), 'untrusted runner observation')
        recovery.exact(payload, {'version', 'proposal_id', 'expected_epoch', 'revision', 'checkpoint_sha256',
                                'identity', 'measurements', 'evidence', 'artifacts', 'reconciliation',
                                'old_primary_fenced', 'old_primary_read_only', 'broker_reached_watermark',
                                'stale_write_rejected', 'stale_ack_rejected', 'unpublished_republished',
                                'duplicate_effects', 'corrupt_partition_halted', 'independent_source_verified',
                                'application_delete_denied', 'cross_environment_decrypt_denied',
                                'audit_principal', 'missing_objects', 'verified_recovery_reads'})
        recovery.require(payload['version'] == 2 and type(payload['version']) is int, 'version2 observation required')
        recovery.require(payload['identity'] == self.identity.diagnostic(), 'observation source identity mismatch')
        recovery.name(payload['proposal_id'])
        recovery.integer(payload['expected_epoch'], 1)
        recovery.integer(payload['revision'], 1)
        recovery.digest(payload['checkpoint_sha256'])
        evidence = payload['evidence']
        recovery.require(isinstance(evidence, list) and len(evidence) == len(recovery.METRICS),
                         'all ten measured requirement observations required')
        seen = set()
        now = int(time.time())
        for item in evidence:
            recovery.measured_evidence(item)
            rid = item['requirement']
            recovery.require(rid not in seen and item['outcome'] == 'pass' and not item['limitations'],
                             'missing, duplicate or failed measured requirement: ' + rid)
            seen.add(rid)
            for key in ('deployment_sha256', 'source_sha256', 'inventory_sha256'):
                recovery.require(item[key] == getattr(self.identity, key), 'evidence provenance mismatch')
            recovery.require(0 <= now - item['ended_unix'] <= self.max_age_seconds,
                             'expired or future recovery measurement')
            for sample in item['samples']:
                recovery.require(sample['environment'] == self.identity.environment and
                                 sample['service'] == self.identity.service and
                                 sample['zone'] in (self.identity.primary_region, self.identity.recovery_region),
                                 'metric ownership mismatch')
        recovery.require(seen == set(recovery.METRICS), 'requirement evidence gap')
        by_requirement = {item['requirement']: item for item in evidence}
        recovery.require(max(s['value'] for s in by_requirement['D14_031']['samples']) < 60,
                         'measured WAL archive age exceeds target')
        recovery.require(max(s['value'] for s in by_requirement['D14_032']['samples']) <= 1800,
                         'measured recovery elapsed exceeds target')
        for rid in ('D14_033', 'D14_036', 'D14_038'):
            recovery.require(by_requirement[rid]['samples'][-1]['value'] > by_requirement[rid]['samples'][0]['value'],
                             'required failure injection not observed: ' + rid)
        recovery.require(by_requirement['D14_034']['samples'][-1]['value'] == 0,
                         'unpublished outbox recovery incomplete')
        recovery.require(by_requirement['D14_039']['samples'][-1]['value'] == 0 and
                         max(s['value'] for s in by_requirement['D14_039']['samples']) > 0,
                         'missing-object failure/recovery not observed')
        artifacts = payload['artifacts']
        recovery.require(isinstance(artifacts, list) and 10 <= len(artifacts) <= 1000,
                         'independent native artifacts required')
        covered = set()
        seen_paths = set()
        input_hashes = {e['requirement']: e['inputs_sha256'] for e in evidence}
        for artifact in artifacts:
            recovery.exact(artifact, {'requirement', 'path', 'sha256', 'inputs_sha256'})
            rid = artifact['requirement']
            recovery.require(rid in recovery.METRICS and artifact['inputs_sha256'] == input_hashes[rid],
                             'native artifact input binding mismatch')
            recovery.digest(artifact['sha256'])
            recovery.object_key(artifact['path'])
            recovery.require(artifact['path'] not in seen_paths, 'native artifact reused')
            seen_paths.add(artifact['path'])
            candidate = regular(self.root / artifact['path'], 16 * 1024 * 1024)
            recovery.require(candidate.resolve().is_relative_to(self.root) and candidate.stat().st_size > 0,
                             'missing native artifact')
            recovery.require(hashlib.sha256(candidate.read_bytes()).hexdigest() == artifact['sha256'],
                             'changed native recovery artifact')
            covered.add(rid)
        recovery.require(covered == set(recovery.METRICS), 'native artifact requirement coverage gap')
        self.validate_measurements(payload)
        return payload

    def validate_measurements(self, payload):
        measurements = payload['measurements']
        bounds = {'wal_archive_age_seconds': (0, 59), 'cross_region_lag_seconds': (0, 300),
                  'rpo_seconds': (0, 300), 'rto_seconds': (0, 1800),
                  'dataset_event_hours': (72, 8760), 'acknowledged_writes_lost': (0, 0),
                  'writable_regions': (1, 1), 'object_replication_lag_seconds': (0, 300)}
        recovery.exact(measurements, bounds)
        for key, (low, high) in bounds.items():
            recovery.integer(measurements[key], low, high)
        for key in ('old_primary_fenced', 'old_primary_read_only', 'broker_reached_watermark',
                    'stale_write_rejected', 'stale_ack_rejected', 'unpublished_republished',
                    'corrupt_partition_halted', 'independent_source_verified',
                    'application_delete_denied', 'cross_environment_decrypt_denied', 'verified_recovery_reads'):
            recovery.require(payload[key] is True, 'failed recovery condition: ' + key)
        recovery.require(isinstance(payload['audit_principal'], str) and
                         1 <= len(payload['audit_principal']) <= 128, 'provider audit principal required')
        recovery.integer(payload['duplicate_effects'], 0, 0)
        recovery.integer(payload['missing_objects'], 0, 0)
        recovery.exact(payload['reconciliation'], {'expected', 'actual'})
        reconcile_v2(payload['reconciliation']['expected'], payload['reconciliation']['actual'])
        checkpoint = payload['reconciliation']['actual']['checkpoint']
        for key in ('source_sha256', 'deployment_sha256', 'interface_sha256'):
            recovery.require(checkpoint[key] == getattr(self.identity, key), 'reconciliation identity mismatch')
        recovery.require(checkpoint['epoch'] == payload['expected_epoch'] and
                         recovery.sha(checkpoint) == payload['checkpoint_sha256'], 'recovery checkpoint mismatch')


class RecoveryOperator:
    def __init__(self, database, identity, observations, *, schema='recovery'):
        self.database, self.identity, self.observations = database, identity.validate(), observations
        self.schema = identifier(schema)

    def authenticated(self, proposal):
        proposal.validate()
        recovery.require(proposal.actor == self.database.role, 'proposal actor must be authenticated database principal')
        return proposal

    def audit(self, proposal, state):
        return ('INSERT INTO ' + self.schema + '.recovery_audit'
                '(proposal_id,state,actor,revision,epoch,region,checkpoint_sha256,fence_proof_sha256) '
                'SELECT proposal_id,state,actor,revision,expected_epoch,next_region,checkpoint_sha256,fence_proof_sha256 '
                'FROM ' + self.schema + '.recovery_proposals WHERE proposal_id=' + literal(proposal.proposal_id) +
                ' AND actor=' + literal(proposal.actor) + ' AND state=' + literal(state) + ';')

    def transaction(self, body):
        return self.database.query('BEGIN; SET LOCAL search_path=pg_catalog; ' + body + ' COMMIT;')

    def propose(self, proposal):
        self.authenticated(proposal)
        p = proposal
        guard = 'SELECT ' + self.schema + '.assert_epoch(' + str(p.expected_epoch) + ',' + literal(self.identity.primary_region) + ');'
        insert = ('INSERT INTO ' + self.schema + '.recovery_proposals '
                  '(proposal_id,expected_epoch,next_region,revision,checkpoint_sha256,state,actor) SELECT ' +
                  ','.join((literal(p.proposal_id), str(p.expected_epoch), literal(self.identity.recovery_region),
                            str(p.revision), literal(p.checkpoint_sha256), "'proposed'", literal(p.actor))) +
                  ' FROM ' + self.schema + '.recovery_control WHERE singleton AND revision<' + str(p.revision) +
                  "; DO $proposal$ BEGIN IF NOT EXISTS(SELECT 1 FROM " + self.schema +
                  '.recovery_proposals WHERE proposal_id=' + literal(p.proposal_id) +
                  ") THEN RAISE EXCEPTION 'stale recovery revision'; END IF; END $proposal$;")
        self.transaction(guard + insert + self.audit(p, 'proposed'))
        return {'proposal_id': p.proposal_id, 'state': 'proposed', 'identity': self.identity.diagnostic()}

    def reject(self, proposal):
        self.authenticated(proposal)
        sql = ('DO $rejection$ DECLARE changed integer; BEGIN UPDATE ' + self.schema +
               ".recovery_proposals SET state='rejected' WHERE proposal_id=" + literal(proposal.proposal_id) +
               ' AND actor=' + literal(proposal.actor) + ' AND expected_epoch=' + str(proposal.expected_epoch) +
               ' AND revision=' + str(proposal.revision) + ' AND checkpoint_sha256=' + literal(proposal.checkpoint_sha256) +
               " AND state IN ('proposed','validated'); GET DIAGNOSTICS changed=ROW_COUNT; "
               "IF changed<>1 THEN RAISE EXCEPTION 'recovery rejection conflict'; END IF; END $rejection$;" +
               self.audit(proposal, 'rejected'))
        self.transaction(sql)
        return {'proposal_id': proposal.proposal_id, 'state': 'rejected'}

    def validate(self, proposal, observation_file):
        self.authenticated(proposal)
        try:
            payload = self.observations.read(observation_file)
            for key in ('proposal_id', 'expected_epoch', 'revision', 'checkpoint_sha256'):
                recovery.require(payload[key] == getattr(proposal, key), 'observation proposal mismatch')
        except (ValueError, OSError, ProviderFailure):
            self.reject(proposal)
            raise
        p = proposal
        sql = ('SELECT ' + self.schema + '.assert_epoch(' + str(p.expected_epoch) + ',' + literal(self.identity.primary_region) + ');'
               'UPDATE ' + self.schema + ".recovery_proposals p SET state='validated',fence_proof_sha256=f.proof_sha256 FROM " +
               self.schema + '.recovery_broker_fences f WHERE p.proposal_id=' + literal(p.proposal_id) +
               ' AND p.actor=' + literal(p.actor) + " AND p.state='proposed' AND p.expected_epoch=" + str(p.expected_epoch) +
               ' AND p.revision=' + str(p.revision) + ' AND p.checkpoint_sha256=' + literal(p.checkpoint_sha256) +
               " AND f.proposal_id=p.proposal_id AND f.state='verified' AND f.expected_epoch=p.expected_epoch "
               'AND f.region=' + literal(self.identity.primary_region) +
               ' AND f.revision=p.revision AND f.actor=p.actor AND f.verified_at>=clock_timestamp()-interval \'5 minutes\';'
               'DO $validation$ BEGIN IF NOT EXISTS(SELECT 1 FROM ' + self.schema + '.recovery_proposals WHERE proposal_id=' +
               literal(p.proposal_id) + " AND state='validated') THEN RAISE EXCEPTION 'fresh broker fence required'; END IF; END $validation$;" +
               self.audit(p, 'validated'))
        self.transaction(sql)
        return {'proposal_id': p.proposal_id, 'state': 'validated', 'observation_sha256': recovery.sha(payload)}

    def activate(self, proposal, observation_file):
        self.authenticated(proposal)
        payload = self.observations.read(observation_file)
        for key in ('proposal_id', 'expected_epoch', 'revision', 'checkpoint_sha256'):
            recovery.require(payload[key] == getattr(proposal, key), 'activation observation proposal mismatch')
        sql = ('SELECT ' + self.schema + '.activate_fenced_v2(' + literal(proposal.proposal_id) + ',' +
               literal(proposal.actor) + ');')
        value = self.transaction(sql)
        recovery.require(value == str(proposal.expected_epoch + 1), 'activation epoch mismatch')
        return {'proposal_id': proposal.proposal_id, 'state': 'activated', 'epoch': int(value),
                'region': self.identity.recovery_region, 'automatic_failback': False,
                'traffic_cutover_allowed': False, 'identity': self.identity.diagnostic()}


class AdversarialArchive(ObservationArchive):
    def __init__(self, root, signing_key, identity, *, native_tools, max_age_seconds=300):
        super().__init__(root, signing_key, identity, max_age_seconds=max_age_seconds)
        recovery.require(isinstance(native_tools, dict) and bool(native_tools), 'admitted native executable inventory required')
        self.native_tool_digests = set()
        for binding in native_tools.values():
            recovery.exact(binding, {'path', 'sha256'})
            recovery.digest(binding['sha256'])
            recovery.require(binding['sha256'] != '0'*64 and isinstance(binding['path'], str) and
                             Path(binding['path']).is_absolute(), 'admitted native executable identity required')
            self.native_tool_digests.add(binding['sha256'])

    def read_scenarios(self, path, observation):
        path = regular(path, recovery.MAX_JSON, private=True)
        recovery.require(path.resolve().is_relative_to(self.root), 'scenario inventory outside admitted directory')
        signed = recovery.load(path.read_bytes())
        recovery.exact(signed, {'payload', 'hmac_sha256'})
        payload = signed['payload']
        signature = hmac.new(self.key, recovery.canonical(payload), hashlib.sha256).hexdigest()
        recovery.require(isinstance(signed['hmac_sha256'], str) and hmac.compare_digest(signature, signed['hmac_sha256']),
                         'untrusted adversarial scenario inventory')
        recovery.exact(payload, {'version', 'realm', 'identity', 'proposal_id', 'expected_epoch', 'revision',
                                'checkpoint_sha256', 'observation_sha256', 'scenarios'})
        recovery.require(type(payload['version']) is int and payload['version'] == 3 and
                         payload['realm'] == 'isolated-recovery-qualification', 'isolated version3 rehearsals required')
        recovery.require(payload['identity'] == self.identity.diagnostic() and
                         payload['observation_sha256'] == recovery.sha(observation), 'scenario observation identity mismatch')
        for key in ('proposal_id', 'expected_epoch', 'revision', 'checkpoint_sha256'):
            recovery.require(payload[key] == observation[key], 'scenario proposal identity mismatch')
        recovery.exact(payload['scenarios'], recovery.METRICS)
        inputs = {e['requirement']: e for e in observation['evidence']}
        seen_paths = set()
        for rid, binding in payload['scenarios'].items():
            recovery.exact(binding, {'path', 'sha256', 'inputs_sha256'})
            recovery.object_key(binding['path']); recovery.digest(binding['sha256'])
            recovery.require(binding['path'] not in seen_paths, 'native scenario reused'); seen_paths.add(binding['path'])
            recovery.require(binding['inputs_sha256'] == inputs[rid]['inputs_sha256'], 'scenario inputs changed')
            native_file = regular(self.root / binding['path'], recovery.MAX_JSON, private=True)
            raw = native_file.read_bytes()
            recovery.require(hashlib.sha256(raw).hexdigest() == binding['sha256'], 'native scenario changed')
            native = recovery.load(raw)
            recovery.exact(native, {'version', 'realm', 'requirement', 'identity', 'inputs_sha256',
                                    'started_unix', 'ended_unix', 'tool_sha256', 'exit_code', 'result'})
            recovery.require(type(native['version']) is int and native['version'] == 3 and
                             native['realm'] == payload['realm'] and native['requirement'] == rid and
                             native['identity'] == self.identity.diagnostic() and
                             native['inputs_sha256'] == binding['inputs_sha256'], 'native scenario provenance mismatch')
            recovery.digest(native['tool_sha256']); recovery.require(native['tool_sha256'] != '0'*64, 'native executable provenance absent')
            recovery.require(native['tool_sha256'] in self.native_tool_digests, 'unadmitted native scenario executable')
            recovery.integer(native['exit_code'], 0, 0)
            evidence = inputs[rid]
            recovery.integer(native['started_unix'], evidence['started_unix'], evidence['failure_start_unix'])
            recovery.integer(native['ended_unix'], evidence['failure_end_unix'], evidence['ended_unix'])
            recovery.adversarial_scenario(rid, native['result'])
        return payload


class AdversarialOperator(RecoveryOperator):
    def status(self, proposal):
        self.authenticated(proposal)
        row = self.database.document('SELECT json_build_object(\'state\',p.state,\'epoch\',c.epoch,\'region\',c.region,'
                                     "'activation_audits',(SELECT count(*) FROM " + self.schema +
                                     ".recovery_audit a WHERE a.proposal_id=p.proposal_id AND a.state='activated' "
                                     'AND a.actor=p.actor AND a.epoch=p.expected_epoch+1 AND a.revision=p.revision)) FROM ' +
                                     self.schema + '.recovery_proposals p CROSS JOIN ' + self.schema +
                                     '.recovery_control c WHERE c.singleton AND p.proposal_id=' + literal(proposal.proposal_id) +
                                     ' AND p.actor=session_user::text AND p.actor=' + literal(proposal.actor) +
                                     ' AND p.expected_epoch=' + str(proposal.expected_epoch) + ' AND p.revision=' + str(proposal.revision) +
                                     ' AND p.checkpoint_sha256=' + literal(proposal.checkpoint_sha256) + ';')
        recovery.require(row['state'] in {'proposed', 'validated', 'activated', 'rejected'}, 'invalid recovery state')
        if row['state'] == 'activated':
            recovery.require(row['activation_audits'] == 1 and row['epoch'] == proposal.expected_epoch + 1 and
                             row['region'] == self.identity.recovery_region, 'activation reconciliation conflict')
        else:
            recovery.require(row['activation_audits'] == 0, 'partial activation audit conflict')
        return {**row, 'proposal_id': proposal.proposal_id, 'traffic_cutover_allowed': False}

    def validate_scenarios(self, proposal, observation_file, scenarios_file):
        self.authenticated(proposal)
        try:
            observation = self.observations.read(observation_file)
            scenarios = self.observations.read_scenarios(scenarios_file, observation)
            for key in ('proposal_id', 'expected_epoch', 'revision', 'checkpoint_sha256'):
                recovery.require(observation[key] == getattr(proposal, key), 'adversarial proposal mismatch')
        except (ValueError, OSError, ProviderFailure):
            self.reject(proposal)
            raise
        p = proposal
        valid_until = min(item['ended_unix'] + self.observations.max_age_seconds for item in observation['evidence'])
        sql = ('SELECT ' + self.schema + '.assert_epoch(' + str(p.expected_epoch) + ',' + literal(self.identity.primary_region) + ');'
               'DO $validation$ DECLARE changed integer; BEGIN UPDATE ' + self.schema +
               ".recovery_proposals p SET state='validated',fence_proof_sha256=f.proof_sha256 FROM " + self.schema +
               '.recovery_broker_fences f WHERE p.proposal_id=' + literal(p.proposal_id) +
               ' AND p.actor=session_user::text AND p.actor=' + literal(p.actor) +
               " AND p.state='proposed' AND p.expected_epoch=" + str(p.expected_epoch) +
               ' AND p.revision=' + str(p.revision) + ' AND p.checkpoint_sha256=' + literal(p.checkpoint_sha256) +
               " AND f.proposal_id=p.proposal_id AND f.state='verified' AND f.expected_epoch=p.expected_epoch "
               'AND f.region=' + literal(self.identity.primary_region) +
               " AND f.revision=p.revision AND f.actor=p.actor AND f.verified_at<=clock_timestamp() "
               "AND f.verified_at>=clock_timestamp()-interval '5 minutes'; GET DIAGNOSTICS changed=ROW_COUNT; "
               "IF changed<>1 THEN RAISE EXCEPTION 'fresh exact broker fence required'; END IF; END $validation$;"
               'INSERT INTO ' + self.schema +
               '.recovery_validations_v3(proposal_id,observation_sha256,scenarios_sha256,actor,valid_until) VALUES(' +
               ','.join((literal(p.proposal_id), literal(recovery.sha(observation)), literal(recovery.sha(scenarios)),
                         literal(p.actor), 'to_timestamp(' + str(valid_until) + ')')) + ');' + self.audit(p, 'validated'))
        self.transaction(sql)
        return {'proposal_id': p.proposal_id, 'state': 'validated', 'observation_sha256': recovery.sha(observation),
                'scenarios_sha256': recovery.sha(scenarios), 'traffic_cutover_allowed': False}

    def activate_scenarios(self, proposal, observation_file, scenarios_file):
        self.authenticated(proposal)
        observation = self.observations.read(observation_file)
        scenarios = self.observations.read_scenarios(scenarios_file, observation)
        for key in ('proposal_id', 'expected_epoch', 'revision', 'checkpoint_sha256'):
            recovery.require(observation[key] == getattr(proposal, key), 'activation proposal mismatch')
        value = self.transaction('SELECT ' + self.schema + '.activate_fenced_v3(' +
                                 ','.join((literal(proposal.proposal_id), literal(recovery.sha(observation)),
                                           literal(recovery.sha(scenarios)))) + ');')
        recovery.require(value == str(proposal.expected_epoch + 1), 'activation epoch mismatch')
        return {'proposal_id': proposal.proposal_id, 'state': 'activated', 'epoch': int(value),
                'region': self.identity.recovery_region, 'automatic_failback': False, 'traffic_cutover_allowed': False,
                'identity': self.identity.diagnostic()}


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--configuration', required=True)
    parser.add_argument('--configuration-sha256', required=True)
    parser.add_argument('--proposal', required=True)
    parser.add_argument('--operation', choices=('propose', 'validate', 'activate', 'reject', 'status'), required=True)
    parser.add_argument('--observations')
    parser.add_argument('--scenarios')
    args = parser.parse_args()
    raw = regular(args.configuration, recovery.MAX_JSON, private=True).read_bytes()
    recovery.require(hashlib.sha256(raw).hexdigest() == args.configuration_sha256, 'operator configuration changed')
    configuration = recovery.load(raw)
    recovery.exact(configuration, {'version', 'identity', 'tools', 'postgres_service_file', 'postgres_service',
                                 'postgres_role', 'observation_directory', 'observation_signing_key'})
    recovery.require(configuration['version'] == 2, 'operator configuration version2 required')
    identity = RecoveryIdentity(**configuration['identity']).validate()
    tools = BoundTools(configuration['tools'])
    database = PostgreSQL(tools, configuration['postgres_service_file'], configuration['postgres_service'],
                          configuration['postgres_role'])
    archive_type = AdversarialArchive if args.scenarios else ObservationArchive
    archive_options = {'native_tools': configuration['tools']} if args.scenarios else {}
    observations = archive_type(configuration['observation_directory'], configuration['observation_signing_key'], identity, **archive_options)
    operator_type = AdversarialOperator if args.scenarios or args.operation == 'status' else RecoveryOperator
    operator = operator_type(database, identity, observations)
    proposal = RecoveryProposal(**recovery.load(regular(args.proposal, 65536).read_bytes())).validate()
    if args.operation in ('activate', 'validate'):
        recovery.require(args.observations is not None, 'fresh independent observations required')
        if args.scenarios:
            result = getattr(operator, args.operation + '_scenarios')(proposal, args.observations, args.scenarios)
        else:
            result = getattr(operator, args.operation)(proposal, args.observations)
    else:
        result = getattr(operator, args.operation)(proposal)
    print(json.dumps(result, sort_keys=True))


if __name__ == '__main__':
    try:
        main()
    except (ValueError, OSError, ProviderFailure) as exc:
        print(json.dumps({'outcome': 'fail', 'error_type': type(exc).__name__,
                          'traffic_cutover_allowed': False}), file=sys.stderr)
        raise SystemExit(1)
