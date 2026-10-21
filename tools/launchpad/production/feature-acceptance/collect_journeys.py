from __future__ import annotations

import argparse
import importlib.util
import json
import pathlib
import sys

_spec = importlib.util.spec_from_file_location('journey_policy', pathlib.Path(__file__).with_name('journey_policy.py'))
journey = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(journey)
policy = journey.policy


def collect(bundles, *, inventory, context, trust, artifact_root, dependencies, authority, now, audit_authority=None):
    features = policy.validate_inventory(inventory)
    policy.keys(context, policy.BINDINGS | {'environment', 'implementation_principals', 'run_id', 'harness'}, 'trusted collection context')
    policy.require(all(type(context[k]) is str and policy.HEX.fullmatch(context[k]) for k in policy.BINDINGS), 'collection candidate digest invalid')
    policy.require(context['inventory'] == policy.digest(inventory), 'collection inventory mismatch')
    policy.require(policy.integer(now) and all(policy.text(context[k]) for k in ['environment', 'run_id', 'harness']), 'collection clock/identity invalid')
    policy.require(type(context['implementation_principals']) is list and all(policy.text(p) for p in context['implementation_principals']), 'collection implementation identities invalid')
    policy.keys(authority, {'providers', 'settlements', 'native_builds'}, 'trusted collection authority')
    policy.require(all(type(v) is dict for v in authority.values()) and type(dependencies) is dict and type(trust) is dict, 'collection authority maps invalid')
    policy.require(type(bundles) is list and len(bundles) <= 55, 'owner evidence collection invalid')
    if audit_authority is not None:
        policy.require(type(audit_authority) is dict and set(audit_authority) == policy.FEATURES, 'audit authority exact feature coverage required')
    supplied = {}
    for bundle in bundles:
        policy.keys(bundle, {'feature', 'evidence', 'review', 'journey'} | ({'audit'} if audit_authority is not None else set()), 'owner result bundle')
        feature = bundle['feature']
        policy.require(feature in features and feature not in supplied, 'duplicate/unknown owner feature')
        supplied[feature] = bundle
    results, blockers = [], []
    for feature, item in sorted(features.items()):
        result = {'feature': feature, 'owner_lanes': item['owner_lanes'], 'requirement_owner_lanes': item['requirement_owner_lanes'], 'requirements': item['requirements'], 'state': 'pending', 'evidence_sha256': None, 'release_qualified': False}
        missing = [d for d in item['prerequisites'] if dependencies.get(d['id']) is not True]
        if missing:
            result['state'] = 'blocked'
            for dependency in missing:
                blockers.append({'feature': feature, 'owner': dependency['owner'], 'dependency': dependency['id'], 'evidence': None, 'remediation': dependency['remediation'], 'review_date': now, 'reason': 'current admitted dependency proof missing'})
        elif feature in supplied:
            bundle = supplied[feature]
            try:
                arguments = dict(inventory=inventory, context=context, trust=trust, artifact_root=artifact_root, dependencies=dependencies, authority=authority, now=now)
                if audit_authority is None:
                    qualified = journey.qualify_feature(feature, bundle['evidence'], bundle['review'], bundle['journey'], **arguments)
                else:
                    qualified = journey.qualify_audited_feature(feature, bundle['evidence'], bundle['review'], bundle['journey'], bundle['audit'], audit_authority=audit_authority[feature], **arguments)
                    result.update(audit_sha256=qualified['audit_sha256'], adversarial_checks=qualified['adversarial_checks'])
                result.update(state='passed', evidence_sha256=qualified['evidence_sha256'])
            except (policy.Rejected, TypeError, KeyError, ValueError, OSError) as exc:
                result['state'] = 'failed'
                blockers.append({'feature': feature, 'owner': item['owner_lanes'][0], 'dependency': feature+':actual_journey_evidence', 'evidence': None, 'remediation': 'Supply actual current candidate owner execution and exact independently reviewed authoritative artifacts', 'review_date': now, 'reason': str(exc)})
        else:
            blockers.append({'feature': feature, 'owner': item['owner_lanes'][0], 'dependency': feature+':owner_result', 'evidence': None, 'remediation': 'Execute the registered real feature scenarios and submit signed current owner results', 'review_date': now, 'reason': 'owner execution evidence absent'})
        results.append(result)
    return {'version': 3 if audit_authority is not None else 2, 'task': '3.50' if audit_authority is not None else '2.50', 'candidate': context['source'], 'bindings': {k: context[k] for k in sorted(policy.BINDINGS)}, 'environment': context['environment'], 'run_id': context['run_id'], 'harness': context['harness'], 'generated_at': now, 'inventory_sha256': policy.digest(inventory), 'features': results, 'blockers': blockers, 'complete_feature_policy_coverage': all(r['state'] == 'passed' for r in results), 'release_qualified': False}


def read_json(path):
    with pathlib.Path(path).open('rb') as handle:
        return policy.strict_json(handle.read(policy.MAX_JSON+1))


def main(argv=None):
    parser = argparse.ArgumentParser()
    parser.add_argument('--owner-results', required=True)
    parser.add_argument('--coordinator-context', required=True)
    parser.add_argument('--output', required=True)
    parser.add_argument('--phase', type=int, choices=[2, 3], default=2)
    args = parser.parse_args(argv)
    try:
        invocation = read_json(args.coordinator_context)
        policy.keys(invocation, {'inventory', 'context', 'trust', 'artifact_root', 'dependencies', 'authority', 'now'} | ({'audit_authority'} if args.phase == 3 else set()), 'trusted collector invocation')
        if args.phase == 3:
            policy.require(type(invocation['audit_authority']) is dict, 'phase3 audit authority required')
        policy.require(policy.integer(invocation['now']), 'trusted clock required')
        result = collect(read_json(args.owner_results), **invocation)
        output = pathlib.Path(args.output)
        output.parent.mkdir(parents=True, exist_ok=True)
        output.write_text(json.dumps(result, indent=2, allow_nan=False)+'\n')
        return 0 if result['complete_feature_policy_coverage'] else 2
    except (policy.Rejected, TypeError, KeyError, ValueError) as exc:
        print(str(exc), file=sys.stderr)
        return 2
    except OSError as exc:
        print(str(exc), file=sys.stderr)
        return 3


if __name__ == '__main__':
    raise SystemExit(main())
