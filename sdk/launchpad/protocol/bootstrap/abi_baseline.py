#!/usr/bin/env python3
"""Read-only comparison of generated Go bindings and supplied contract artifacts."""
import argparse,hashlib,json,pathlib,re

def digest(p): return hashlib.sha256(p.read_bytes()).hexdigest()
def typ(v):
    t=v['type']
    return '('+','.join(typ(x) for x in v.get('components',[]))+')'+t[5:] if t.startswith('tuple') else t
def identity(v):return v.get('type','')+':'+v.get('name','')+'('+','.join(typ(x) for x in v.get('inputs',[]))+')'
def canonical(v): return json.dumps(v,sort_keys=True,separators=(',',':'))
def main():
    a=argparse.ArgumentParser();a.add_argument('--contracts',type=pathlib.Path,required=True);a.add_argument('--output',type=pathlib.Path,required=True);args=a.parse_args()
    module=pathlib.Path(__file__).resolve().parents[1];manifest=args.contracts/'deployments/paxeer-x-network-addresses.json';deployment=json.loads(manifest.read_text());items=[]
    for binding in sorted((module/'bindings').glob('*.go')):
        source=binding.read_text();match=re.search(r'var (\w+)MetaData.*?ABI:\s*("(?:[^"\\]|\\.)*")',source,re.S)
        if not match:raise ValueError('Missing actual metadata in '+str(binding))
        name=match[1];existing=json.loads(json.loads(match[2]));candidates=list((args.contracts/'artifacts/contracts').glob('**/'+name+'.sol/'+name+'.json'))
        if len(candidates)!=1:raise ValueError('Ambiguous or absent artifact '+name)
        artifact=candidates[0];current=json.loads(artifact.read_text())['abi'];old={identity(x):x for x in existing};new={identity(x):x for x in current}
        items.append({'contract':name,'binding':str(binding),'binding_sha256':digest(binding),'artifact':str(artifact),'artifact_sha256':digest(artifact),'embedded_abi_sha256':hashlib.sha256(canonical(existing).encode()).hexdigest(),'current_abi_sha256':hashlib.sha256(canonical(current).encode()).hexdigest(),'missing_signatures':sorted(new.keys()-old.keys()),'removed_signatures':sorted(old.keys()-new.keys()),'changed_schemas':[k for k in sorted(new.keys()&old.keys()) if canonical(new[k])!=canonical(old[k])],'current_address':deployment.get(name+'_proxy',deployment.get(name))})
    records={k:{field:v.get(field) for field in ['address','status','txHash','blockNumber','creationHash','runtimeHash']} for k,v in deployment['_deployment']['records'].items()}
    out={'evidence':'Read-only source/artifact comparison; no live RPC, deployed-code validation or production qualification','manifest':str(manifest),'manifest_sha256':digest(manifest),'manifest_status':deployment['_meta']['status'],'chain_id':int(deployment['_meta']['network']),'bindings':items,'deployment_records':records,'missing_binding_contracts':['SidioraPool','SidioraERC20','OpticalRegistry','Treasury','Timelock','GovernanceModule','EmergencyPauseController'],'limitations':['Artifact ABI provenance is captured by file hash; compiler build-input linkage and runtime matching require separate release verification.','Signature identity comparison also records schema changes including indexed fields and argument names.','Declared ABI event does not prove any implementation emission path.','Deployment records are persisted source evidence, not fresh chain confirmation.']}
    args.output.write_text(json.dumps(out,indent=2)+'\n')
if __name__=='__main__':main()
