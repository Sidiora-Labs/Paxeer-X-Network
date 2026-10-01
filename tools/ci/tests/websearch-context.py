#!/usr/bin/env python3
import hashlib
import json
import os
from pathlib import Path, PurePosixPath
import re
import shlex
import stat
import subprocess
import sys
import tarfile
import tempfile
import tomllib

ROOT = Path(__file__).resolve().parents[3]
DOCKERFILE = "docker/layerx/Dockerfile"
IGNORE = DOCKERFILE + ".dockerignore"
EXTRA = {
    "include/layerx/lxp_result.h", "include/layerx/programs.h",
    "agent/deny.toml", "agent/unsafe-allowlist.toml",
    "contracts/config/checkpoint-settlement.json",
    "interop/specs/vendor/x402/x402-specification-v2.md",
    "interop/Cargo.lock", "rust-toolchain.toml",
}
CHECKS = 0


class Refused(Exception):
    pass


def require(condition, reason):
    if not condition:
        raise Refused(reason)


def command(argv):
    result = subprocess.run(argv, cwd=ROOT, capture_output=True, text=True,
                            stdin=subprocess.DEVNULL, timeout=60, check=False)
    require(result.returncode == 0, "command failed: " + shlex.join(argv))
    return result.stdout.strip()


def relative(path):
    resolved = path.resolve()
    require(resolved.is_relative_to(ROOT), "source escapes repository: " + str(path))
    require(not path.is_symlink(), "symlink source refused: " + str(path))
    return resolved.relative_to(ROOT).as_posix()


def glob_regex(pattern):
    output = ""
    index = 0
    while index < len(pattern):
        char = pattern[index]
        if pattern[index:index + 3] == "**/":
            output += "(?:.*/)?"
            index += 3
        elif pattern[index:index + 2] == "**":
            output += ".*"
            index += 2
        elif char == "*":
            output += "[^/]*"
            index += 1
        elif char == "?":
            output += "[^/]"
            index += 1
        else:
            require(char not in "[]\\", "unsupported Dockerignore pattern: " + pattern)
            output += re.escape(char)
            index += 1
    return re.compile("^" + output + "$")


def rules(text):
    result = []
    for raw in text.splitlines():
        if not raw or raw.startswith("#"):
            continue
        value = raw.strip()
        if not value or value == ".":
            continue
        allow = value.startswith("!")
        value = value[1:] if allow else value
        value = value.strip("/")
        require(value and ".." not in PurePosixPath(value).parts,
                "unsafe Dockerignore rule")
        result.append((allow, glob_regex(value)))
    return result


def admitted(name, policy):
    parents = [name] + [str(p) for p in PurePosixPath(name).parents if str(p) != "."]
    keep = True
    for allow, pattern in policy:
        if any(pattern.fullmatch(p) for p in parents):
            keep = allow
    return keep


