#!/usr/bin/env python3
import hashlib
import json
import os
from pathlib import Path
import shutil
import stat
import subprocess
import sys
import tempfile

ROOT = Path(__file__).resolve().parents[3]
WEB = ROOT / "human/apps/web"
SOURCE_PATHS = ["human/apps/web", "agent/sdk/typescript", "tools/paxeer-x/gates/104.13.1.sh",
                "tools/paxeer-x/builders/104.13.1.py",
                "human/crates/layerx-human-service/src/server/identity_dispatch.rs"]


def refuse(reason):
    raise SystemExit("settings artifacts refused: " + reason)


def plain_file(path, private=False):
    metadata = path.lstat()
    if (path.resolve() != path or not stat.S_ISREG(metadata.st_mode)
            or metadata.st_nlink != 1 or metadata.st_uid != os.geteuid()
            or (private and stat.S_IMODE(metadata.st_mode) != 0o600)):
        refuse("file ownership, type or permissions")
    return path


def digest(path):
    plain_file(path)
    hasher = hashlib.sha256()
    with path.open("rb") as stream:
        for block in iter(lambda: stream.read(1048576), b""):
            hasher.update(block)
    return hasher.hexdigest()


def source_binding():
    revision = subprocess.check_output(["git", "rev-parse", "HEAD"], cwd=ROOT, text=True).strip()
    if subprocess.check_output(["git", "status", "--porcelain", "--untracked-files=all", "--",
                                *SOURCE_PATHS], cwd=ROOT):
        refuse("relevant source must be committed and clean")
    for filename in (".env", ".env.local", ".env.production", ".env.production.local"):
        if (WEB / filename).exists() or (WEB / filename).is_symlink():
            refuse("implicit build environment files are not permitted")
    paths = subprocess.check_output(["git", "ls-files", "-z", "--", *SOURCE_PATHS], cwd=ROOT).decode().split("\0")
    sources = {}
    for relative in filter(None, paths):
        if Path(relative).name.startswith(".env"):
            refuse("environment files cannot be source evidence")
        sources[relative] = digest(ROOT / relative)
    if not sources:
        refuse("missing source inventory")
    return {"revision": revision, "sources": sources}


def artifact_binding():
    directory = WEB / ".next"
    if directory.resolve() != directory or not directory.is_dir():
        refuse("production artifact directory is absent or redirected")
    artifacts = {}
    for name in ("server", "static"):
        tree = directory / name
        if tree.resolve() != tree or not tree.is_dir():
            refuse("missing production " + name + " artifacts")
        files = sorted(tree.rglob("*"))
        count = 0
        for path in files:
            if path.is_symlink():
                refuse("redirected production artifacts")
            if path.is_file():
                artifacts[str(path.relative_to(ROOT))] = digest(path)
                count += 1
        if count == 0:
            refuse("empty production " + name + " artifacts")
    for path in sorted(directory.iterdir()):
        if path.name == "BUILD_ID" or path.suffix == ".json":
            artifacts[str(path.relative_to(ROOT))] = digest(path)
    build_id = directory / "BUILD_ID"
    if not plain_file(build_id).read_text().strip():
        refuse("empty production build identifier")
    return artifacts


def production_environment(output):
    for tool in ("authbind", "certutil", "openssl", "python3", "node", "npm"):
        if shutil.which(tool) is None:
            refuse("missing production prerequisite " + tool)
    if not os.access("/etc/authbind/byport/443", os.X_OK):
        refuse("explicit authbind permission for port 443 is required")
    environment = {key: value for key, value in os.environ.items()
                   if key in ("PATH", "HOME", "LANG", "LC_ALL", "TMPDIR")}
    environment.update(CI="false", NO_COLOR="1", FORCE_COLOR="0", NEXT_TELEMETRY_DISABLED="1")
    configuration = subprocess.check_output(
        ["python3", str(WEB / "e2e/prepare-production.py"), str(ROOT), str(output)],
        cwd=ROOT, env=environment, text=True).splitlines()
    if len(configuration) != 5 or any(not value for value in configuration):
        refuse("production preparation did not return its complete binding")
    origin, service, ca, browser_home, tls = configuration
    environment.update(HUMAN_E2E_REAL_STACK="1", HUMAN_E2E_LOCAL_PRODUCTION="1",
                       HUMAN_E2E_BASE_URL=origin, LAYERX_HUMAN_WEB_ORIGIN=origin,
                       LAYERX_HUMAN_SERVICE_URL=service, NODE_EXTRA_CA_CERTS=ca,
                       HUMAN_E2E_BROWSER_HOME=browser_home, HUMAN_E2E_TLS_CONFIG=tls,
                       LAYERX_RUM_STORAGE_DIRECTORY=str(output / "rum-data"))
    return environment


def output_directory():
    parent = ROOT / "qual-logs"
    parent.mkdir(exist_ok=True)
    if parent.resolve() != parent or parent.stat().st_uid != os.geteuid():
        refuse("production output directory ownership or path")
    return Path(tempfile.mkdtemp(prefix="human-settings-", dir=parent))


def verify_artifacts(filename):
    path = Path(filename)
    if not path.is_absolute() or not path.is_relative_to(ROOT / "qual-logs"):
        refuse("manifest must be an absolute path in the qualification directory")
    plain_file(path, private=True)
    if path.stat().st_size > 16777216:
        refuse("manifest exceeds its bound")
    manifest = json.loads(path.read_text())
    expected_keys = {"version", "producer", "command", "source", "origin", "service", "artifacts"}
    if (not isinstance(manifest, dict) or set(manifest) != expected_keys
            or manifest["version"] != 1
            or manifest["producer"] != "tools/paxeer-x/builders/104.13.1.py"
            or manifest["command"] != ["npm", "--prefix", "human/apps/web", "run", "build"]):
        refuse("unrecognized build producer evidence")
    if manifest["source"] != source_binding():
        refuse("build source does not match the current source")
    if manifest["artifacts"] != artifact_binding():
        refuse("production artifacts differ from the completed build")
    return manifest


def main():
    if len(sys.argv) != 1:
        refuse("build producer accepts no arguments")
    os.umask(0o077)
    source = source_binding()
    output = output_directory()
    environment = production_environment(output)
    command = ["npm", "--prefix", "human/apps/web", "run", "build"]
    subprocess.run(command, cwd=ROOT, env=environment, check=True)
    if source_binding() != source:
        refuse("source changed during the build")
    manifest = {"version": 1, "producer": "tools/paxeer-x/builders/104.13.1.py",
                "command": command, "source": source,
                "origin": environment["LAYERX_HUMAN_WEB_ORIGIN"],
                "service": environment["LAYERX_HUMAN_SERVICE_URL"],
                "artifacts": artifact_binding()}
    target = output / "artifacts.json"
    temporary = output / "artifacts.pending"
    with temporary.open("x") as stream:
        json.dump(manifest, stream, sort_keys=True)
        stream.write("\n")
        stream.flush()
        os.fsync(stream.fileno())
    os.replace(temporary, target)
    print("HUMAN_E2E_SETTINGS_ARTIFACTS=" + str(target))


if __name__ == "__main__":
    main()
