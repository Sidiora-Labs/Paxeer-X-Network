#!/usr/bin/env python3
import argparse
import hashlib
import json
import os
from pathlib import Path
import subprocess
import sys
import tomllib

ROOT = Path(__file__).resolve().parents[3]
PROGRAMS = ROOT / "programs"
EVIDENCE = Path("/root/lx-ops/paxeer-x-integration-2026-10-03")


def registry_packages():
    lock = tomllib.loads((PROGRAMS / "Cargo.lock").read_text())
    return [package for package in lock["package"] if package.get("source", "").startswith("registry+")]


def closure():
    config = tomllib.loads((PROGRAMS / ".cargo/config.toml").read_text())
    if (config["source"]["crates-io"]["replace-with"] != "vendored-sources"
            or config["source"]["vendored-sources"]["directory"] != "vendor"
            or config["net"]["offline"] is not True):
        raise RuntimeError("original strict offline source replacement required")
    packages = registry_packages()
    required = {(package["name"], package["version"]) for package in packages}
    directories = {}
    for directory in (PROGRAMS / "vendor").iterdir():
        manifest = directory / "Cargo.toml"
        if manifest.is_file():
            package = tomllib.loads(manifest.read_text()).get("package", {})
            identity = (package.get("name"), package.get("version"))
            if identity not in required:
                continue
            if identity in directories:
                raise RuntimeError("duplicate vendor identity: " + str(identity))
            directories[identity] = directory
    verified = []
    for package in packages:
        identity = (package["name"], package["version"])
        directory = directories.get(identity)
        if directory is None:
            raise RuntimeError("locked registry package absent: " + str(identity))
        checksums = json.loads((directory / ".cargo-checksum.json").read_text())
        if checksums["package"] != package["checksum"]:
            raise RuntimeError("locked archive checksum mismatch: " + str(identity))
        for name, expected in checksums["files"].items():
            path = directory / name
            if Path(name).name == ".env":
                raise RuntimeError("unexpected credential-named registry input")
            if (not path.is_file() or path.is_symlink()
                    or not path.resolve().is_relative_to(directory.resolve())
                    or hashlib.sha256(path.read_bytes()).hexdigest() != expected):
                raise RuntimeError("vendored file checksum mismatch: " + str(identity) + " " + name)
        verified.append({"name": identity[0], "version": identity[1], "checksum": package["checksum"]})
    return verified


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--build", action="store_true")
    arguments = parser.parse_args()
    environment = dict(os.environ)
    environment["CARGO_BUILD_JOBS"] = "4"
    environment["CARGO_TARGET_DIR"] = "/root/lx-target/arbiter-prestate/rust"
    environment["CARGO_NET_OFFLINE"] = "true"
    if arguments.build:
        command = ["/root/.cargo/bin/cargo", "test", "--locked", "--offline", "--manifest-path", "Cargo.toml", "-p", "layerx-programs-arbiter", "--test", "verified_step", "--no-run"]
        return subprocess.run(command, cwd=PROGRAMS, env=environment, timeout=1100).returncode
    packages = closure()
    command = ["/root/.cargo/bin/cargo", "metadata", "--locked", "--offline", "--manifest-path", "Cargo.toml", "--format-version", "1"]
    with (EVIDENCE / "task-104.35.27-offline-metadata.json").open("w") as output:
        result = subprocess.run(command, cwd=PROGRAMS, env=environment, stdout=output, timeout=540)
    if result.returncode:
        return result.returncode
    (EVIDENCE / "task-104.35.27-closure-proof.json").write_text(json.dumps(packages, indent=2) + "\n")
    print("locked-registry-packages=" + str(len(packages)) + " checksum-closure=verified offline-metadata=0")
    return 0


if __name__ == "__main__":
    try:
        sys.exit(main())
    except (OSError, ValueError, KeyError, RuntimeError, subprocess.SubprocessError) as error:
        print(str(error), file=sys.stderr)
        sys.exit(1)
