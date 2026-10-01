#!/usr/bin/env python3
"""Explicit foundation build and prebuilt-artifact integrity qualification."""
import argparse
import copy
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import stat
import subprocess
import sys
import tempfile

ROOT = Path(__file__).resolve().parents[3]
NAMES = ("paxd", "attestor", "layerxd", "layerx-genesis-build",
         "layerx-module-registry", "layerx-custody-proof")
GO_PATHS = ("chain.mk", "go.mod", "go.sum", "admin", "consensus", "custodyproof",
            "daemon", "engine", "interchain", "layerxproof", "modules", "node",
            "precompiles", "ratelimiter", "rpc", "sdk", "storage", "store", "sync",
            "types", "utils", "wasm", "wasm-runtime", "wasmbinding", "tools/chain")
NATIVE_PATHS = ("Makefile", "src", "include", "cmd", "programs", "agent",
                "contracts/config/checkpoint-settlement.json")
PATHS = {name: GO_PATHS if name in ("paxd", "layerx-custody-proof") else
         GO_PATHS + ("human/wallet/attestor",) if name == "attestor" else NATIVE_PATHS
         for name in NAMES}
LIBRARIES = (
    "wasm-runtime/internal/api/libwasmvm.x86_64.so",
    "wasm/x/wasm/artifacts/v152/api/libwasmvm152.x86_64.so",
    "wasm/x/wasm/artifacts/v155/api/libwasmvm155.x86_64.so",
)
HEX = re.compile(r"[0-9a-f]{40}\Z")


class Refused(Exception):
    pass


def require(condition, message):
    if not condition:
        raise Refused(message)


def command(argv, cwd=ROOT, capture=False, env=None):
    return subprocess.run(argv, cwd=cwd, env=env, check=True, text=True,
                          stdout=subprocess.PIPE if capture else None,
                          stderr=subprocess.PIPE if capture else None)


def git(*args):
    return command(["git", *args], capture=True).stdout.strip()


def clean_identity():
    require(not git("status", "--porcelain", "--untracked-files=normal"), "source must be clean")
    return git("rev-parse", "HEAD"), git("rev-parse", "HEAD^{tree}")


def source_binding(revision, paths):
    require(HEX.fullmatch(revision) is not None, "invalid source revision")
    data = command(["git", "ls-tree", "-r", "--full-tree", "-z", revision, "--", *paths], capture=True).stdout
    require(bool(data), "source binding is empty")
    return hashlib.sha256(data.encode()).hexdigest()


def sha256(path):
    with path.open("rb") as stream:
        return hashlib.file_digest(stream, "sha256").hexdigest()


def safe_path(path):
    require(path.is_absolute() and ".." not in path.parts, "path must be absolute and normalized")
    for part in (path, *path.parents):
        require(not part.is_symlink(), "symlink path refused")
    return path


def private_directory(path):
    safe_path(path)
    info = path.stat()
    require(stat.S_ISDIR(info.st_mode) and info.st_uid == os.geteuid() and
            stat.S_IMODE(info.st_mode) == 0o700, "directory must be owned mode0700")


def output_path(path):
    safe_path(path)
    require(path != ROOT and ROOT not in path.parents, "repository output refused")
    require(len(path.parts) > 2 and path.parts[1] not in
            ("etc", "usr", "bin", "sbin", "lib", "lib64", "boot", "dev", "proc", "sys"),
            "protected system output refused")
    private_directory(path.parent)
    require(not path.exists(), "existing output refused")


def regular_private(path, executable=False):
    safe_path(path)
    info = path.lstat()
    require(stat.S_ISREG(info.st_mode) and info.st_nlink == 1 and
            info.st_uid == os.geteuid() and info.st_mode & 0o077 == 0,
            "artifact must be an owned private regular file")
    require(info.st_size > 0, "empty artifact")
    if executable:
        require(bool(info.st_mode & stat.S_IXUSR), "non-executable artifact")
        with path.open("rb") as stream:
            require(stream.read(4) == b"\x7fELF", "artifact is not an ELF executable")


