#!/usr/bin/env python3
"""Project pinned generator with explicit versioned bundle and package outputs."""
import argparse
import hashlib
import importlib.util
import json
import pathlib
import sys
import types

sys.dont_write_bytecode = True


def sha(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def local_refs(value):
    if isinstance(value, dict):
        result = {}
        for key, child in value.items():
            if key == '$ref' and isinstance(child, str) and child.startswith('#/components/schemas/'):
                result[key] = '#/$defs/' + child[len('#/components/schemas/'):]
            else:
                result[key] = local_refs(child)
        return result
    if isinstance(value, list):
        return [local_refs(child) for child in value]
    return value



def emit_operations(root, contracts):
    pin = json.loads((contracts / 'codegen-manifest.json').read_bytes())
    if pin.get('schemaMajor') != 2 or sha(contracts / 'schema-definitions.json') != pin.get('schemaSha256'):
        raise ValueError('Pinned v2 definition digest mismatch')
    definitions = json.loads((contracts / 'schema-definitions.json').read_bytes())
    document = json.loads((contracts / 'openapi.json').read_bytes())
    async_document = json.loads((contracts / 'asyncapi.json').read_bytes())
    if async_document.get('asyncapi') != '3.0.0' or local_refs(async_document['components']['schemas']) != definitions['$defs'] or local_refs(document['components']['schemas']) != definitions['$defs']:
        raise ValueError('Authoritative HTTP/stream definition drift')
    if document.get('openapi') != '3.1.0' or not document['info']['version'].startswith('2.'):
        raise ValueError('operation generation requires explicit OpenAPI3.1 v2')
    def ts(schema):
        if '$ref' in schema:
            return 'V2ContractTypes[' + json.dumps(schema['$ref'].split('/')[-1]) + ']'
        if 'enum' in schema:
            return '|'.join(json.dumps(v) for v in schema['enum'])
        if 'anyOf' in schema:
            return '|'.join(ts(v) for v in schema['anyOf'])
        kind = schema.get('type')
        if kind == 'array': return 'Array<' + ts(schema['items']) + '>'
        if kind == 'object':
            return '{' + ';'.join(json.dumps(k) + ('' if k in schema.get('required',[]) else '?') + ':' + ts(v) for k,v in schema.get('properties',{}).items()) + '}'
        if kind in ('integer','number'): return 'number'
        if kind in ('string','boolean','null'): return kind
        raise ValueError('Unsupported operation parameter schema: ' + str(schema))
    rows = {}; types_out = []
    for path, methods in sorted(document['paths'].items()):
        for method, operation in sorted(methods.items()):
            if method not in ('get','post','put','patch','delete'): continue
            name = operation['operationId']
            content = operation.get('requestBody',{}).get('content',{})
            body = content.get('application/json',{}).get('schema')
            binary_body = bool(content) and not body
            binary_response = any(response.get('content') and 'application/json' not in response.get('content',{}) for code,response in operation['responses'].items() if code.startswith('2'))
            successes = [response for code,response in operation['responses'].items() if code.startswith('2')]
            responses = [response.get('content',{}).get('application/json',{}).get('schema') for response in successes]
            response = next((node['$ref'].split('/')[-1] for node in responses if node), None)
            parameters = operation.get('parameters',[])
            fields = []
            for location in ('path','query'):
                selected = [v for v in parameters if v['in'] == location]
                if selected:
                    fields.append(location + ('' if any(v.get('required') for v in selected) else '?') + ':{' + ';'.join(json.dumps(v['name']) + ('' if v.get('required') else '?') + ':' + ts(v['schema']) for v in selected) + '}')
            if binary_body: fields.append('body:Blob')
            if body: fields.append('body' + ('' if operation['requestBody'].get('required') else '?') + ':' + ts(body))
            types_out.append(json.dumps(name) + ':{input:{' + ';'.join(fields) + '};response:' + ('Response' if binary_response else 'V2ContractTypes['+json.dumps(response)+']' if response else 'undefined') + '}')
            service = operation.get('x-service') or ('identity' if path.startswith('/v2/auth/') or path == '/v2/stream/tickets' else 'media' if path.startswith(('/v2/assets','/v2/tokens/','/v2/streams','/v2/provider-webhooks','/v2/voice-rooms','/v2/profiles','/v2/me/watchlist','/v2/me/following')) else 'api')
            security = operation.get('security',[])
            rows[name] = {'path':path,'wireMajor':int(path.split('/')[1][1:]),'binaryBody':binary_body,'binaryResponse':binary_response,'method':method.upper(),'service':service,'authenticated':bool(security) and not any(not v for v in security),'cookieSession':name in ('IF_LOGIN','IF_REFRESH'),'idempotentRead':method=='get' and ('read' in operation.get('x-idempotency','').lower() or name=='IF_TOPIC_SNAPSHOT_READ'),'public':not security and operation.get('x-cache-policy','').startswith('Public'),'timeoutMs':operation['x-timeout-ms'],'body':body['$ref'].split('/')[-1] if body else None,'response':response,'emptyResponse':all(not value.get('content') for value in successes),'parameters':local_refs(parameters),'idempotencyKey':'Idempotency-Key' in operation.get('x-idempotency','')}
    generated = "import type { ContractTypes as V2ContractTypes } from '@kindle/sdk-v2/types';\nexport interface OperationTypes {\n" + ';\n'.join(types_out) + '\n}\nexport const operationDefinitions = ' + json.dumps(rows,sort_keys=True,indent=2) + ' as const;\n'
    target = root / 'client/packages/sdk/src/operations.gen.ts'
    target.write_text(generated)
    return {'generatorSha256':sha(pathlib.Path(__file__)), 'openapiSha256':sha(contracts/'openapi.json'),'outputSha256':sha(target),'operationCount':len(rows)}


def main():
    root = pathlib.Path(__file__).resolve().parents[4]
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--major', type=int, default=1)
    parser.add_argument('--contracts', type=pathlib.Path)
    parser.add_argument('--manifest', type=pathlib.Path)
    parser.add_argument('--package', type=pathlib.Path)
    parser.add_argument('--generator', type=pathlib.Path)
    parser.add_argument('--reproduce-v1', action='store_true')
    parser.add_argument('--operations-only', action='store_true')
    args = parser.parse_args()
    if args.major < 1:
        raise ValueError('positive schema major required')
    contracts = (args.contracts or root / ('spec/production-system/integration/contracts/v' + str(args.major))).resolve()
    if args.operations_only:
        if args.major != 2: raise ValueError('--operations-only requires --major2')
        print(json.dumps(emit_operations(root, contracts), sort_keys=True)); return
    manifest_path = (args.manifest or contracts / 'codegen-manifest.json').resolve()
    package = (args.package or pathlib.Path(__file__).resolve().parents[1]).resolve()
    if not contracts.is_relative_to(root / 'spec/production-system/integration/contracts') or not manifest_path.is_relative_to(contracts):
        raise ValueError('bundle/manifest outside contract allowlist')
    expected_package = root / ('client/packages/sdk' if args.major == 1 else 'client/packages/sdk-v' + str(args.major))
    if package != expected_package:
        raise ValueError('explicit versioned SDK package required; frozen SDK must not be overwritten')
    manifest = json.loads(manifest_path.read_bytes())
    generator = (args.generator or root / manifest['generator']).resolve()
    legacy_source = None
    if args.reproduce_v1:
        if args.major != 1:
            raise ValueError('--reproduce-v1 requires major1')
        current_spec = importlib.util.spec_from_file_location('versioned_codegen', generator)
        current_module = importlib.util.module_from_spec(current_spec)
        current_spec.loader.exec_module(current_module)
        legacy_source = current_module.frozen_v1_generator()
    actual_generator_sha = hashlib.sha256(legacy_source).hexdigest() if legacy_source is not None else sha(generator)
    if actual_generator_sha != manifest['generatorSha256']:
        raise ValueError('Pinned generator digest mismatch; use explicit --reproduce-v1 or supply the immutable generator')
    if sha(generator.with_name('validator.go.txt')) != manifest['validatorTemplateSha256']:
        raise ValueError('Pinned validator template mismatch')
    if args.major != 1 and manifest.get('schemaMajor') != args.major:
        raise ValueError('versioned codegen manifest major mismatch')
    schema_path = contracts / 'schema-definitions.json'
    if sha(schema_path) != manifest['schemaSha256']:
        raise ValueError('Pinned schema input mismatch')
    document = json.loads(schema_path.read_bytes())
    transport_versions = []
    for filename in ('openapi.json', 'asyncapi.json'):
        source = json.loads((contracts / filename).read_bytes())
        transport_versions.append(source['info']['version'])
        if int(source['info']['version'].split('.')[0]) != args.major:
            raise ValueError(filename + ': API/schema major mismatch')
        if local_refs(source['components']['schemas']) != document['$defs']:
            raise ValueError(filename + ': schema drift')
    if len(set(transport_versions)) != 1:
        raise ValueError('transport document versions differ')
    if legacy_source is not None:
        module = types.ModuleType('pinned_codegen')
        module.__file__ = str(generator)
        exec(compile(legacy_source, str(generator), 'exec'), module.__dict__)
    else:
        module_spec = importlib.util.spec_from_file_location('pinned_codegen', generator)
        module = importlib.util.module_from_spec(module_spec)
        module_spec.loader.exec_module(module)
    for name, node in document['$defs'].items():
        module.validate_schema(node, document['$defs'], name)
    if args.major == 1:
        outputs = module.Generator(document).emit()
    else:
        go_outs = {str(pathlib.PurePosixPath(name).parent) for name in manifest['outputs'] if name.endswith('.go')}
        ts_outs = {str(pathlib.PurePosixPath(name).parent) for name in manifest['outputs'] if name.endswith(('.ts', '.json'))}
        if len(go_outs) != 1 or len(ts_outs) != 1:
            raise ValueError('manifest must declare exactly one Go and TypeScript generated root')
        outputs = module.Generator(document, major=args.major, go_package=manifest['goPackage'], go_out=next(iter(go_outs)), ts_out=next(iter(ts_outs)), format_output=manifest['formatOutput']).emit()
    if set(outputs) != set(manifest['outputs']):
        raise ValueError('Pinned generated output inventory mismatch')
    for name, content in outputs.items():
        if hashlib.sha256(content.encode()).hexdigest() != manifest['outputs'][name]:
            raise ValueError('Pinned generated output mismatch: ' + name)
    ts_root = 'client/packages/production-contracts/src' if args.major == 1 else next(str(pathlib.PurePosixPath(name).parent) for name in outputs if name.endswith('types.gen.ts'))
    package.joinpath('src').mkdir(parents=True, exist_ok=True)
    for filename in ('types.gen.ts', 'schemas.gen.json'):
        (package / 'src' / filename).write_text(outputs[ts_root + '/' + filename])
    release = {'apiMajor': args.major, 'streamMajor': args.major, 'streamSubprotocol': 'sidiora.v' + str(args.major), 'generatorSha256': manifest['generatorSha256'], 'inputs': {name: sha(contracts / name) for name in ('openapi.json', 'asyncapi.json', 'schema-definitions.json')}, 'outputs': {name: sha(package / 'src' / name) for name in ('types.gen.ts', 'schemas.gen.json')}}
    (package / 'release-manifest.json').write_text(json.dumps(release, sort_keys=True, indent=2) + '\n')
    print(json.dumps(release, sort_keys=True))


if __name__ == '__main__':
    try:
        main()
    except (ValueError, OSError, KeyError) as error:
        print('sdk-codegen: ' + str(error), file=sys.stderr)
        raise SystemExit(1)
