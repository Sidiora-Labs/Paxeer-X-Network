import copy
import json
from pathlib import Path
import sys
import unittest

ROOT = Path(__file__).resolve().parent
sys.path.insert(0, str(ROOT))
import contracts as v1
import contracts_v2 as v2


class InventoryV2(unittest.TestCase):
    def setUp(self):
        self.config = json.loads((ROOT / 'config-v2.json').read_text())

    def test_selected_twelve_roles_and_explicit_version(self):
        self.assertIs(v2.validate(self.config), self.config)
        self.assertEqual(len(self.config['services']), 12)
        self.assertEqual(self.config['services'], list(v2.SERVICES))
        for role in self.config['services']:
            with self.subTest(role=role):
                self.assertEqual(v2.safe_labels({'service': role}, self.config), ({'service': role}, 0))
        legacy = json.loads((ROOT / 'config.json').read_text())
        self.assertIs(v1.validate(legacy), legacy)
        with self.assertRaises(v1.Invalid):
            v1.validate(self.config)
        with self.assertRaises(v2.Invalid):
            v2.validate(legacy)

    def test_inventory_must_not_omit_duplicate_or_invent_roles(self):
        for services in [list(v1.SERVICES), list(v2.SERVICES[:-1]), list(v2.SERVICES) + ['notifications'], [v2.SERVICES[0]] * 12, list(reversed(v2.SERVICES))]:
            with self.subTest(services=services):
                config = copy.deepcopy(self.config)
                config['services'] = services
                with self.assertRaises(v2.Invalid):
                    v2.validate(config)

    def test_operational_bounds_and_proof_requirements_remain_strict(self):
        mutations = [
            lambda c: c.update(extra=True),
            lambda c: c.pop('exporter'),
            lambda c: c.update(version='1.0'),
            lambda c: c.update(revision=True),
            lambda c: c.update(series_per_instance=20001),
            lambda c: c.update(success_sample_percent=True),
            lambda c: c.update(failure_queue_mib=513),
            lambda c: c.update(retention_days=31),
            lambda c: c.update(zones=['wallet-derived-zone']),
            lambda c: c.update(zones=[['zone-a']]),
            lambda c: c['exporter'].update(endpoint_env='UNREVIEWED_ENDPOINT'),
            lambda c: c['probes'][0].update(economic_write=True),
            lambda c: c['probes'][0].update(timeout_seconds=6),
            lambda c: c['alerts'][0].update(owner=''),
            lambda c: c['alerts'][0].update(ack_seconds=301),
            lambda c: c['capacity'].update(status='qualified'),
            lambda c: c['capacity'].update(evidence='declaration'),
        ]
        for mutate in mutations:
            config = copy.deepcopy(self.config)
            mutate(config)
            with self.subTest(config=config), self.assertRaises(v2.Invalid):
                v2.validate(config)

    def test_private_identifiers_and_unknown_values_never_become_labels(self):
        marker = 'DO_NOT_EXPORT_PRIVATE_VALUE'
        safe, dropped = v2.safe_labels({'service': 'apid_rest', 'route': '/pool/' + marker, 'wallet': marker, 'pool': marker, 'raw_url': marker, 'error_code': marker}, self.config)
        self.assertEqual(safe, {'service': 'apid_rest'})
        self.assertEqual(dropped, 5)
        record = v2.sanitize_record({'service': 'apid_sockets', 'zone': marker, 'stage': marker, 'authorization': marker, 'body': marker, 'signature': marker, 'stream_key': marker})
        self.assertEqual(record, {'service': 'apid_sockets'})
        self.assertNotIn(marker, json.dumps(record))
        invalid = copy.deepcopy(self.config)
        invalid['zones'] = [marker]
        with self.assertRaises(v2.Invalid):
            v2.safe_labels({'zone': marker}, invalid)

    def test_failed_commit_and_unproven_activation_are_never_success(self):
        for mask in range(16):
            decisions = [bool(mask & (1 << i)) for i in range(4)]
            self.assertEqual(v2.mutation_good(*decisions), mask == 15)
        for state in ['proposed', 'validated']:
            with self.assertRaises(v2.Invalid):
                v2.transition(state, 'activate', 'operator', 1, validated=True)
        self.assertEqual(v2.transition('proposed', 'validate', 'operator', 1, validated=True)['state'], 'validated')


if __name__ == '__main__':
    unittest.main(verbosity=2)