def go_tool():
    found = os.environ.get("PAXEER_GO") or shutil.which("go")
    require(bool(found), "Go toolchain required")
    version = command([found, "version"], capture=True).stdout.strip()
    required = re.search(r"^go (\S+)$", (ROOT / "go.mod").read_text(), re.M).group(1)
    require(version.startswith("go version go" + required + " "), "Go version differs from source contract")
    return str(Path(found).resolve()), version


def go_metadata(path, revision, tool):
    output = command([tool, "version", "-m", str(path)], capture=True).stdout
    require("vcs.revision=" + revision in output and "vcs.modified=false" in output,
            "Go executable source metadata mismatch")
    return output


def validate(manifest, directory, selected_revision=None):
    private_directory(directory)
    require(manifest.get("version") == 1, "unsupported manifest version")
    revision = manifest.get("source_revision", "")
    require(HEX.fullmatch(revision) is not None, "invalid source revision")
    require(manifest.get("source_tree") == git("rev-parse", revision + "^{tree}"), "source tree mismatch")
    selected_revision = selected_revision or clean_identity()[0]
    artifacts = manifest.get("artifacts", {})
    require(set(artifacts) == set(NAMES), "incomplete artifact set")
    for name in NAMES:
        record = artifacts[name]
        require(record.get("source_revision") == revision and
                record.get("source_tree") == manifest["source_tree"], "artifact source mismatch")
        require(record.get("source_paths") == list(PATHS[name]), "artifact source paths mismatch")
        binding = source_binding(revision, PATHS[name])
        require(record.get("source_binding") == binding and
                source_binding(selected_revision, PATHS[name]) == binding,
                "artifact source is incompatible with selected checkout")
        path = Path(record["path"])
        require(path == directory / name, "artifact path outside bundle")
        regular_private(path, executable=True)
        require(record.get("sha256") == sha256(path), "artifact digest mismatch")
    libraries = manifest.get("runtime_libraries", [])
    require(len(libraries) == len(LIBRARIES), "incomplete runtime libraries")
    checksums = dict((line.split()[1], line.split()[0]) for line in
                     git("show", revision + ":wasm-runtime/libwasmvm-linux.sha256").splitlines())
    for source, record in zip(LIBRARIES, libraries):
        path = directory / Path(source).name
        require(record.get("path") == str(path) and record.get("source_revision") == revision,
                "library source mismatch")
        regular_private(path)
        require(record.get("sha256") == sha256(path) == checksums[path.name], "runtime library digest mismatch")
    require(manifest.get("build", {}).get("exit_code") == 0 and
            manifest["build"].get("commands"), "missing successful build provenance")
    return manifest


