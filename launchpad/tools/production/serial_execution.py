"""Cancellable native proof execution with fresh owned output namespaces."""
import hashlib
import errno
import hmac
import os
from pathlib import Path
import signal
import subprocess
import threading
import time

class SerialExecutionError(ValueError):
    pass

def _cancelled(control):
    return control is not None and control.cancelled.is_set()

def _process(command, root, timeout, control):
    if _cancelled(control): raise SerialExecutionError('qualification cancelled before spawn')
    started=time.monotonic(); process=None; buffers=[bytearray(),bytearray()];threads=[]
    def drain(stream,buffer):
        try:
            while chunk:=stream.read(65536):
                if len(buffer)<1024*1024:buffer.extend(chunk[:1024*1024-len(buffer)])
        finally:stream.close()
    try:
        process=subprocess.Popen(command,cwd=root,start_new_session=True,stdout=subprocess.PIPE,stderr=subprocess.PIPE)
        for stream,buffer in zip((process.stdout,process.stderr),buffers):
            thread=threading.Thread(target=drain,args=(stream,buffer),daemon=True);thread.start();threads.append(thread)
        while process.poll() is None:
            if _cancelled(control):raise SerialExecutionError('qualification cancelled during native check')
            if time.monotonic()-started>=timeout:raise SerialExecutionError('native check timed out')
            time.sleep(0.025)
        if _cancelled(control):raise SerialExecutionError('qualification cancelled at native completion')
    finally:
        if process is not None:
            # Clean the whole inherited group even when the direct child exited.
            try:os.killpg(process.pid,signal.SIGTERM)
            except ProcessLookupError:pass
            try:process.wait(timeout=0.5)
            except subprocess.TimeoutExpired:pass
            try:os.killpg(process.pid,signal.SIGKILL)
            except ProcessLookupError:pass
            process.wait(timeout=2)
            for thread in threads:thread.join(timeout=2)
    if any(t.is_alive() for t in threads):raise SerialExecutionError('native process pipe cleanup failed')
    return dict(exit_code=process.returncode,timed_out=False,stdout=buffers[0].decode('utf-8','replace'),stderr=buffers[1].decode('utf-8','replace'),duration=time.monotonic()-started,pid=process.pid,cleanup=True)

def _checker(argv,root,evidence):
    try:return evidence._checker(argv,root)
    except OSError as error:
        if error.errno!=errno.ENAMETOOLONG:raise
    # Compatibility until the coordinator's narrow argv fix is integrated.
    result=evidence._checker(argv[:1],root)
    for argument in argv[1:]:
        candidate=Path(argument)
        if not candidate.is_absolute():candidate=root/candidate
        try:exists=candidate.is_file()
        except OSError as error:
            if error.errno==errno.ENAMETOOLONG:continue
            raise
        if exists:
            if any(p.is_symlink() for p in [candidate,*candidate.parents]):raise SerialExecutionError('checker script symlink')
            result[str(candidate.resolve())]=evidence._file(candidate)
    return result

