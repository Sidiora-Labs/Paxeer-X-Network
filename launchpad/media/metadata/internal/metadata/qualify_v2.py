import json
import pathlib
import subprocess
import sys

from jsonschema import Draft202012Validator, FormatChecker

root = pathlib.Path(__file__).resolve().parents[4]
base = root / 'spec/production-system/handoffs/L39/phase1'
validators = {name: Draft202012Validator(json.loads((base / ('metadata-v2-' + name + '.schema.json')).read_text()), format_checker=FormatChecker()) for name in ('patch', 'response')}
result = subprocess.run(['go', 'test', '-race', '-p', '1', '-count=1', '-json', './media/metadata/internal/image', './media/gateway/internal/image', './media/metadata/internal/metadata', '-run', 'Production|Allowed|SVG'], cwd=root, text=True, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, timeout=300)
print(result.stdout, end='')
if result.returncode:
    sys.exit(result.returncode)
passes = set()
seen = {'patch': [], 'response': []}
outputs = {}
for line in result.stdout.splitlines():
    event = json.loads(line)
    if event.get('Action') in ('fail', 'skip'):
        raise AssertionError(event)
    if event.get('Action') == 'pass' and event.get('Test'):
        passes.add(event['Test'])
    identity = (event.get('Package'), event.get('Test'))
    outputs.setdefault(identity, []).append(event.get('Output', ''))
for chunks in outputs.values():
    for output in ''.join(chunks).splitlines():
        if 'V2_SCHEMA ' not in output:
            continue
        name, raw = output.split('V2_SCHEMA ', 1)[1].strip().split(' ', 1)
        value = json.loads(raw)
        validators[name].validate(value)
        seen[name].append(value)
if not {'TestProductionV2RoundTrip', 'TestProductionV2Bounds', 'TestProductionFrozenResponses', 'TestProductionPatchBounds'} <= passes:
    raise AssertionError(passes)
if len(seen['patch']) != 2 or len(seen['response']) != 2:
    raise AssertionError(seen)
for name, values in seen.items():
    descriptions = [v['description'] if name == 'patch' else v['data']['display']['description'] for v in values]
    if sorted(map(len, descriptions)) != [0, 4096]:
        raise AssertionError(descriptions)
print(json.dumps({'named_passes': len(passes), 'v2_actual_http_schema_validations': {k: len(v) for k, v in seen.items()}, 'passed': True}))
