from __future__ import annotations

from dataclasses import dataclass
import hashlib
import json
from pathlib import Path
import re
import sys

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
import recovery


@dataclass(frozen=True)
class RecoveryIdentity:
    source_sha256: str
    interface_sha256: str
    deployment_sha256: str
    inventory_sha256: str
    environment: str
    service: str
    primary_region: str
    recovery_region: str
    chain_id: int
    addresses: dict[str, str]

    def validate(self):
        for value in (self.source_sha256, self.interface_sha256, self.deployment_sha256, self.inventory_sha256):
            recovery.digest(value)
            recovery.require(value != '0' * 64, 'nonzero provenance required')
        for value in (self.environment, self.service, self.primary_region, self.recovery_region):
            recovery.name(value)
        recovery.require(type(self.chain_id) is int and self.chain_id == 125, 'chain125 required')
        recovery.require(self.primary_region != self.recovery_region, 'independent recovery region required')
        recovery.require(isinstance(self.addresses, dict) and 1 <= len(self.addresses) <= 128,
                         'deployment address inventory required')
        for name, address in self.addresses.items():
            recovery.name(name)
            recovery.require(isinstance(address, str) and re.fullmatch(r'0x[0-9a-fA-F]{40}', address)
                             and int(address[2:], 16) != 0, 'invalid chain125 address')
        return self

    def diagnostic(self):
        self.validate()
        return dict(version=2, service=self.service, environment=self.environment,
                    primary_region=self.primary_region, recovery_region=self.recovery_region,
                    chain_id=self.chain_id, addresses=dict(sorted(self.addresses.items())),
                    deployment_sha256=self.deployment_sha256, source_sha256=self.source_sha256,
                    interface_sha256=self.interface_sha256, inventory_sha256=self.inventory_sha256)


def render(configuration, resource_declarations, identity, secret_references):
    recovery.configuration(configuration)
    recovery.resources(resource_declarations)
    identity.validate()
    recovery.require(identity.environment == configuration['environment'] and
                     identity.primary_region == configuration['primary_region'] and
                     identity.recovery_region == configuration['recovery_region'], 'configuration identity mismatch')
    expected = {'backup', 'restore', 'retention', 'application', 'coordinator', 'evidence-signing'}
    recovery.require(isinstance(secret_references, dict) and set(secret_references) == expected,
                     'exact independent secret reference inventory required')
    for value in secret_references.values():
        recovery.require(isinstance(value, str) and re.fullmatch(r'secret://[a-z0-9-]+/[a-z0-9/-]{1,128}', value),
                         'secret references only')
    recovery.require(len(set(secret_references.values())) == len(expected), 'independent credentials required')
    result = dict(version=2, identity=identity.diagnostic(), configuration=configuration,
                  resources=resource_declarations, secret_references=secret_references,
                  state='proposed', traffic_cutover_allowed=False)
    result['configuration_sha256'] = hashlib.sha256(recovery.canonical(result)).hexdigest()
    return result


def load_identity(path, expected_sha256):
    recovery.digest(expected_sha256)
    path = Path(path)
    recovery.require(path.is_file() and not path.is_symlink(), 'regular identity document required')
    raw = path.read_bytes()
    recovery.require(hashlib.sha256(raw).hexdigest() == expected_sha256, 'identity digest mismatch')
    value = recovery.load(raw)
    return RecoveryIdentity(**value).validate()


def render_adversarial(configuration, resource_declarations, identity, secret_references, scenario_plans):
    base = render(configuration, resource_declarations, identity, secret_references)
    recovery.exact(scenario_plans, recovery.METRICS)
    references = set()
    for requirement, plan in scenario_plans.items():
        recovery.exact(plan, {'executable_sha256', 'configuration_reference', 'configuration_sha256',
                             'inputs_sha256', 'timeout_seconds'})
        for key in ('executable_sha256', 'configuration_sha256', 'inputs_sha256'):
            recovery.digest(plan[key]); recovery.require(plan[key] != '0'*64, 'unbound adversarial scenario')
        reference = plan['configuration_reference']
        recovery.require(isinstance(reference, str) and re.fullmatch(r'secret://[a-z0-9-]+/[a-z0-9/-]{1,128}', reference),
                         'private scenario configuration reference required')
        recovery.require(reference not in references, 'scenario configuration identity reused'); references.add(reference)
        recovery.integer(plan['timeout_seconds'], 1, 1800)
    result = dict(version=3, realm='isolated-recovery-qualification', state='proposed',
                  identity=identity.diagnostic(), baseline=base, scenario_plans=scenario_plans,
                  required_native_requirements=sorted(recovery.METRICS),
                  activation_function='recovery.activate_fenced_v3',
                  observation_digest_required=True, scenarios_digest_required=True,
                  automatic_failback=False, traffic_cutover_allowed=False, capacity_qualified=False)
    result['configuration_sha256'] = recovery.sha(result)
    return result


def load_operator():
    import importlib.util
    name = 'kindle_recovery_operator_v2'
    if name not in sys.modules:
        spec = importlib.util.spec_from_file_location(name, Path(__file__).with_name('operator.py'))
        module = importlib.util.module_from_spec(spec)
        sys.modules[name] = module
        spec.loader.exec_module(module)
    return sys.modules[name]
