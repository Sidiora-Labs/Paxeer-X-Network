#!/usr/bin/env bash
set -euo pipefail
exec python3 - <<'PY'
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess
import sys
from urllib.parse import urlsplit
from urllib.request import Request, build_opener, HTTPRedirectHandler

sys.dont_write_bytecode = True
root = Path.cwd().resolve()
sys.path.insert(0, str(root / "tools/paxeer-x"))
from candidate import duplicate_free, load_private, source_identity

expected = {
    "custodyproof": "custodyproof.test",
    "modules/launchpad/keeper": "launchpad-keeper.test",
    "modules/launchpad/types": "launchpad-types.test",
}
params = {
    "TestMsgUpdateParamsGovernanceRoute",
    "TestMsgUpdateParamsCodecAndSigners",
    "TestMsgUpdateParamsRefusals",
}
required = {
    "custodyproof": {
        "TestRealStateCreditAndClockBounds",
        "TestRealStateCreditRefusesMissingAuthority",
        "TestJSONRequestRefusesAmbiguousFields",
        "TestLightVectorTrustRules",
        "TestLightVectorTamper",
        "TestLightVectorTimeRules",
        "TestLightNilVoteSignature",
    },
    "modules/launchpad/keeper": params,
}


class NoRedirect(HTTPRedirectHandler):
    def redirect_request(self, request, fp, code, message, headers, url):
        raise ValueError("committed RPC redirect refused")


def rpc(endpoint, method, params):
    payload = json.dumps(
        {"jsonrpc": "2.0", "id": 1, "method": method, "params": params}
    ).encode()
    request = Request(
        endpoint, data=payload, headers={"Content-Type": "application/json"}
    )
    with build_opener(NoRedirect()).open(request, timeout=30) as response:
        data = response.read(4 * 1024 * 1024 + 1)
        if response.status != 200 or len(data) > 4 * 1024 * 1024:
            raise ValueError("committed RPC response bound")
    result = json.loads(data, object_pairs_hook=duplicate_free)
    if (
        not isinstance(result, dict)
        or set(result) != {"jsonrpc", "id", "result"}
        or result["jsonrpc"] != "2.0"
        or type(result["id"]) is not int
        or result["id"] != 1
    ):
        raise ValueError("committed RPC response identity")
    return result["result"]


def quantity(value):
    if (
        not isinstance(value, str)
        or re.fullmatch(r"0x(?:0|[1-9a-f][0-9a-f]*)", value) is None
    ):
        raise ValueError("noncanonical committed quantity")
    return int(value, 16)


def finalized_block():
    endpoint = os.environ["PAXEER_X_COMMITTED_RPC"]
    url = urlsplit(endpoint)
    if (
        url.scheme not in {"https", "http"}
        or not url.hostname
        or url.username is not None
        or url.password is not None
        or url.fragment
    ):
        raise ValueError("explicit committed RPC endpoint required")
    block = rpc(endpoint, "eth_getBlockByNumber", ["finalized", False])
    if not isinstance(block, dict) or quantity(block.get("number")) <= 0:
        raise ValueError("finalized committed block absent")
    for key in ("hash", "stateRoot"):
        value = block.get(key)
        if (
            not isinstance(value, str)
            or re.fullmatch(r"0x[0-9a-fA-F]{64}", value) is None
            or int(value, 16) == 0
        ):
            raise ValueError("finalized committed block identity absent")
    if rpc(endpoint, "eth_getBlockByNumber", [block["number"], False]) != block:
        raise ValueError("finalized tag differs from its committed height")
    if quantity(rpc(endpoint, "eth_blockNumber", [])) < quantity(block["number"]):
        raise ValueError("finalized height exceeds committed head")


try:
    manifest_path = Path(os.environ["PAXEER_X_106_4_9_BUNDLE"]).resolve()
    if root == manifest_path.parent or root in manifest_path.parents:
        raise ValueError("bundle must be private and outside source")
    bundle = load_private(manifest_path)
    identity = source_identity(
        root, os.environ.get("PAXEER_X_MAINLINE", "refs/heads/main")
    )
    if (
        set(bundle) != {"source", "targets"}
        or bundle["source"] != identity
        or identity["dirty"]
    ):
        raise ValueError("bundle source identity mismatch")
    targets = bundle["targets"]
    if set(targets) != set(expected):
        raise ValueError("bundle package corpus mismatch")
    paths = {}
    for package, name in expected.items():
        row = targets[package]
        if set(row) != {"binary", "sha256"} or row["binary"] != name:
            raise ValueError("bundle target mismatch")
        binary = manifest_path.parent / name
        if (
            binary.is_symlink()
            or not binary.is_file()
            or not os.access(binary, os.X_OK)
        ):
            raise ValueError("prebuilt test binary absent")
        if hashlib.sha256(binary.read_bytes()).hexdigest() != row["sha256"]:
            raise ValueError("prebuilt test binary digest mismatch")
        paths[package] = binary
    tests = skipped = 0
    for package, binary in paths.items():
        print("package: " + package, flush=True)
        result = subprocess.run(
            [str(binary), "-test.v", "-test.count=1", "-test.timeout=10m"],
            cwd=root / package,
            stdin=subprocess.DEVNULL,
            stdout=subprocess.PIPE,
            stderr=subprocess.STDOUT,
        )
        output = result.stdout.decode("utf-8", errors="replace")
        print(output, end="", flush=True)
        passed = set(re.findall(r"^--- PASS: (\S+)", output, re.M))
        skipped += len(re.findall(r"^\s*--- SKIP:", output, re.M))
        tests += len(re.findall(r"^\s*--- PASS:", output, re.M))
        if (
            result.returncode
            or not passed
            or not required.get(package, set()) <= passed
        ):
            raise ValueError(
                "prebuilt test failure or missing required case: " + package
            )
    if skipped:
        raise ValueError("required corpus contains skipped tests")
    finalized_block()
    print(f"PAXEER_X_GATE tests={tests + 1} skipped=0")
except (OSError, ValueError, KeyError, TypeError) as error:
    print("106.4.9 refused: " + type(error).__name__, file=sys.stderr)
    sys.exit(1)
PY
