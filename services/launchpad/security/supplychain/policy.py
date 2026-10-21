"""Fail-closed release primitives. No network access and no trust from artifact fields."""
import hashlib, json, re, subprocess, tempfile
from pathlib import Path
class Rejected(ValueError): pass
def reject(message): raise Rejected(message)
def require(ok, message):
    if not ok: reject(message)
def digest(data): return hashlib.sha256(data).hexdigest()
def canonical(value): return json.dumps(value,sort_keys=True,separators=(",",":"),ensure_ascii=True).encode()
def load(data):
    require(len(data)<=4*1024*1024,"document too large")
    def pairs(items):
        out={}
        for k,v in items:
            require(k not in out,"duplicate JSON key")
            out[k]=v
        return out
    return json.loads(data,object_pairs_hook=pairs,parse_constant=lambda _:reject("nonfinite JSON"))
def sha(value): return isinstance(value,str) and re.fullmatch(r"[0-9a-f]{64}",value) is not None
def pinned_image(value):
    require(isinstance(value,str) and re.fullmatch(r"[a-z0-9][a-z0-9./_-]+@sha256:[0-9a-f]{64}",value),"unpinned image")
def verify_signature(document,signature,public_key,expected_key_sha):
    require(sha(expected_key_sha) and digest(public_key)==expected_key_sha,"untrusted signing identity")
    require(len(signature)==64,"invalid signature length")
    with tempfile.TemporaryDirectory(prefix="release-signature-") as temp:
        d=Path(temp); (d/"key").write_bytes(public_key); (d/"doc").write_bytes(document); (d/"sig").write_bytes(signature)
        result=subprocess.run(["openssl","pkeyutl","-verify","-pubin","-inkey",str(d/"key"),"-rawin","-in",str(d/"doc"),"-sigfile",str(d/"sig")],capture_output=True,timeout=5)
        require(result.returncode==0,"signature verification failed")
def admission(doc, expected, now):
    require(type(now) is int and now>=0,"invalid clock")
    required={"verify","contracts","frontend","policy","scan","signing"}
    require(set(doc.get("checks",{}))==required,"missing or unexpected checks")
    for name, result in doc["checks"].items():
        require(set(result)=={"id","result","source"},"invalid check shape")
        require(result["result"]=="success" and isinstance(result["id"],str) and result["id"],"required check failed: "+name)
        require(result["source"]==expected["source"],"stale check: "+name)
    require(len({v["id"] for v in doc["checks"].values()})==len(required),"reused check ID")
    for field in ("source","workflow","image","sbom","repository","builder","run_id"):
        require(doc.get(field)==expected[field],"binding mismatch: "+field)
    for field in ("source","workflow","sbom"): require(sha(doc[field]),"invalid digest")
    pinned_image(doc["image"])
    require(type(doc.get("issued")) is int and type(doc.get("expires")) is int,"invalid timestamps")
    require(0<=doc["issued"]<=now<doc["expires"]<=doc["issued"]+86400,"stale provenance")
    require(doc.get("event")=="push" and doc.get("ref")==expected["ref"] and doc.get("fork") is False,"untrusted publication context")
    require(doc.get("scanner_status")=="success","scanner unavailable")
    require(type(doc.get("vulnerabilities")) is list and len(doc["vulnerabilities"])<=10000,"missing or oversized vulnerability list")
    seen=set()
    for finding in doc["vulnerabilities"]:
        require(type(finding) is dict and set(finding) in ({"id","severity","reachable","analysis"},{"id","severity","reachable","analysis","exception"}),"invalid finding shape")
        identity=finding.get("id"); require(isinstance(identity,str) and identity not in seen,"duplicate vulnerability");seen.add(identity)
        require(finding.get("severity") in {"critical","high","medium","low"},"unknown severity")
        require(type(finding.get("reachable")) is bool,"missing reachability")
        require(isinstance(finding.get("analysis"),str) and finding["analysis"].strip(),"missing exposure analysis")
        if finding["severity"]=="critical" and finding["reachable"]: reject("reachable critical: "+identity)
        if finding["severity"]=="high":
            e=finding.get("exception",{})
            require(type(e) is dict and set(e)=={"id","issued","expires","approved_by","controls"},"invalid exception shape")
            eid=e.get("id","missing")
            require(isinstance(eid,str) and 0<len(eid)<=256,"invalid exception identity")
            binding={"finding":identity,"source":expected["source"],"image":expected["image"],"exception":e}
            require(digest(canonical(binding)) in expected.get("approved_exception_digests",[]),"exception lacks independent approval: "+eid)
            require(type(e.get("issued")) is int and type(e.get("expires")) is int,"invalid exception: "+eid)
            require(e["issued"]<=now<e["expires"]<=e["issued"]+604800,"expired or excessive exception: "+eid)
            require(e.get("approved_by") in expected["exception_approvers"] and bool(e.get("controls")),"unapproved exception: "+eid)
    return {"admitted":True,"image":doc["image"],"source":doc["source"]}
def validate_sbom(data, expected_sha):
    require(digest(data)==expected_sha,"SBOM digest mismatch")
    doc=load(data);require(doc.get("spdxVersion")=="SPDX-2.3","unsupported SBOM")
    require(isinstance(doc.get("packages"),list) and doc["packages"],"empty SBOM")
    kinds=set()
    for p in doc["packages"]:
        require(p.get("name") and p.get("versionInfo") and p.get("licenseConcluded"),"incomplete SBOM package")
        kinds.add(p.get("kind"))
    require(kinds >= {"go","javascript","os"},"incomplete dependency coverage")
def runtime(profile):
    require(profile.get("runAsNonRoot") is True and type(profile.get("uid")) is int and profile["uid"]>0,"root runtime")
    require(profile.get("readOnlyRootFilesystem") is True,"writable root")
    require(profile.get("privileged") is False and profile.get("allowPrivilegeEscalation") is False,"privileged runtime")
    require(profile.get("hostNetwork") is False and profile.get("hostPID") is False and profile.get("hostIPC") is False,"host namespace")
    require(profile.get("capabilities")=={"drop":["ALL"],"add":[]},"capability violation")
    require(profile.get("seccomp")=="RuntimeDefault","seccomp violation")
    require(profile.get("egressPolicyDigest") and sha(profile["egressPolicyDigest"]),"missing egress policy")
    require(set(profile.get("limits",{}))=={"cpu","memory","pids"} and all(type(v)is int and v>0 for v in profile["limits"].values()),"missing resource limits")
    require(type(profile.get("tmpfs",[])) is list and len(profile.get("tmpfs",[]))<=1,"invalid tmpfs inventory")
    for mount in profile.get("tmpfs",[]):
        require(set(mount)=={"path","bytes"} and mount["path"]=="/tmp" and type(mount["bytes"])is int and 0<mount["bytes"]<=67108864,"unbounded tmpfs")

def admit_signed(document, signature, public_key, expected_key_sha, sbom, expected, now):
    require(len(document)<=4*1024*1024,"document too large")
    verify_signature(document,signature,public_key,expected_key_sha)
    doc=load(document)
    validate_sbom(sbom,expected["sbom"])
    return admission(doc,expected,now)

def verified_tool_bytes(data, expected_sha):
    require(sha(expected_sha) and digest(data)==expected_sha,"tool checksum mismatch")
    return data