def dependency_inputs(tracked):
    documents = {}
    required = set(EXTRA)

    def read(path):
        name = relative(path)
        require(name in tracked, "missing tracked Cargo manifest: " + name)
        required.add(name)
        if name not in documents:
            documents[name] = tomllib.loads(path.read_text())
        return documents[name]

    def workspace(path):
        package = read(path).get("package", {})
        if "workspace" in package:
            parent = (path.parent / package["workspace"] / "Cargo.toml").resolve()
            return parent, read(parent)["workspace"]
        for parent in path.parent.parents:
            if not parent.is_relative_to(ROOT):
                break
            manifest = parent / "Cargo.toml"
            if manifest.is_file():
                value = read(manifest)
                if "workspace" in value:
                    return manifest, value["workspace"]
        raise Refused("Cargo workspace unavailable: " + relative(path))

    workspace_file = ROOT / "interop/Cargo.toml"
    root = read(workspace_file)["workspace"]
    pending = []
    for member in root["members"]:
        matches = list((ROOT / "interop").glob(member + "/Cargo.toml"))
        require(matches, "missing Cargo workspace member: " + member)
        pending.extend(matches)
    visited = set()
    while pending:
        path = pending.pop().resolve()
        name = relative(path)
        if name in visited:
            continue
        visited.add(name)
        value = read(path)
        workspace_path, inherited = workspace(path)
        prefix = relative(path.parent) + "/"
        sources = {p for p in tracked if p.startswith(prefix) and p.endswith(".rs")}
        require(sources, "missing real Cargo source: " + name)
        required.update(sources)
        build = value.get("package", {}).get("build", "build.rs")
        if isinstance(build, str) and (path.parent / build).is_file():
            required.add(relative(path.parent / build))
        for kind in ("lib", "bin", "bench", "example", "test"):
            targets = value.get(kind, [])
            if isinstance(targets, dict):
                targets = [targets]
            for target in targets:
                if "path" in target:
                    required.add(relative(path.parent / target["path"]))
        for kind in ("dependencies", "build-dependencies", "dev-dependencies"):
            tables = [value.get(kind, {})]
            tables += [v.get(kind, {}) for v in value.get("target", {}).values()]
            for table in tables:
                for dep, options in table.items():
                    if not isinstance(options, dict):
                        continue
                    base = path.parent
                    if options.get("workspace"):
                        require(dep in inherited.get("dependencies", {}),
                                "missing inherited dependency: " + dep)
                        options = inherited["dependencies"][dep]
                        base = workspace_path.parent
                    if isinstance(options, dict) and "path" in options:
                        pending.append((base / options["path"] / "Cargo.toml").resolve())
    for name in tuple(required):
        require(name in tracked, "missing compile input: " + name)
    return required, sorted(visited)


def validate_context(required, available):
    missing = sorted(required - available)
    require(not missing, "Docker context missing compile input: " + (missing[0] if missing else ""))


def check(condition, reason):
    global CHECKS
    require(condition, reason)
    CHECKS += 1


def negatives(required, available, policy):
    global CHECKS
    for name in sorted(required):
        try:
            validate_context(required, available - {name})
        except Refused as error:
            require(name in str(error), "imprecise missing-input refusal: " + name)
            CHECKS += 1
        else:
            raise Refused("missing input accepted: " + name)
    forbidden = [".env", ".env.production", "interop/.env", "agent/secret.env",
                 "interop/crates/x-websearch/.env.local", ".chat/owner.kvx",
                 ".logs/raw.log", "private-evidence/candidate.json", "secrets/token",
                 "agent/crates/layerx-types/private.key", "interop/key.pem",
                 "interop/target/release/x-websearch", ".git/config",
                 "interop/.x-websearch-target/release/x-websearch",
                 "human/node_modules/huge", "agent/.cache/build"]
    for name in forbidden:
        check(not admitted(name, policy), "sensitive/cache path admitted: " + name)
    check(admitted("interop/specs/vendor/x402/x402-specification-v2.md", policy),
          "compiled x402 document excluded")
    check(not admitted("interop/specs/vendor/x402/unrelated.md", policy),
          "unrelated Markdown admitted")


