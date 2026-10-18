from __future__ import annotations

from dataclasses import dataclass
import hashlib
import json
import os
from pathlib import Path
import re
from urllib.parse import urlsplit

ROOT = Path(__file__).resolve().parents[3]
HEX = re.compile(r"^[0-9a-f]{64}$")


def canonical(value):
    return json.dumps(value, separators=(",", ":"), ensure_ascii=False, allow_nan=False).encode()


def digest(value):
    return hashlib.sha256(value).hexdigest()


def unique(pairs):
    result = {}
    for key, value in pairs:
        if key in result:
            raise ValueError("duplicate input key")
        result[key] = value
    return result


def bound_document(path, expected, *, maximum_bytes=4 * 1024 * 1024):
    path = Path(path)
    if not HEX.fullmatch(expected) or path.is_symlink() or not path.is_file() or path.stat().st_size > maximum_bytes:
        raise ValueError("bounded source-bound document required")
    raw = path.read_bytes()
    if digest(raw) != expected:
        raise ValueError("document digest differs")
    return json.loads(raw, object_pairs_hook=unique)


def origin(value, schemes=("https",)):
    parsed = urlsplit(value)
    if parsed.scheme not in schemes or not parsed.hostname or parsed.username or parsed.password or parsed.path not in ("", "/") or parsed.query or parsed.fragment or value.rstrip("/") != value:
        raise ValueError("exact permitted origin required")
    return parsed


def validate_v2(name, value):
    from jsonschema import Draft202012Validator, FormatChecker
    definitions = json.loads((ROOT / "spec/production-system/integration/contracts/v2/schema-definitions.json").read_text())
    if name not in definitions["$defs"]:
        raise ValueError("unknown v2 contract")
    schema = {**definitions, "$ref": "#/$defs/" + name}
    Draft202012Validator(schema, format_checker=FormatChecker()).validate(value)
    return value


def source_digest():
    directory = Path(__file__).parent
    files = sorted(directory.glob("*.py")) + sorted(directory.glob("phase2_browser.*")) + sorted(directory.glob("phase-*-scenario-manifest.json")) + sorted(directory.glob("phase-*-evidence-policy.json")) + sorted(directory.glob("final-*.json")) + sorted(directory.glob("eventdriver/*.go"))
    return digest(canonical({p.name: digest(p.read_bytes()) for p in files}))


@dataclass(frozen=True)
class Bindings:
    candidate: str
    interface: str
    topology: str
    generator: str

    def __post_init__(self):
        if any(not isinstance(v, str) or not HEX.fullmatch(v) for v in self.__dict__.values()):
            raise ValueError("exact candidate/interface/topology/generator hashes required")


class Artifacts:
    def __init__(self, directory, *, phase=2):
        if type(phase) is not int or phase not in (2, 3, 4):
            raise ValueError("exact lane evidence phase required")
        self.directory = Path(directory).resolve()
        allowed = (ROOT / ("evidence/production-system/L48/phase" + str(phase))).resolve()
        if not self.directory.is_relative_to(allowed):
            raise ValueError("lane-owned evidence directory required")
        self.directory.mkdir(parents=True, exist_ok=False)
        self.files = {}

    def rows(self, name):
        if not re.fullmatch(r"[a-z0-9-]+\.ndjson", name) or name in self.files:
            raise ValueError("unique bounded artifact name required")
        output = (self.directory / name).open("x", encoding="utf-8")
        self.files[name] = output
        return output

    @staticmethod
    def append(output, value):
        output.write(canonical(value).decode() + "\n")

    def json(self, name, value):
        if not re.fullmatch(r"[a-z0-9-]+\.json", name):
            raise ValueError("bounded artifact name required")
        with (self.directory / name).open("x") as output:
            output.write(json.dumps(value, indent=2, allow_nan=False) + "\n")

    def close(self):
        for output in self.files.values():
            output.flush()
            os.fsync(output.fileno())
            output.close()

    def checksums(self):
        result = {}
        for path in sorted(self.directory.iterdir()):
            if path.is_file() and path.name != "artifact-sha256.json":
                hasher = hashlib.sha256()
                with path.open("rb") as source:
                    for block in iter(lambda: source.read(1048576), b""):
                        hasher.update(block)
                result[path.name] = hasher.hexdigest()
        return result
