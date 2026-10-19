#!/usr/bin/env python3
import hashlib
import json
import os
from pathlib import Path
import re
import stat
import subprocess
import sys

ROOT = Path(__file__).resolve().parents[3]
SCHEMA_TEST = "rpc::tests::schema_lists_every_public_method"
CHAIN_METHODS = ("eth_sendRawTransaction", "eth_call", "pax_sequencerReceipt")


def require(condition, message):
    if not condition:
        raise RuntimeError(message)


def git(*args):
    return subprocess.check_output(["git", *args], cwd=ROOT, text=True).strip()


def protected(path, executable=False):
    info = path.lstat()
    require(
        stat.S_ISREG(info.st_mode) and not info.st_mode & 0o077,
        "artifact must be a protected regular file",
    )
    require(not executable or os.access(path, os.X_OK), "artifact must be executable")


def document(variable):
    value = os.environ.get(variable)
    require(value, variable + " is required")
    path = Path(value)
    protected(path)
    return json.loads(path.read_text())


def artifact(record, revision):
    require(record["source_revision"] == revision, "artifact revision mismatch")
    path = Path(record["path"])
    require(path.is_absolute(), "absolute artifact path required")
    protected(path, True)
    require(
        hashlib.file_digest(path.open("rb"), "sha256").hexdigest() == record["sha256"],
        "artifact digest mismatch",
    )
    return str(path)


def main():
    count = 0
    try:
        require(
            not git("status", "--porcelain", "--untracked-files=no"),
            "clean source required",
        )
        revision = git("rev-parse", "HEAD")
        build = document("PAXEER_X_ROUTER_READINESS_BUILD_MANIFEST")
        require(
            build["source_revision"] == revision
            and build["source_tree"] == git("rev-parse", "HEAD^{tree}")
            and build["build_exit"] == 0,
            "source-bound successful scoped build required",
        )
        executable = artifact(build["artifacts"]["gateway_tests"], revision)
        result = subprocess.run(
            [executable, SCHEMA_TEST, "--exact", "--nocapture", "--test-threads=1"],
            cwd=ROOT,
            stdin=subprocess.DEVNULL,
            stdout=subprocess.PIPE,
            stderr=subprocess.STDOUT,
            text=True,
            timeout=840,
        )
        print(result.stdout, end="")
        markers = re.findall(
            r"^test result: ok\. (\d+) passed; 0 failed; 0 ignored; 0 measured; \d+ filtered out;",
            result.stdout,
            re.M,
        )
        require(result.returncode == 0, "real router schema contract test failed")
        require(
            markers == ["1"] and f"test {SCHEMA_TEST} ... ok" in result.stdout,
            "exactly one executed schema contract and one actual count required",
        )
        count = int(markers[0])
        require(count > 0, "zero cases cannot qualify")
        schema = json.loads((ROOT / "services/platform/hosted/gateway/openrpc.json").read_text())
        methods = schema["methods"]
        require(isinstance(methods, list), "router method schema is not a list")
        require(
            all(
                isinstance(method, dict) and isinstance(method.get("name"), str)
                for method in methods
            ),
            "router method schema fields differ",
        )
        for name in CHAIN_METHODS:
            require(
                sum(method.get("name") == name for method in methods) == 1,
                "router chain method schema absent or duplicated: " + name,
            )
        require(
            not git("status", "--porcelain", "--untracked-files=no")
            and git("rev-parse", "HEAD") == revision,
            "schema source changed during verification",
        )
        return 0
    except (
        OSError,
        ValueError,
        KeyError,
        RuntimeError,
        subprocess.SubprocessError,
    ) as error:
        print("router-authority-schema: refusal: " + str(error), file=sys.stderr)
        return 1
    finally:
        print(f"PAXEER_X_GATE tests={count} skipped=0")


if __name__ == "__main__":
    sys.exit(main())