def build(destination):
    output_path(destination)
    revision, tree = clean_identity()
    tool, version = go_tool()
    environment = dict(os.environ, PATH=str(Path(tool).parent) + os.pathsep + os.environ.get("PATH", ""),
                       PAXEER_GO=tool, GOMAXPROCS="5", CARGO_BUILD_JOBS="5", GOFLAGS="-mod=readonly -p=5")
    stage = Path(tempfile.mkdtemp(prefix=".foundation-build-", dir=destination.parent))
    commands = [
        ["make", "-f", "chain.mk", "build"],
        ["make", "-j5", "-B", "LXP_REVISION=" + revision, "PAXEER_GO=" + tool,
         "PAXEER_GO_JOBS=5", "layerxd", "layerx-genesis-build", "layerx-module-registry", "custody-proof-build"],
        [tool, "build", "-trimpath", "-ldflags=-s -w", "-o", str(stage / "attestor"), "./cmd/attestor"],
    ]
    manifest = {"version": 1, "source_revision": revision, "source_tree": tree,
                "artifacts": {}, "runtime_libraries": [],
                "build": {"exit_code": None, "commands": commands, "go_version": version,
                          "rustc_version": command(["rustc", "--version"], capture=True).stdout.strip(),
                          "cc_version": command([os.environ.get("CC", "cc"), "--version"], capture=True).stdout.splitlines()[0]}}
    try:
        for index, argv in enumerate(commands):
            print("BUILD " + json.dumps(argv), flush=True)
            env = dict(environment)
            cwd = ROOT
            if index == 2:
                env.update(CGO_ENABLED="0", GOOS="linux")
                cwd = ROOT / "human/wallet/attestor"
            command(argv, cwd=cwd, env=env)
        require(clean_identity() == (revision, tree), "source changed during build")
        for name in NAMES:
            if name != "attestor":
                source = ROOT / "build" / ("paxd" if name == "paxd" else "bin/" + name)
                require(source.is_file() and not source.is_symlink() and os.access(source, os.X_OK), "missing built executable")
                shutil.copyfile(source, stage / name)
            os.chmod(stage / name, 0o500)
            record = {"path": str(destination / name), "sha256": sha256(stage / name),
                      "source_revision": revision, "source_tree": tree,
                      "source_paths": list(PATHS[name]), "source_binding": source_binding(revision, PATHS[name])}
            if name in ("paxd", "attestor", "layerx-custody-proof"):
                record["go_build_metadata"] = go_metadata(stage / name, revision, tool)
            manifest["artifacts"][name] = record
        for relative in LIBRARIES:
            source = ROOT / relative
            shutil.copyfile(source, stage / source.name)
            os.chmod(stage / source.name, 0o400)
            manifest["runtime_libraries"].append({"path": str(destination / source.name),
                "sha256": sha256(stage / source.name), "source_revision": revision})
        manifest["build"]["exit_code"] = 0
        (stage / "manifest.json").write_text(json.dumps(manifest, indent=2) + "\n")
        os.chmod(stage / "manifest.json", 0o600)
        staged_manifest = copy.deepcopy(manifest)
        for record in staged_manifest["artifacts"].values():
            record["path"] = str(stage / Path(record["path"]).name)
        for record in staged_manifest["runtime_libraries"]:
            record["path"] = str(stage / Path(record["path"]).name)
        validate(staged_manifest, stage, revision)
        output_path(destination)
        os.rename(stage, destination)
        print("FOUNDATION_ARTIFACT_MANIFEST=" + str(destination / "manifest.json"), flush=True)
    finally:
        if stage.exists():
            shutil.rmtree(stage)


