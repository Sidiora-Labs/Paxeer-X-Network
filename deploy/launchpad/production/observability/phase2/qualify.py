from __future__ import annotations

import argparse
import json
from pathlib import Path

from contracts import canonical, load, REQUIREMENTS, safe_diagnostics, validate_binding, require
from control import read_key
from evidence import Evidence, evaluate, evaluate_phase3
from render import atomic_write


def qualify(binding,config,evidence_path,key,output,phase=2):
    require(phase in (2,3),'unsupported qualification phase')
    validate_binding(binding,config)
    try:
        evidence=Evidence(evidence_path,binding,config,key)
        result=evaluate_phase3(evidence) if phase==3 else evaluate(evidence)
    except (ValueError,KeyError,TypeError,OSError):
        result={'version':'2.0','candidate_sha256':binding['candidate_sha256'],'input_interface_sha256':binding['input_interface_sha256'],'deployment_digest':binding['deployment_digest'],'configuration_revision':binding['configuration_revision'],'passed':False,'results':[{'requirement':r,'criteria':[r+'.'+str(n) for n in range(1,6)],'passed':False,'reason':'unavailable_or_untrusted_native_evidence','observation':None} for r in REQUIREMENTS]}
    result['phase']=phase
    result['diagnostics']=safe_diagnostics(binding)
    for row in result['results']:
        atomic_write(Path(output)/(row['requirement']+'.json'),canonical({'identity':result['diagnostics'],**row}))
    atomic_write(Path(output)/'result.json',canonical(result))
    return result


def main():
    p=argparse.ArgumentParser();p.add_argument('--binding',required=True);p.add_argument('--config',required=True);p.add_argument('--evidence',required=True);p.add_argument('--attestation-key-file',required=True);p.add_argument('--output',required=True);p.add_argument('--phase',type=int,choices=(2,3),default=2);a=p.parse_args()
    result=qualify(load(a.binding),load(a.config),a.evidence,read_key(a.attestation_key_file),a.output,a.phase)
    print(json.dumps({'passed':result['passed'],'requirements':len(result['results']),'result_path':str(Path(a.output)/'result.json')}))
    raise SystemExit(0 if result['passed'] else 1)


if __name__=='__main__':
    main()
