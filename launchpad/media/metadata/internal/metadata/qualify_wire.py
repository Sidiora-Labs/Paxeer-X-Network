#!/usr/bin/env python3
"""Execute actual Go serializers and validate outputs against the frozen registry."""
import json, pathlib, subprocess, sys
from jsonschema import Draft202012Validator, FormatChecker
root=pathlib.Path(__file__).resolve().parents[4]
schema=json.loads((root/'spec/production-system/integration/contracts/v1/schema-definitions.json').read_text())
r=subprocess.run(['go','test','-p','1','-count=1','-json','./media/metadata/internal/metadata','-run','^TestProductionFrozenResponses$'],cwd=root,text=True,stdout=subprocess.PIPE,stderr=subprocess.STDOUT,timeout=120)
print(r.stdout,end='')
if r.returncode:sys.exit(r.returncode)
seen=set();count=0;ran=passed=package_pass=False
for line in r.stdout.splitlines():
 event=json.loads(line)
 if event.get('Action') in {'fail','skip'}:raise AssertionError(event)
 if event.get('Test')=='TestProductionFrozenResponses' and event.get('Action')=='run':ran=True
 if event.get('Test')=='TestProductionFrozenResponses' and event.get('Action')=='pass':passed=True
 if 'Test' not in event and event.get('Action')=='pass':package_pass=True
 output=event.get('Output','')
 if 'FROZEN_RESPONSE ' not in output:continue
 name,payload=output.split('FROZEN_RESPONSE ',1)[1].strip().split(' ',1)
 value=json.loads(payload);v=Draft202012Validator({'$ref':'#/$defs/'+name,'$defs':schema['$defs']},format_checker=FormatChecker());v.validate(value)
 # Schema negative proves the actual frozen required member is enforced.
 mutated=dict(value);mutated.pop('requestId' if name.endswith('Response') else 'sha256')
 if v.is_valid(mutated):raise AssertionError('missing requestId accepted')
 seen.add(name);count+=1
required={'UploadCreateResponse','UploadCompleteResponse','AssetGetResponse','MetadataGetResponse','MetadataPatchResponse','UploadCreateBody','UploadCompleteBody'}
if not ran or not passed or not package_pass or seen!=required or count!=8:raise AssertionError((seen,count))
print(json.dumps({'frozen_response_serializations':count,'schemas':sorted(seen),'passed':True}))