def verify(manifest_path, evidence):
    private_directory(evidence)
    cases = []
    result = {"task": "24.13", "command": "timeout 10m python3 tools/bringup/tests/foundation-artifacts.py --verify",
              "cases": cases, "tests": 0, "skipped": 0, "exit_code": 1,
              "qualification": "artifact identity only; no executable or service launched"}
    def case(name, action, refused=False):
        try:
            action()
        except (Refused, OSError, ValueError, KeyError, subprocess.CalledProcessError):
            if not refused:
                raise
        else:
            require(not refused, name + " unexpectedly accepted")
        cases.append({"name": name, "passed": True})
        print("PASS " + name, flush=True)
    try:
        regular_private(manifest_path)
        manifest = json.loads(manifest_path.read_text())
        revision = clean_identity()[0]
        result.update(source_revision=revision, artifact_manifest=str(manifest_path))
        case("complete-six-real-executables", lambda: validate(manifest, manifest_path.parent, revision))
        tool, _ = go_tool()
        for name in ("paxd", "attestor", "layerx-custody-proof"):
            case(name + "-embedded-source", lambda name=name: go_metadata(Path(manifest["artifacts"][name]["path"]), manifest["source_revision"], tool))
        with tempfile.TemporaryDirectory(prefix="foundation-negative-", dir=evidence) as temporary:
            root = Path(temporary)
            clone = root / "bundle"
            shutil.copytree(manifest_path.parent, clone)
            copied = copy.deepcopy(manifest)
            for record in copied["artifacts"].values():
                record["path"] = str(clone / Path(record["path"]).name)
            for record in copied["runtime_libraries"]:
                record["path"] = str(clone / Path(record["path"]).name)
            check = lambda value=copied: validate(value, clone, revision)
            case("actual-bundle-copy", check)
            target = clone / "layerx-module-registry"
            os.chmod(target, 0o700)
            with target.open("r+b") as stream:
                byte = stream.read(1)
                stream.seek(0)
                stream.write(bytes([byte[0] ^ 1]))
            case("tampered-executable-refused", check, True)
            shutil.copyfile(manifest_path.parent / target.name, target)
            os.chmod(target, 0o500)
            hidden = clone / "saved"
            target.rename(hidden)
            case("missing-executable-refused", check, True)
            target.symlink_to(hidden)
            case("symlink-executable-refused", check, True)
            target.unlink(); hidden.rename(target)
            os.chmod(target, 0o400)
            case("non-executable-refused", check, True)
            os.chmod(target, 0o500)
            os.chmod(clone, 0o755)
            case("unprotected-bundle-refused", check, True)
            os.chmod(clone, 0o700)
            wrong = copy.deepcopy(copied); wrong["source_revision"] = "0" * 40
            case("wrong-source-refused", lambda: validate(wrong, clone, revision), True)
            wrong = copy.deepcopy(copied); wrong["source_tree"] = "0" * 40
            case("wrong-tree-refused", lambda: validate(wrong, clone, revision), True)
            wrong = copy.deepcopy(copied); wrong["artifacts"]["paxd"]["source_binding"] = "0" * 64
            case("wrong-dependency-source-refused", lambda: validate(wrong, clone, revision), True)
            wrong = copy.deepcopy(copied); del wrong["artifacts"]["attestor"]
            case("incomplete-manifest-refused", lambda: validate(wrong, clone, revision), True)
            wrong = copy.deepcopy(copied); wrong["build"]["exit_code"] = 1
            case("unproven-build-refused", lambda: validate(wrong, clone, revision), True)
            case("existing-output-refused", lambda: output_path(clone), True)
            case("repository-output-refused", lambda: output_path(ROOT / "foundation-artifacts"), True)
            case("system-output-refused", lambda: output_path(Path("/usr/local/foundation-artifacts")), True)
            case("new-private-output-accepted", lambda: output_path(root / "new-bundle"))
        result["exit_code"] = 0
    except (Refused, OSError, ValueError, KeyError, subprocess.CalledProcessError) as error:
        result["failure"] = str(error)
        print("foundation artifacts refused: " + str(error), file=sys.stderr)
    result["tests"] = len(cases)
    (evidence / "result.json").write_text(json.dumps(result, indent=2) + "\n")
    os.chmod(evidence / "result.json", 0o600)
    print(f"PAXEER_X_GATE tests={len(cases)} skipped=0")
    return result["exit_code"]


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    modes = parser.add_mutually_exclusive_group(required=True)
    modes.add_argument("--build", action="store_true")
    modes.add_argument("--verify", action="store_true")
    parser.add_argument("--output", type=Path)
    parser.add_argument("--manifest", type=Path, default=os.environ.get("PAXEER_X_FOUNDATION_ARTIFACT_MANIFEST"))
    parser.add_argument("--evidence", type=Path, default=os.environ.get("PAXEER_X_EVIDENCE_DIR"))
    args = parser.parse_args()
    os.umask(0o077)
    try:
        if args.build:
            require(args.output is not None, "--output is required for the explicit build")
            build(args.output)
            return 0
        require(args.manifest is not None and args.evidence is not None, "prebuilt manifest and private evidence directory are required")
        return verify(args.manifest, args.evidence)
    except (Refused, OSError, ValueError, KeyError, subprocess.CalledProcessError) as error:
        print("foundation artifacts refused: " + str(error), file=sys.stderr)
        if args.verify:
            print("PAXEER_X_GATE tests=0 skipped=0")
        return 1


if __name__ == "__main__":
    sys.exit(main())
