#!/usr/bin/env python3
"""Deterministic SYNTHETIC_SCHEMA_EXAMPLES, never handler/receipt evidence.

Generate with --write; --check validates and compares without writing. This is
schema placement and instance evidence only: accounting and business-state truth
require independent qualification. No network access or user transactions.
"""
import argparse
import copy
import hashlib
import importlib.util
import json
import pathlib
import re
import sys
import tomllib
# Read-only checks must not create Python bytecode beside the materializer.
sys.dont_write_bytecode = True
from jsonschema import Draft202012Validator, FormatChecker, RefResolver, ValidationError

LABEL = 'SYNTHETIC_SCHEMA_EXAMPLES_NOT_HANDLER_OR_RECEIPT_PROOF'
UUID = '00000000-0000-7000-8000-000000000001'
ADDRESS = '0x' + '1' * 40
UTC = '2026-10-09T00:00:00.000Z'
class ExampleError(ValueError): pass

def encode(value):
    return (json.dumps(value, sort_keys=True, indent=2, ensure_ascii=False) + '\n').encode()

def load_materializer(path):
    spec = importlib.util.spec_from_file_location('example_materializer', path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module

class Samples:
    def __init__(self, document, api_base="/v1"):
        if not re.fullmatch(r"/v[1-9][0-9]*",api_base):raise ExampleError("invalid configured API base")
        self.api_base=api_base
        self.document = document
        self.defs = document['$defs']
        self.formats = FormatChecker()
    def validate(self, schema, value):
        resolver = RefResolver.from_schema(self.document, handlers={'http': self.no_network, 'https': self.no_network})
        Draft202012Validator(schema, resolver=resolver, format_checker=self.formats).validate(value)
    @staticmethod
    def no_network(uri):
        raise ExampleError('External schema reference forbidden: ' + uri)
    def sample(self, schema, proof='eoa_eip191', stack=()):
        if not isinstance(schema, dict) or not schema:
            raise ExampleError('Unconstrained/boolean schema cannot generate an example')
        if '$ref' in schema:
            name = schema['$ref'].split('/')[-1]
            if not schema['$ref'].startswith('#/$defs/') or name not in self.defs:
                raise ExampleError('Unknown local schema reference: ' + schema['$ref'])
            if name in stack:
                raise ExampleError('Required recursive schema needs explicit fragment: ' + name)
            target = self.defs[name]
            if name in ('WalletProof', 'InputWalletProof'):
                name = ('Input' if name.startswith('Input') else '') + ('EoaWalletProof' if proof == 'eoa_eip191' else 'Erc1271WalletProof')
                target = self.defs[name]
            return self.sample(target, proof, stack + (name,))
        if 'const' in schema: value = schema['const']
        elif 'enum' in schema: value = schema['enum'][0]
        elif 'oneOf' in schema or 'anyOf' in schema:
            branches = schema.get('oneOf', schema.get('anyOf'))
            failures = []
            for branch in branches:
                try:
                    value = self.sample(branch, proof, stack)
                    self.validate(schema, value)
                    return value
                except (ValueError, RecursionError, ValidationError) as error: failures.append(str(error))
            raise ExampleError('No declared union branch sample: ' + repr(stack) + ': ' + '; '.join(failures)[:400])
        elif isinstance(schema.get('type'), list):
            for typ in schema['type']:
                candidate = dict(schema, type=typ)
                try: return self.sample(candidate, proof, stack)
                except ExampleError: pass
            raise ExampleError('No type-union sample: ' + repr(schema))
        elif schema.get('type') == 'null': value = None
        elif schema.get('type') == 'boolean': value = False
        elif schema.get('type') in ('integer', 'number'):
            value = max(0, schema.get('minimum', 0))
            if 'exclusiveMinimum' in schema: value = max(value, schema['exclusiveMinimum'] + 1)
        elif schema.get('type') == 'object':
            if 'properties' not in schema: raise ExampleError('Object properties not declared')
            # Include all optional properties so placement is exercised as well.
            value = {name: self.sample(shape, proof, stack) for name, shape in sorted(schema['properties'].items())}
        elif schema.get('type') == 'array':
            if 'items' not in schema: raise ExampleError('Array items not declared')
            count = max(1, schema.get('minItems', 0)) if schema.get('maxItems', 1) else 0
            value = [self.sample(schema['items'], proof, stack) for _ in range(count)]
            if schema.get('uniqueItems') and count > 1: raise ExampleError('Unique multi-item array requires explicit fragment')
        elif schema.get('type') == 'string': value = self.string(schema)
        else: raise ExampleError('Unsupported schema shape: ' + repr(schema))
        self.validate(schema, value)
        return value
    def string(self, schema):
        pattern = schema.get('pattern')
        manual = {
            '^(?:(?:000[1-9]|00[1-9][0-9]|0[1-9][0-9]{2}|[1-9][0-9]{3})-(?:(?:01|03|05|07|08|10|12)-(?:0[1-9]|[12][0-9]|3[01])|(?:04|06|09|11)-(?:0[1-9]|[12][0-9]|30)|02-(?:0[1-9]|1[0-9]|2[0-8]))|(?:[0-9]{2}(?:0[48]|[2468][048]|[13579][26])|(?:0[48]|[2468][048]|[13579][26])00)-02-29)T(?:[01][0-9]|2[0-3]):[0-5][0-9]:[0-5][0-9]\\.[0-9]{3}Z$': UTC,
            '^https://': 'https://example.invalid/synthetic',
            r'^https://[A-Za-z0-9](?:[A-Za-z0-9.-]*[A-Za-z0-9])?(?::[0-9]{1,5})?$': 'https://example.invalid',
            r'^https://[A-Za-z0-9](?:[A-Za-z0-9.-]*[A-Za-z0-9])?(?::[0-9]{1,5})?(?:[/?#][^\s]*)?$': 'https://example.invalid/synthetic',
            r'^\d{4}-\d{2}-\d{2}T(?:[01][0-9]|2[0-3]):[0-5][0-9]:[0-5][0-9]\.\d{3}Z$': UTC,
            r'^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-7[0-9a-fA-F]{3}-[89abAB][0-9a-fA-F]{3}-[0-9a-fA-F]{12}$': UUID,
            r'^0x(?:[0-9a-fA-F]{2})*$': '0x11',
            '^[0-9a-fA-F]{64}$': '1' * 64,
            '^[0-9a-f]{64}$': '1' * 64,
            '^[A-Z][A-Z0-9_]*$': 'SYNTHETIC',
            '^[A-Za-z0-9_.-]+$': 'synthetic.cursor',
            '^[A-Za-z]{2,8}(?:-[A-Za-z0-9]{1,8})*$': 'en',
            '^(UTC|[A-Za-z_+-]+(?:/[A-Za-z0-9_+.-]+)+)$': 'UTC',
            '^'+re.escape(self.api_base)+'/[A-Za-z0-9_/:?&=.%,-]*$': self.api_base+'/synthetic',
        }
        if pattern:
            if pattern in manual: value = manual[pattern]
            elif re.fullmatch(r'\^0x\[0-9a-fA-F\]\{(40|64|130)\}\$', pattern):
                value = '0x' + '1' * int(re.search(r'\{(\d+)\}', pattern)[1])
            elif pattern.startswith(('^(?:0|[1-9][0-9]', '^(?:[1-9][0-9]', '^(0|[1-9][0-9]', '^(0|-?[1-9][0-9]', '^[1-9][0-9]', '^-?(?:0|[1-9][0-9]')):
                value = '1'
            elif pattern.startswith(('^market\\.', '^private\\.', '^rooms\\.', '^streams\\.', '^bounties\\.')):
                candidates = [
                    'market.price.'+ADDRESS, 'market.stats.'+ADDRESS, 'market.trades.'+ADDRESS,
                    'market.candles.'+ADDRESS+'.1m', 'market.rankings.trending',
                    *['private.'+kind+'.'+ADDRESS for kind in ('notifications','orders','portfolio','rewards')],
                    'rooms.'+UUID+'.messages', 'streams.'+UUID+'.status', 'bounties.'+UUID+'.updates',
                ]
                matches = [candidate for candidate in candidates if re.search(pattern, candidate)]
                if len(matches) != 1: raise ExampleError('Ambiguous stream pattern needs fragment: ' + pattern)
                value = matches[0]
            else: raise ExampleError('Unknown pattern needs explicit synthetic fragment: ' + pattern)
        elif schema.get('format') in ('uuid', 'date-time', 'uri', 'hostname'):
            value = {'uuid':UUID, 'date-time':UTC, 'uri':'https://example.invalid/synthetic','hostname':'example.invalid'}[schema['format']]
        elif schema.get('format'): raise ExampleError('Unsupported string format: ' + schema['format'])
        else: value = 's' * max(1, schema.get('minLength', 1))
        if len(value) < schema.get('minLength', 0): value += 'x' * (schema['minLength']-len(value))
        if len(value) > schema.get('maxLength', len(value)): raise ExampleError('Manual string violates maxLength: '+repr(schema))
        return value

def local(schema):
    if isinstance(schema,dict):
        return {key: value.replace('#/components/schemas/', '#/$defs/') if key == '$ref' else local(value) for key,value in schema.items()}
    if isinstance(schema,list): return [local(value) for value in schema]
    return schema

def validate_placement(samples, operation, instance):
    if set(instance) != {'path','query','header','cookie','body'}: raise ExampleError('Request location set differs')
    expected = {location:{} for location in ('path','query','header','cookie')}
    for parameter in operation.get('parameters',[]): expected[parameter['in']][parameter['name']] = parameter
    for location, parameters in expected.items():
        if set(instance[location]) != set(parameters): raise ExampleError('Request placement mismatch: ' + location)
        for name, parameter in parameters.items(): samples.validate(local(parameter['schema']), instance[location][name])
    body = operation.get('requestBody', {}).get('content', {}).get('application/json', {}).get('schema')
    if body: samples.validate(local(body), instance['body'])
    elif instance['body'] is not None: raise ExampleError('Undeclared body')

def reject_pagination_duplicate(body):
    # ResponseEnvelope owns pagination. Explicit duplicated domain cursors are not
    # allowed even though additive domain fields generally remain compatible.
    if isinstance(body,dict) and isinstance(body.get('data'),dict):
        duplicates = {'nextCursor','cursor','pagination'}.intersection(body['data'])
        if duplicates: raise ExampleError('Undeclared nested pagination duplicate: '+','.join(sorted(duplicates)))

def compatibility_cases(samples):
    cases=[]
    quote=samples.sample({'$ref':'#/$defs/QuoteRequest'})
    numeric=copy.deepcopy(quote);numeric['inputRaw']=9007199254740993
    cases.append(('amount_number', 'QuoteRequest', numeric, False, False))
    login=samples.sample({'$ref':'#/$defs/LoginBody'})
    missing=copy.deepcopy(login);del missing['challengeId']
    cases.append(('missing_required', 'LoginBody', missing, False, False))
    unknown=copy.deepcopy(login);unknown['unknownMutationField']=True
    cases.append(('unknown_mutation_field', 'LoginBody', unknown, False, False))
    response=samples.sample({'$ref':'#/$defs/PoolListResponse'})
    additive=copy.deepcopy(response);additive['futureEnvelopeField']='compatible';additive['data']['futureDomainField']='compatible'
    cases.append(('additive_response_compatible', 'PoolListResponse', additive, True, True))
    duplicate=copy.deepcopy(response);duplicate['data']['nextCursor']='legacy.cursor'
    cases.append(('undeclared_pagination_duplicate', 'PoolListResponse', duplicate, True, False))
    result=[]
    for name,schema,value,schema_expected,semantic_expected in cases:
        schema_valid=True
        try:samples.validate({'$ref':'#/$defs/'+schema},value)
        except ValidationError:schema_valid=False
        semantic_valid=schema_valid
        if schema_valid:
            try:reject_pagination_duplicate(value)
            except ExampleError:semantic_valid=False
        if (schema_valid,semantic_valid)!=(schema_expected,semantic_expected):raise ExampleError('Compatibility case expectation failed: '+name)
        result.append({'id':name,'schema':schema,'instance':value,'expectedSchemaValidity':schema_expected,'expectedEnvelopePolicyValidity':semantic_expected})
    return result

def generate(spec, directory, materializer, major=1, api_base=None):
    documents = {name:json.loads((directory/name).read_bytes()) for name in ('schema-definitions.json','catalog-map.json','openapi.json')}
    mapping=documents['catalog-map.json']; api=documents['openapi.json']; samples=Samples(documents['schema-definitions.json'],api_base or '/v'+str(major))
    digest=materializer.semantic_digest(spec)
    if mapping['inputs']['catalogSha256'] != digest: raise ExampleError('Materialized catalog semantic hash is stale')
    if set(mapping['endpoints']) != set(spec['endpoint']): raise ExampleError('Catalog endpoint coverage differs')
    records={}
    for identifier, endpoint in sorted(mapping['endpoints'].items()):
        operation=api['paths'][endpoint['path']][endpoint['method'].lower()]
        if operation['operationId'] != identifier: raise ExampleError('Operation ID mismatch: '+identifier)
        status=str(spec['endpoint'][identifier]['success_status'])
        if status not in operation['responses']: raise ExampleError('Missing declared success status '+identifier)
        variants=[]
        for proof in ('eoa_eip191','erc1271'):
            request={'path':{},'query':{},'header':{},'cookie':{},'body':None}
            for parameter in operation.get('parameters',[]):
                request[parameter['in']][parameter['name']]=samples.sample(local(parameter['schema']),proof)
            body=operation.get('requestBody',{}).get('content',{}).get('application/json',{}).get('schema')
            if body:request['body']=samples.sample(local(body),proof)
            validate_placement(samples,operation,request)
            # Independently validate assembled request against its DTO. Headers
            # absent from DTO are transport-only and checked above, never body.
            assembled={**request['path'],**request['query'],**(request['body'] or {})}
            dto={key:value for key,value in assembled.items() if key in samples.defs[endpoint['request']]['properties']}
            samples.validate({'$ref':'#/$defs/'+endpoint['request']},dto)
            if variants and request==variants[0]['request']:continue
            variants.append({'proofVariant':proof,'request':request})
        successes=[]
        if endpoint['wireMode']=='empty':
            if status!='204' or operation['responses'][status].get('content'):raise ExampleError('Empty response must be undeclared-content 204')
            samples.validate({'$ref':'#/$defs/'+endpoint['response']},None)
            successes.append({'status':204,'body':None,'bodyBytes':0})
        elif endpoint['wireMode']=='sse':
            for streamid, stream in sorted(mapping['streams'].items()):
                payload=samples.sample({'$ref':'#/$defs/'+stream['eventEnvelope']})
                samples.validate({'$ref':'#/$defs/'+endpoint['response']},payload)
                cursor='synthetic.cursor'
                frame='id: '+cursor+'\nevent: message\ndata: '+json.dumps(payload,sort_keys=True,separators=(',',':'))+'\n\n'
                successes.append({'status':int(status),'contentType':'text/event-stream','streamId':streamid,'frame':frame,'jsonData':payload})
            payload=samples.sample({'$ref':'#/$defs/StreamControl'})
            successes.append({'status':int(status),'contentType':'text/event-stream','streamId':'StreamControl','frame':'event: control\ndata: '+json.dumps(payload,sort_keys=True,separators=(',',':'))+'\n\n','jsonData':payload})
        else:
            payload=samples.sample({'$ref':'#/$defs/'+endpoint['response']})
            samples.validate(local(operation['responses'][status]['content']['application/json']['schema']),payload)
            reject_pagination_duplicate(payload)
            successes.append({'status':int(status),'contentType':'application/json','body':payload})
        records[identifier]={'method':endpoint['method'],'path':endpoint['path'],'requestVariants':variants,'successes':successes}
    return {'label':LABEL,'scope':'Schema-only instances; no handler, receipt, signature validity, business-state or arithmetic proof.','inputs':{'semanticCatalogSha256':digest,'semanticDigestAlgorithm':materializer.SEMANTIC_DIGEST_VERSION,**{name:hashlib.sha256((directory/name).read_bytes()).hexdigest() for name in documents}},'endpoints':records,'compatibilityCases':compatibility_cases(samples)}

def check(actual, expected):
    if actual.get('label') != LABEL: raise ExampleError('Synthetic evidence label missing')
    if set(actual.get('endpoints',{})) != set(expected['endpoints']): raise ExampleError('Missing, unknown or duplicate endpoint examples')
    if actual.get('inputs') != expected['inputs']: raise ExampleError('Stale example input hashes')
    if actual != expected: raise ExampleError('Examples missing, stale, invalid or non-deterministic')

def main():
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--spec',default='launchpad/spec/production-system/spec.kvx')
    parser.add_argument('--contracts',default='launchpad/spec/production-system/integration/contracts/v1')
    parser.add_argument('--major',type=int,default=1)
    parser.add_argument('--api-base')
    parser.add_argument('--catalog')
    parser.add_argument('--examples', help='examples artifact path; defaults to contracts/examples.json')
    parser.add_argument('--materializer',default=str(pathlib.Path(__file__).with_name('materialize.py')))
    mode=parser.add_mutually_exclusive_group(required=True);mode.add_argument('--check',action='store_true');mode.add_argument('--write',action='store_true')
    args=parser.parse_args();directory=pathlib.Path(args.contracts)
    spec=json.loads(pathlib.Path(args.catalog).read_text()) if args.catalog else tomllib.loads(pathlib.Path(args.spec).read_text())
    if 'sections' in spec:spec=spec['sections']
    expected=generate(spec,directory,load_materializer(args.materializer),args.major,args.api_base)
    target=pathlib.Path(args.examples) if args.examples else directory/'examples.json'
    if args.check:
        if not target.is_file():raise ExampleError('Missing examples.json')
        # Parsing rejects duplicate JSON keys rather than silently losing IDs.
        def pairs(entries):
            result={}
            for key,value in entries:
                if key in result:raise ExampleError('Duplicate JSON key: '+key)
                result[key]=value
            return result
        actual=json.loads(target.read_bytes(),object_pairs_hook=pairs);check(actual,expected)
        if target.read_bytes()!=encode(expected):raise ExampleError('Non-canonical examples bytes')
    else:target.parent.mkdir(parents=True,exist_ok=True);target.write_bytes(encode(expected))
    print('PASS '+str(len(expected['endpoints']))+' exact endpoint synthetic schema examples; not runtime/receipt evidence')
if __name__=='__main__':
    try:main()
    except Exception as error:print('FAIL: '+str(error),file=sys.stderr);sys.exit(1)
