#!/usr/bin/env bash
set -euo pipefail
: "${PAXEER_X_RUST_PROGRAMS_BUILD_JSON:?captured Cargo build JSON is required}"
: "${PAXEER_X_RUST_PROGRAMS_EVIDENCE:?private evidence directory is required}"
python3 - "$PAXEER_X_RUST_PROGRAMS_BUILD_JSON" "$PAXEER_X_RUST_PROGRAMS_EVIDENCE" <<'PYVERIFY'
import hashlib,json,os,pathlib,re,subprocess,sys
build=pathlib.Path(sys.argv[1]); evidence=pathlib.Path(sys.argv[2]); evidence.mkdir(parents=True,exist_ok=True)
messages=[json.loads(line) for line in build.read_text().splitlines() if line.strip()]
assert any(m.get("reason")=="build-finished" and m.get("success") is True for m in messages), "build did not finish successfully"
artifacts=[m for m in messages if m.get("reason")=="compiler-artifact" and m.get("target",{}).get("name")=="layerx_sdk" and "lib" in m.get("target",{}).get("kind",[]) and m.get("profile",{}).get("test") is True and m.get("executable")]
assert len(artifacts)==1, "exactly one real SDK lib test executable is required"
binary=pathlib.Path(artifacts[0]["executable"]); assert binary.is_file() and os.access(binary,os.X_OK), "compiled SDK executable unavailable"
record={"build_json":str(build.resolve()),"build_json_sha256":hashlib.sha256(build.read_bytes()).hexdigest(),"executable":str(binary.resolve()),"sha256":hashlib.sha256(binary.read_bytes()).hexdigest(),"cases":[]}
expected={"programs::http::source_contract":11,"program_lifecycle::tests":2}
for selector,count in expected.items():
    command=[str(binary),selector,"--nocapture","--test-threads=1"]
    result=subprocess.run(command,text=True,stdout=subprocess.PIPE,stderr=subprocess.STDOUT)
    (evidence/(selector.replace("::","-")+".log")).write_text(result.stdout)
    summary=re.search(r"test result: ok\. (\d+) passed; (\d+) failed; (\d+) ignored;",result.stdout)
    record["cases"].append({"command":command,"exit_code":result.returncode})
    (evidence/"result.json").write_text(json.dumps(record,indent=2)+"\n")
    print(result.stdout,end="")
    assert result.returncode==0 and summary and int(summary[1])==count and int(summary[2])==0 and int(summary[3])==0, "focused SDK coverage failed or was skipped"
assert hashlib.sha256(binary.read_bytes()).hexdigest()==record["sha256"], "compiled artifact changed during qualification"
PYVERIFY
