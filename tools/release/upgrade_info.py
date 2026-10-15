#!/usr/bin/env python3

import argparse
import json
import os
from pathlib import Path
import re
import sys
import tempfile

MAX_HEIGHT = (1 << 63) - 1
RELEASE_URL = "https://github.com/Sidiora-Labs/Paxeer-X-Network/releases/download"


class ArgumentParser(argparse.ArgumentParser):
    def error(self, message):
        raise ValueError(message)


def release_identity():
    identity = json.loads(Path(__file__).with_name("version.json").read_text())
    if not isinstance(identity, dict):
        raise ValueError("invalid release identity")
    for key in ("version", "upgrade"):
        value = identity.get(key)
        if not isinstance(value, str) or not re.fullmatch(
            r"v[0-9]+\.[0-9]+\.[0-9]+", value
        ):
            raise ValueError(f"invalid release {key}")
    return identity


def validate_name(name, identity):
    if name != identity["upgrade"]:
        raise ValueError(
            f"name {name} is not the release upgrade {identity['upgrade']}"
        )
    return name


def validate_checksum(checksum):
    if not isinstance(checksum, str) or not re.fullmatch(r"[0-9a-f]{64}", checksum):
        raise ValueError("checksum must be 64 lowercase hex digits")
    return checksum


def asset_url(identity):
    version = identity["version"]
    return f"{RELEASE_URL}/paxeer-network/{version}/paxd-{version}-linux-amd64"


def write_json(path, document, mode):
    encoded = json.dumps(document, indent=2) + "\n"
    path.parent.mkdir(parents=True, exist_ok=True)
    temporary = None
    try:
        with tempfile.NamedTemporaryFile(
            mode="w",
            encoding="utf-8",
            dir=path.parent,
            prefix=f".{path.name}.",
            delete=False,
        ) as output:
            temporary = Path(output.name)
            os.chmod(temporary, mode)
            output.write(encoded)
            output.flush()
            os.fsync(output.fileno())
        os.replace(temporary, path)
    finally:
        if temporary is not None:
            temporary.unlink(missing_ok=True)


def release(args, identity):
    name = validate_name(
        identity["upgrade"] if args.name is None else args.name, identity
    )
    expected_tag = f"paxeer-network/{identity['version']}"
    if args.tag is not None and args.tag != expected_tag:
        raise ValueError(f"tag {args.tag} is not the release tag {expected_tag}")
    asset = f"paxd-{identity['version']}-linux-amd64"
    checksums = []
    for line in args.sums.read_text(encoding="utf-8").splitlines():
        match = re.fullmatch(r"(\S+) [ *](.+)", line)
        if match and match[2] == asset:
            checksums.append(validate_checksum(match[1]))
    if not checksums:
        raise ValueError(f"no {asset} line in SHA256SUMS")
    if len(checksums) != 1:
        raise ValueError(f"multiple {asset} lines in SHA256SUMS")
    info = {
        "binaries": {
            "linux/amd64": f"{asset_url(identity)}?checksum=sha256:{checksums[0]}"
        }
    }
    document = {"name": name, "info": json.dumps(info, separators=(",", ":"))}
    if args.out is None:
        print(json.dumps(document, indent=2))
    else:
        write_json(args.out, document, 0o644)


def release_document_name(path, checksum, identity):
    validate_checksum(checksum)
    document = json.loads(path.read_text(encoding="utf-8"))
    if not isinstance(document, dict) or set(document) != {"name", "info"}:
        raise ValueError("release document must contain name and info")
    name = validate_name(document["name"], identity)
    if not isinstance(document["info"], str):
        raise ValueError("release document info must be a JSON string")
    info = json.loads(document["info"])
    if not isinstance(info, dict) or not isinstance(info.get("binaries"), dict):
        raise ValueError("release document must contain binaries")
    url = info["binaries"].get("linux/amd64")
    prefix = f"{asset_url(identity)}?checksum=sha256:"
    if not isinstance(url, str) or not url.startswith(prefix):
        raise ValueError("release document must name the release paxd asset")
    actual_checksum = validate_checksum(url[len(prefix) :])
    if actual_checksum != checksum:
        raise ValueError("release document checksum differs")
    return name


def validate_height(height, label):
    if height < 1:
        raise ValueError(f"{label} must be positive")
    if height > MAX_HEIGHT:
        raise ValueError(f"{label} exceeds int64")
    return height


def node(args, identity):
    name = validate_name(
        identity["upgrade"] if args.name is None else args.name, identity
    )
    if (args.release_doc is None) != (args.sha256 is None):
        raise ValueError("--release-doc and --sha256 must be supplied together")
    if args.release_doc is not None:
        name = release_document_name(args.release_doc, args.sha256, identity)
    height = args.height
    if height is not None:
        validate_height(height, "height")
    if args.halt_height is not None:
        halt = validate_height(args.halt_height, "halt height")
        if height is not None and height != halt + 1:
            raise ValueError("height must be halt height + 1")
        height = validate_height(halt + 1, "height")
    if height is None:
        raise ValueError("--height or --halt-height is required")
    write_json(
        args.home / "data" / "upgrade-info.json",
        {"name": name, "height": height},
        0o600,
    )


def main():
    parser = ArgumentParser(prog="upgrade_info")
    commands = parser.add_subparsers(dest="command", required=True)
    release_parser = commands.add_parser("release")
    release_parser.add_argument("--sums", type=Path, required=True)
    release_parser.add_argument("--tag")
    release_parser.add_argument("--name")
    release_parser.add_argument("--out", type=Path)
    release_parser.set_defaults(run=release)
    node_parser = commands.add_parser("node")
    node_parser.add_argument("--home", type=Path, required=True)
    node_parser.add_argument("--name")
    node_parser.add_argument("--height", type=int)
    node_parser.add_argument("--halt-height", type=int)
    node_parser.add_argument("--release-doc", type=Path)
    node_parser.add_argument("--sha256")
    node_parser.set_defaults(run=node)
    try:
        args = parser.parse_args()
        args.run(args, release_identity())
    except (OSError, ValueError, UnicodeError) as error:
        print(f"upgrade_info: {error}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
