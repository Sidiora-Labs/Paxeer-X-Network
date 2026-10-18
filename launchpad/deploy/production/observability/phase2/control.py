from __future__ import annotations

import argparse
from contextlib import contextmanager
import json
import os
from pathlib import Path
import sqlite3
import time

from contracts import Invalid,canonical, inventory, load, private_document, require, sha, validate_binding
from evidence import Evidence, evaluate, evaluate_phase3
from render import immutable_bundle, safe_directory


class Denied(Invalid):
    pass


class Control:
    def __init__(self, path, authority_path=None, phase=2):
        require(phase in (2,3), 'unsupported control phase')
        self.phase=phase
        self.authority_path=authority_path
        self.path=safe_directory(Path(path).parent)/Path(path).name
        self.path.parent.mkdir(parents=True,exist_ok=True,mode=0o700)
        require(not self.path.is_symlink(),'state symlink denied')
        with self.connection() as db:
            db.execute('CREATE TABLE IF NOT EXISTS control_settings (name TEXT PRIMARY KEY, value TEXT NOT NULL)')
            secured=db.execute('SELECT value FROM control_settings WHERE name="security_phase"').fetchone()
            if secured:
                require(int(secured[0])<=phase, 'control authority downgrade denied')
            authenticated=db.execute('SELECT value FROM control_settings WHERE name="authentication"').fetchone()
            require(not authenticated or self.authority_path is not None, 'control authentication downgrade denied')
            if self.authority_path is not None:
                db.execute('INSERT INTO control_settings(name,value) VALUES ("authentication","required") ON CONFLICT(name) DO UPDATE SET value="required"')
            if phase==3:
                db.execute('INSERT INTO control_settings(name,value) VALUES ("security_phase","3") ON CONFLICT(name) DO UPDATE SET value="3"')
            db.execute('CREATE TABLE IF NOT EXISTS transitions (sequence INTEGER PRIMARY KEY, revision INTEGER NOT NULL, state TEXT NOT NULL, actor TEXT NOT NULL, at REAL NOT NULL, binding_sha256 TEXT NOT NULL, configuration_sha256 TEXT NOT NULL, evidence_sha256 TEXT, previous_sha256 TEXT NOT NULL, record_sha256 TEXT NOT NULL UNIQUE, payload TEXT NOT NULL)')
            db.execute('CREATE UNIQUE INDEX IF NOT EXISTS proposals ON transitions(revision) WHERE state="proposed"')
        os.chmod(self.path,0o600)

    def authorize(self, action, actor, binding):
        if self.authority_path is None:
            if self.phase != 2 or binding['environment'] != 'qualification':
                raise Denied('authenticated operator authority required')
            return
        authority=private_document(self.authority_path)
        inventory.exact(authority, ('version','binding_sha256','expires_at','principals'))
        require(authority['version']=='3.0' and authority['binding_sha256']==sha(canonical(binding)), 'operator authority scope differs')
        require(type(authority['expires_at']) in (int,float) and time.time()<authority['expires_at']<=time.time()+86400, 'operator authority expired or unbounded')
        require(isinstance(authority['principals'],list) and authority['principals'], 'operator authority has no principals')
        identities=set();assigned=None
        for principal in authority['principals']:
            inventory.exact(principal, ('uid','actor','actions'))
            require(type(principal['uid']) is int and principal['uid']>=0 and principal['uid'] not in identities, 'ambiguous operating system principal')
            identities.add(principal['uid'])
            require(isinstance(principal['actor'],str) and inventory.SAFE.fullmatch(principal['actor']) and principal['actor'] in binding['responders'].values(), 'unassigned authority actor')
            require(isinstance(principal['actions'],list) and principal['actions'] and len(principal['actions'])==len(set(principal['actions'])) and set(principal['actions']) <= {'propose','validate','activate','reject'}, 'operator action scope invalid')
            if principal['uid']==os.geteuid():assigned=principal
        if assigned is None or assigned['actor']!=actor or action not in assigned['actions']:
            raise Denied('operator action denied')

    def journal(self, db):
        rows=db.execute('SELECT sequence,revision,state,actor,at,binding_sha256,configuration_sha256,evidence_sha256,previous_sha256,record_sha256,payload FROM transitions ORDER BY sequence').fetchall()
        previous='0'*64;last=None
        keys=('sequence','revision','state','actor','at','binding_sha256','configuration_sha256','evidence_sha256','previous_sha256')
        for expected,row in enumerate(rows,1):
            record=json.loads(row[10])
            require(row[0]==expected and all(record.get(key)==value for key,value in zip(keys,row[:9])), 'operator audit columns corrupt')
            require(record['previous_sha256']==previous and sha(canonical(record))==row[9], 'operator audit journal corrupt')
            previous=row[9];last={**record,'record_sha256':row[9]}
        return last

    @contextmanager
    def connection(self):
        db=sqlite3.connect(self.path,timeout=5,isolation_level=None)
        try:
            db.execute('PRAGMA synchronous=FULL')
            db.execute('PRAGMA foreign_keys=ON')
            db.execute('BEGIN IMMEDIATE')
            yield db
            db.commit()
        except BaseException:
            db.rollback()
            raise
        finally:
            db.close()

    def change(self, action, actor, binding, config, expected_sequence, evidence_path=None, attestation_key=None, output=None):
        validate_binding(binding,config)
        require(isinstance(actor,str) and inventory.SAFE.fullmatch(actor),'invalid actor')
        if actor not in binding['responders'].values():
            raise Denied('operator actor not assigned')
        self.authorize(action,actor,binding)
        revision=config['revision'];binding_hash=sha(canonical(binding));config_hash=sha(canonical(config))
        require(type(expected_sequence) is int and expected_sequence>=0,'invalid expected sequence')
        evaluation=None
        if action in ('validate','activate'):
            require(evidence_path is not None,'native evidence required')
            evidence=Evidence(evidence_path,binding,config,attestation_key)
            evaluation=evaluate_phase3(evidence) if self.phase==3 else evaluate(evidence)
            require(evaluation['passed'],'scenario validation failed')
        with self.connection() as db:
            secured=db.execute('SELECT value FROM control_settings WHERE name="security_phase"').fetchone()
            require(not secured or int(secured[0])<=self.phase, 'control authority downgrade denied')
            authenticated=db.execute('SELECT value FROM control_settings WHERE name="authentication"').fetchone()
            require(not authenticated or self.authority_path is not None, 'control authentication downgrade denied')
            latest=self.journal(db)
            sequence=latest['sequence'] if latest else 0
            require(sequence==expected_sequence,'stale operator state revision')
            previous=latest['record_sha256'] if latest else '0'*64
            if action=='propose':
                require(not latest or revision>latest['revision'],'configuration revision must increase')
                state='proposed'
            else:
                require(latest and latest['revision']==revision and latest['binding_sha256']==binding_hash and latest['configuration_sha256']==config_hash,'configuration changed since proposal')
                states={('proposed','validate'):'validated',('proposed','reject'):'rejected',('validated','activate'):'activated',('validated','reject'):'rejected'}
                require((latest['state'],action) in states,'invalid operator state transition')
                state=states[(latest['state'],action)]
            payload={'sequence':sequence+1,'revision':revision,'state':state,'actor':actor,'at':time.time(),'binding_sha256':binding_hash,'configuration_sha256':config_hash,'evidence_sha256':evaluation['evidence_sha256'] if evaluation else None,'previous_sha256':previous}
            if action=='activate':
                require(output is not None,'activation output directory required')
                require(latest['evidence_sha256']==evaluation['evidence_sha256'], 'activation evidence changed since validation')
                target,manifest=immutable_bundle(binding,config,output)
                payload['render_manifest_sha256']=sha(canonical(manifest))
                payload['active_bundle']=str(target.resolve())
            record_hash=sha(canonical(payload))
            db.execute('INSERT INTO transitions(sequence,revision,state,actor,at,binding_sha256,configuration_sha256,evidence_sha256,previous_sha256,record_sha256,payload) VALUES (?,?,?,?,?,?,?,?,?,?,?)',(sequence+1,revision,state,actor,payload['at'],binding_hash,config_hash,payload['evidence_sha256'],previous,record_hash,canonical(payload).decode()))
            return {**payload,'record_sha256':record_hash}

    def current(self):
        with self.connection() as db:
            return self.journal(db)


