from __future__ import annotations

import argparse
import importlib.util
import json
import pathlib
import sys

from jsonschema.exceptions import SchemaError, ValidationError

_spec = importlib.util.spec_from_file_location('feature_readiness_policy', pathlib.Path(__file__).with_name('policy.py'))
policy = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(policy)


def read_json(path):
    with pathlib.Path(path).open('rb') as source:
        return policy.strict_json(source.read(policy.MAX_JSON+1))


def main(argv=None):
    parser = argparse.ArgumentParser()
    parser.add_argument('--mode', required=True, choices=['readiness', 'closure'])
    parser.add_argument('--owner-results', required=True)
    parser.add_argument('--coordinator-context', required=True)
    parser.add_argument('--review-assignments')
    parser.add_argument('--gate-results')
    parser.add_argument('--output', required=True)
    args = parser.parse_args(argv)
    try:
        invocation = read_json(args.coordinator_context)
        expected = {'readiness_authority', 'inventory', 'context', 'trust', 'artifact_root', 'dependencies', 'authority', 'audit_authority', 'now'}
        policy.keys(invocation, expected | ({'closure_authority'} if args.mode == 'closure' else set()), 'trusted phase4 invocation')
        policy.require(policy.integer(invocation['now']), 'trusted current clock required')
        bundles = read_json(args.owner_results)
        if args.mode == 'readiness':
            policy.require(args.review_assignments is not None and args.gate_results is None, 'exact readiness assignment input required')
            result = policy.validator_readiness(bundles, read_json(args.review_assignments), **invocation)
        else:
            policy.require(args.gate_results is not None and args.review_assignments is None, 'exact final integrated gate input required')
            result = policy.final_release_closure(bundles, read_json(args.gate_results), **invocation)
        output = pathlib.Path(args.output)
        output.parent.mkdir(parents=True, exist_ok=True)
        with output.open('x', encoding='utf-8') as target:
            target.write(json.dumps(result, indent=2, allow_nan=False)+'\n')
        return 0
    except (ValueError, TypeError, KeyError, AttributeError, RecursionError, ValidationError) as exc:
        print(str(exc), file=sys.stderr)
        return 2
    except (OSError, SchemaError) as exc:
        print(str(exc), file=sys.stderr)
        return 3
    except Exception as exc:
        print('validator execution failed: '+str(exc), file=sys.stderr)
        return 3


if __name__ == '__main__':
    raise SystemExit(main())
