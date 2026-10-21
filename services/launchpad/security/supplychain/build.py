"""Validate inputs before starting a container build. No default mutable base images."""
import argparse, json, pathlib, subprocess
from policy import require,pinned_image
TARGETS={"api":"apid","indexer":"indexerd","pnl-tracker":"pnld","ranking-algo":"rankingd","stats-workers":"statsd","trading-charts":"chartsd","gateway":"mediagatewayd","livestream":"livestreamd","social":"sociald","user":"userd"}
NATIVE_TARGETS = {**TARGETS, "metadata": "metadatad"}
def validate(service,binary,builder,runtime):
    require(TARGETS.get(service)==binary,"unrecognized or separately reviewed service target")
    pinned_image(builder);pinned_image(runtime)
def validate_context(exclusions):
    lines=[line.strip() for line in exclusions.splitlines() if line.strip() and not line.lstrip().startswith("#")]
    require(not any(line.startswith("!") or "\\" in line for line in lines),"unsupported context reinclusion or escape")
    required={".env",".env.*","**/.env*","**/node_modules","**/.git","evidence","**/*.pem","**/*.key",".codify",".cg",".codex","**/token.json","**/coordinator.key"}
    require(required<=set(lines),"unsafe build context exclusions")
def main():
    a=argparse.ArgumentParser();a.add_argument("--service",required=True);a.add_argument("--binary",required=True);a.add_argument("--builder",required=True);a.add_argument("--runtime",required=True);a.add_argument("--context",required=True);a.add_argument("--tag",required=True);x=a.parse_args()
    validate(x.service,x.binary,x.builder,x.runtime)
    root=pathlib.Path(x.context).resolve()
    require((root/"deploy/launchpad/Dockerfile.svc.dockerignore").is_file(),"missing reviewed build context exclusion")
    validate_context((root/"deploy/launchpad/Dockerfile.svc.dockerignore").read_text())
    subprocess.run(["docker","build","-f",str(root/"deploy/launchpad/Dockerfile.svc"),"--build-arg","SERVICE="+x.service,"--build-arg","BIN="+x.binary,"--build-arg","BUILD_IMAGE="+x.builder,"--build-arg","RUNTIME_IMAGE="+x.runtime,"-t",x.tag,str(root)],check=True,timeout=900)
if __name__=="__main__": main()