def read_key(path):
    path=Path(path)
    require(path.is_file() and not path.is_symlink() and path.stat().st_mode & 0o077==0,'attestation key mount must be private')
    raw=path.read_bytes();require(32<=len(raw)<=4096,'attestation key length')
    return raw


def main():
    p=argparse.ArgumentParser();p.add_argument('action',choices=('propose','validate','activate','reject','current'));p.add_argument('--state',required=True);p.add_argument('--binding');p.add_argument('--config');p.add_argument('--actor');p.add_argument('--expected-sequence',type=int);p.add_argument('--evidence');p.add_argument('--attestation-key-file');p.add_argument('--output');p.add_argument('--phase',type=int,choices=(2,3),default=2);p.add_argument('--operator-authority-file');a=p.parse_args()
    control=Control(a.state,a.operator_authority_file,a.phase)
    if a.action=='current':result=control.current()
    else:
        require(a.binding and a.config and a.actor and a.expected_sequence is not None,'operator command inputs missing')
        key=read_key(a.attestation_key_file) if a.attestation_key_file else None
        try:
            result=control.change(a.action,a.actor,load(a.binding),load(a.config),a.expected_sequence,a.evidence,key,a.output)
        except Denied:
            print(json.dumps({'action':a.action,'decision':'denied','error_code':'unauthorized','sequence':a.expected_sequence}))
            raise SystemExit(1)
    print(json.dumps(result))


if __name__=='__main__':
    main()
