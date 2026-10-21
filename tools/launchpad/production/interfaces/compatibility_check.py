#!/usr/bin/env python3
"""Validate frozen legacy source mappings; never claims runtime adapters exist."""
import argparse
import hashlib
import json
import re
from pathlib import Path

EXPECTED = {'candles-sdk','stats-sdk','indexer-sdk','ranking-algo-sdk','metadata-sdk','users-sdk','sidiora-launchpad-v3','browser-chat','opaque-resume','portfolio-basis'}

def interface_fields(text):
    result=set()
    for match in re.finditer(r'(?:export )?interface (\w+)[^{]*\{',text):
        start=match.end(); depth=1; end=start
        while end<len(text) and depth:
            depth += (text[end]=='{')-(text[end]=='}');end+=1
        for field in re.finditer(r'\b(\w+)\??\s*:',text[start:end-1]):
            result.add(match[1]+'.'+field[1])
    return sorted(result)

def check(root, manifest, schemas=None):
    failures=[]; data=json.loads(Path(manifest).read_text()); root=Path(root)
    records=data['contracts']
    if {r['id'] for r in records} != EXPECTED: failures.append('unmapped wire contract or unexpected contract ID')
    defs=json.loads(Path(schemas).read_text()).get('$defs',{}) if schemas else None
    for record in records:
        ident=record['id']
        if record['status']!='adapter_required' or record.get('runtime_qualified') is not False:
            failures.append(ident+': unsupported compatibility/runtime qualification')
        sources={}
        for source in record['sources']:
            path=root/source['path']
            if not path.is_file(): failures.append(ident+': missing source '+source['path']);continue
            raw=path.read_bytes();sources[source['path']]=raw.decode()
            if hashlib.sha256(raw).hexdigest()!=source['sha256']: failures.append(ident+': source hash drift '+source['path'])
        for route in record['routes']:
            if route['literal'] not in sources.get(route['source'],''): failures.append(ident+': route reference missing '+route['literal'])
        for inventory in record['interface_inventories']:
            observed=interface_fields(sources.get(inventory['source'],''))
            if observed!=sorted(inventory['fields']): failures.append(ident+': unmapped source interface fields '+inventory['source'])
            mapped={m['source_field'] for m in record['field_mappings'] if m['source']==inventory['source']}
            if mapped!=set(observed): failures.append(ident+': unmapped or invented wire fields')
        for mapping in record['field_mappings']:
            if mapping['disposition'] not in {'transform','legacy_only','requires_enrichment'} or not mapping.get('rule'):
                failures.append(ident+': silent field compatibility assumption')
            if 'target_schema_ref' in mapping and defs is not None:
                target=defs.get(mapping['target_schema_ref'].split('#/$defs/')[-1],{})
                if mapping['target_field'] not in target.get('properties',{}): failures.append(ident+': unresolved target field '+mapping['target_field'])
        for proof in record['source_anchors']:
            if proof['text'] not in sources.get(proof['source'],''): failures.append(ident+': missing source proof '+proof['text'])
        if not record['missing_prerequisites']: failures.append(ident+': missing enrichment/adapter prerequisites omitted')
        for ref in record['target_schema_refs']:
            name=ref.split('#/$defs/')[-1]
            if not ref.startswith('schema-definitions.json#/$defs/'): failures.append(ident+': invalid schema reference')
            if defs is not None and name not in defs: failures.append(ident+': unresolved generated schema '+name)
    return failures

def main():
    p=argparse.ArgumentParser();p.add_argument('--root',required=True);p.add_argument('--manifest');p.add_argument('--schemas');p.add_argument('--schemas-required',action='store_true');p.add_argument('--check',action='store_true',required=True)
    a=p.parse_args();manifest=a.manifest or str(Path(a.root)/'spec/production-system/integration/contracts/v1/compatibility.json')
    if a.schemas_required and not a.schemas:p.error('--schemas-required needs --schemas')
    failures=check(a.root,manifest,a.schemas)
    print(json.dumps({'status':'failed' if failures else 'passed','assertion':'source-grounded compatibility assessment only; runtime adapters and v1 handlers pending','contracts':len(EXPECTED),'generated_schema_refs_checked':bool(a.schemas),'failures':failures},indent=2));return bool(failures)
if __name__=='__main__':raise SystemExit(main())