def main():
    revision = command(["git", "rev-parse", "HEAD"])
    tree = command(["git", "rev-parse", "HEAD^{tree}"])
    require(not command(["git", "status", "--porcelain", "--untracked-files=normal"]),
            "dirty candidate source")
    tracked = set(command(["git", "ls-files", "-z"]).split("\0")) - {""}
    policy = rules((ROOT / IGNORE).read_text())
    required, manifests = dependency_inputs(tracked)
    available = {name for name in tracked if admitted(name, policy)}
    validate_context(required, available)
    check(True, "production context closure")
    negatives(required, available, policy)
    dockerfile = (ROOT / DOCKERFILE).read_text()
    check(bool(re.search(r"FROM docker.io/rust:1\.91\.1-bookworm@sha256:[0-9a-f]{64} AS x-websearch", dockerfile)),
          "x-websearch base image is not pinned")
    check("cargo build --locked --manifest-path interop/Cargo.toml --release --package x-websearch --bin x-websearch" in dockerfile,
          "production Cargo build command missing")
    check("test -x /src/.x-websearch-target/release/x-websearch" in dockerfile,
          "expected executable check missing")
    evidence = Path(os.environ["PAXEER_X_EVIDENCE_DIR"]).resolve()
    require(not evidence.is_relative_to(ROOT), "evidence must be outside source")
    info = evidence.stat()
    require(stat.S_ISDIR(info.st_mode) and info.st_uid == os.geteuid() and not info.st_mode & 0o077,
            "evidence directory must be private and caller-owned")
    wrapper = shlex.split(os.environ.get("PAXEER_X_BUILD_WRAPPER", ""))
    require(wrapper and Path(wrapper[0]).is_file() and os.access(wrapper[0], os.X_OK),
            "explicit executable build wrapper required")
    jobs = os.environ.get("CARGO_BUILD_JOBS", "")
    require(jobs.isdigit() and int(jobs) > 0, "explicit positive CARGO_BUILD_JOBS required")
    docker = os.environ.get("PAXEER_X_DOCKER", "docker")
    command([docker, "version", "--format", "{{.Server.Version}}"])
    with tempfile.TemporaryDirectory(prefix="websearch-context-", dir=evidence) as scratch:
        scratch = Path(scratch)
        archive = scratch / "context.tar"
        entries = sorted(available | {DOCKERFILE, IGNORE})
        with tarfile.open(archive, "w") as output:
            for name in entries:
                path = ROOT / name
                require(path.is_file() and not path.is_symlink(), "non-regular context input: " + name)
                relative(path)
                output.add(path, arcname=name, recursive=False)
        with archive.open("rb") as stream:
            context_sha = hashlib.file_digest(stream, "sha256").hexdigest()
        iid = scratch / "image-id"
        argv = wrapper + [docker, "build", "--progress=plain", "--target", "x-websearch",
                          "--file", DOCKERFILE, "--build-arg", "CARGO_BUILD_JOBS=" + jobs,
                          "--label", "org.opencontainers.image.revision=" + revision,
                          "--iidfile", str(iid), "-"]
        print("build-command: " + shlex.join(argv), flush=True)
        record = {"revision": revision, "tree": tree, "context_sha256": context_sha,
                  "manifests": manifests, "input_count": len(entries), "command": argv,
                  "stage": "x-websearch", "exit_code": None, "image_id": None}
        destination = evidence / ("websearch-" + revision + ".json")
        require(not destination.exists(), "evidence already exists for this revision")
        environment = dict(os.environ, DOCKER_BUILDKIT="1")
        with archive.open("rb") as stream:
            result = subprocess.run(argv, cwd=ROOT, stdin=stream, env=environment, check=False)
        record["exit_code"] = result.returncode
        if result.returncode == 0 and iid.is_file():
            record["image_id"] = iid.read_text().strip()
        with destination.open("x") as output:
            os.chmod(destination, 0o600)
            json.dump(record, output, indent=2)
            output.write("\n")
        require(result.returncode == 0, "actual x-websearch Docker build failed: " + str(result.returncode))
        require(bool(re.fullmatch(r"sha256:[0-9a-f]{64}", record["image_id"] or "")),
                "Docker build produced no image identity")
        require(command(["git", "rev-parse", "HEAD"]) == revision and
                not command(["git", "status", "--porcelain", "--untracked-files=normal"]),
                "source changed during qualification")
        check(True, "actual Docker stage build")
        print("image-id: " + record["image_id"])
        print("evidence: " + str(destination))


if __name__ == "__main__":
    os.umask(0o077)
    status = 0
    try:
        main()
    except (Refused, OSError, ValueError, KeyError, subprocess.SubprocessError) as error:
        print("websearch-context: refused: " + str(error), file=sys.stderr)
        status = 1
    finally:
        print(f"PAXEER_X_GATE tests={CHECKS} skipped=0")
    sys.exit(status)