def run_checks(checks,cwd,output_dir,binding,token,secret,evidence,control=None):
    """Output templates resolve into new per-check directories; no old path reuse."""
    root=Path(cwd).resolve();output=Path(output_dir).resolve()
    evidence._secret(secret)
    if output==root or output.is_relative_to(root) or output.exists():raise SerialExecutionError('fresh external evidence directory required')
    if not isinstance(checks,list) or not checks or token.get('bindings')!=binding:raise SerialExecutionError('checks and matching binding required')
    source=evidence.source_digest(root)
    if binding.get('candidate')!=source:raise SerialExecutionError('wrong candidate binding')
    output.mkdir(parents=True);prepared=[];ids=set()
    for ordinal,check in enumerate(checks):
        if not isinstance(check,dict) or not isinstance(check.get('id'),str) or not check['id'] or check['id'] in ids or check.get('skip') or check.get('skipped'):raise SerialExecutionError('invalid, duplicate or skipped check')
        ids.add(check['id']);namespace=output/'generated'/str(ordinal)
        command=check.get('command');timeout=check.get('timeout');paths=check.get('outputs',[])
        if not isinstance(command,list) or not command or any(not isinstance(a,str) or not a for a in command) or type(timeout) not in (int,float) or not 0<timeout<=3600 or not check.get('proof') or not isinstance(paths,list):raise SerialExecutionError('invalid native check')
        resolved=[]
        for value in paths:
            if not isinstance(value,str) or not value.startswith('{output_dir}/'):raise SerialExecutionError('declared output must use fresh owned output_dir namespace')
            suffix=value[len('{output_dir}/'):]
            if not suffix or any(p in ('','.','..') for p in suffix.split('/')) or '\\' in suffix:raise SerialExecutionError('unsafe output template')
            path=namespace/suffix
            if path.exists() or path.is_symlink():raise SerialExecutionError('preexisting declared output')
            resolved.append(str(path))
        if len(set(resolved))!=len(resolved):raise SerialExecutionError('duplicate outputs')
        namespace.mkdir(parents=True)
        for value in resolved:Path(value).parent.mkdir(parents=True,exist_ok=True)
        argv=[a.replace('{output_dir}',str(namespace)) for a in command]
        prepared.append((check,argv,resolved,namespace,evidence._paths(check.get('inputs',[]),root),_checker(argv,root,evidence)))
    receipts=[]
    for check,command,paths,namespace,inputs,checker in prepared:
        if any(Path(p).exists() or Path(p).is_symlink() for p in paths):raise SerialExecutionError('output appeared before execution')
        execution=_process(command,root,check['timeout'],control)
        receipt=dict(id=check['id'],command=command,timeout=check['timeout'],input_paths=check.get('inputs',[]),output_paths=paths,inputs=inputs,checker=checker,outputs=evidence._paths(paths,root,outside=True),executed=True,proof_policy=check['proof'],output_freshness={'namespace':str(namespace),'absent_before':True,'template_outputs':check.get('outputs',[]),'template_command':check['command']},**execution)
        receipt['proof']=evidence._proof(receipt,check['proof']);receipts.append(receipt)
        if _cancelled(control):raise SerialExecutionError('qualification cancelled after native check')
    if evidence.source_digest(root)!=source:raise SerialExecutionError('native checks changed source')
    for receipt in receipts:
        for path,expected in receipt['checker'].items():
            if evidence._file(path)!=expected:raise SerialExecutionError('native checker changed during execution')
    (output/'outputs').mkdir()
    for receipt in receipts:
        for path,value in receipt['outputs'].items():
            data=Path(path).read_bytes()
            if hashlib.sha256(data).hexdigest()!=value:raise SerialExecutionError('generated output changed before retention')
            (output/'outputs'/value).write_bytes(data)
    result=dict(version=1,root=str(root),output_directory=str(output),bindings=binding,token=token,source_digest=source,source_before=source,receipts=receipts)
    result['signature']=hmac.new(secret,evidence._json(result),hashlib.sha256).hexdigest()
    (output/'evidence.json').write_bytes(evidence._json(result)+b'\n')
    if _cancelled(control):raise SerialExecutionError('qualification cancelled before native evidence publication')
    return result


def historical_checks(result,checks):
    """Validate fresh namespace provenance and expand trusted output templates."""
    import copy
    if not isinstance(checks,list) or not checks:raise SerialExecutionError('missing staged checks')
    receipts=result.get('receipts',[])
    if len(receipts)!=len(checks):raise SerialExecutionError('staged receipt count differs')
    expanded=[]
    for ordinal,(check,receipt) in enumerate(zip(checks,receipts)):
        expected=copy.deepcopy(check)
        if check.get('outputs'):
            fresh=receipt.get('output_freshness',{})
            namespace=Path(result.get('output_directory',''))/'generated'/str(ordinal)
            if not namespace.is_absolute() or fresh.get('namespace')!=str(namespace) or fresh.get('absent_before') is not True or fresh.get('template_outputs')!=check['outputs'] or fresh.get('template_command')!=check['command']:
                raise SerialExecutionError('staged output lacks fresh production provenance')
            expected['command']=[a.replace('{output_dir}',str(namespace)) for a in check['command']]
            expected['outputs']=[a.replace('{output_dir}',str(namespace)) for a in check['outputs']]
        elif receipt.get('output_paths') or receipt.get('outputs'):
            raise SerialExecutionError('undeclared staged outputs')
        expanded.append(expected)
    return expanded
