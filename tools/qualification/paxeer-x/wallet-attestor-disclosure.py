#!/usr/bin/env python3
"""Attestor signatures bound to the independently approved activity disclosure.

Runs the real attestor HTTP cluster cases (old and partial shapes, substitution, approval
replay across principal, key, session, network, protocol, digest and expiry, restart and
recovery), the Human KMS signer against the real attestor daemon quorum, and the built
gateway client serializer. Exits nonzero when any prerequisite or acceptance is unmet.
"""
import json
import os
import re
import shutil
import subprocess
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parents[3]
ATTESTOR = ROOT / "human/wallet/attestor"
GATEWAY_CLIENT = ROOT / "human/wallet/gateway/dist/attestor/client.js"
GO_CASES = "^(TestApprovedDisclosureBindsActivitySignatures|TestFiveNodeEndToEnd)$"

U128_MAX = str((1 << 128) - 1)
U64_MAX = str((1 << 64) - 1)

GATEWAY_SCRIPT = r"""
const { signRequestBody, decimalWire } = await import(process.argv[1]);
const results = [];
const check = (name, fn) => { try { fn(); results.push([name, null]); } catch (e) { results.push([name, String(e && e.message || e)]); } };
const refuses = (fn) => { try { fn(); } catch (e) { if (e && e.code === 'session_bad_request') return; throw e; } throw new Error('accepted'); };
const id = (b) => b.repeat(64);
const disclosure = (amount, sequence) => ({ account: id('a'), module: 'asset', operation: 5, amounts: [{ asset: id('b'), amount }], destinations: [id('c')], sequence, not_before: '0', not_after: U64 });
const approval = (session, key, expires) => ({ version: 1, principal: 'user-0001', key_id: key, network_id: 125, protocol_version: 1, session_id: session, activity_digest: id('d'), expires_at: expires });
const payload = (d, a) => ({ kind: 'lx_activity', activity: '0x00', disclosure: d, approval: a });
const U128 = process.argv[2], U64 = process.argv[3];
check('u128 and u64 bounds serialize losslessly', () => {
  const body = JSON.stringify(signRequestBody('s-1', 'k-1', ['node-1'], payload(disclosure(U128, U64), approval('s-1', 'k-1', U64))));
  const parsed = JSON.parse(body);
  if (parsed.disclosure.amounts[0].amount !== U128 || parsed.disclosure.sequence !== U64 || parsed.approval.expires_at !== U64) throw new Error(body);
  if (!body.includes('"amount":"' + U128 + '"')) throw new Error('amount is not a decimal string');
});
check('decimalWire keeps full bigint precision', () => {
  if (decimalWire((1n << 128n) - 1n, 128) !== U128 || decimalWire((1n << 64n) - 1n, 64) !== U64) throw new Error('lossy');
  refuses(() => decimalWire(1n << 128n, 128));
  refuses(() => decimalWire(-1n, 64));
});
check('a JSON number amount is refused', () => refuses(() => signRequestBody('s-1', 'k-1', ['node-1'], payload(disclosure(5000000, '1'), approval('s-1', 'k-1', '1')))));
check('a non-canonical decimal is refused', () => refuses(() => signRequestBody('s-1', 'k-1', ['node-1'], payload(disclosure('05000000', '1'), approval('s-1', 'k-1', '1')))));
check('an out-of-range u64 is refused', () => refuses(() => signRequestBody('s-1', 'k-1', ['node-1'], payload(disclosure('1', '18446744073709551616'), approval('s-1', 'k-1', '1')))));
check('an approval for another session is refused', () => refuses(() => signRequestBody('s-1', 'k-1', ['node-1'], payload(disclosure('1', '1'), approval('s-2', 'k-1', '1')))));
check('an approval for another key is refused', () => refuses(() => signRequestBody('s-1', 'k-1', ['node-1'], payload(disclosure('1', '1'), approval('s-1', 'k-2', '1')))));
check('an unknown approval version is refused', () => refuses(() => signRequestBody('s-1', 'k-1', ['node-1'], payload(disclosure('1', '1'), { ...approval('s-1', 'k-1', '1'), version: 2 }))));
console.log(JSON.stringify(results));
"""


def fail(message):
    print(f"FAIL {message}", flush=True)
    return 1


def run(label, command, cwd):
    print(f"== {label}: {' '.join(command)}", flush=True)
    proc = subprocess.run(command, cwd=cwd, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, text=True)
    print(proc.stdout, flush=True)
    return proc


def main():
    for tool in ("go", "cargo", "node"):
        if shutil.which(tool) is None:
            return fail(f"prerequisite {tool} is not on PATH")
    if not GATEWAY_CLIENT.is_file():
        return fail(f"prerequisite built gateway client {GATEWAY_CLIENT.relative_to(ROOT)} is missing; build the gateway first")
    tests = skipped = 0
    failures = []

    go = run("attestor HTTP cluster", ["go", "test", "./internal/server", "-run", GO_CASES, "-count=1", "-v"], ATTESTOR)
    passed = re.findall(r"^--- PASS: (\S+)", go.stdout, re.M)
    go_skipped = re.findall(r"^--- SKIP: (\S+)", go.stdout, re.M)
    tests += len(passed)
    skipped += len(go_skipped)
    if go.returncode != 0 or sorted(passed) != ["TestApprovedDisclosureBindsActivitySignatures", "TestFiveNodeEndToEnd"]:
        failures.append(f"attestor cluster cases exit {go.returncode}, passed {passed}")

    lx = run("attestor disclosure wire", ["go", "test", "./internal/policy/lx", "-count=1"], ATTESTOR)
    if lx.returncode != 0:
        failures.append(f"attestor policy package exit {lx.returncode}")
    else:
        tests += 1

    kms = run("Human KMS against the real attestor daemon", ["cargo", "test", "--locked", "-p", "layerx-human-kms", "--test", "attestor", "--", "--test-threads=1"], ROOT / "human")
    summary = re.findall(r"test result: (\w+)\. (\d+) passed; (\d+) failed; (\d+) ignored", kms.stdout)
    if kms.returncode != 0 or not summary or summary[-1][0] != "ok" or int(summary[-1][1]) == 0:
        failures.append(f"Human KMS attestor cases exit {kms.returncode}, summary {summary}")
    else:
        tests += int(summary[-1][1])
        skipped += int(summary[-1][3])

    gw = subprocess.run(["node", "--input-type=module", "-e", GATEWAY_SCRIPT, "--", GATEWAY_CLIENT.as_uri(), U128_MAX, U64_MAX],
                        cwd=ROOT, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, text=True)
    print("== built gateway client serializer", flush=True)
    print(gw.stdout, flush=True)
    try:
        cases = json.loads(gw.stdout.strip().splitlines()[-1])
    except (ValueError, IndexError):
        cases = None
    if gw.returncode != 0 or not cases:
        failures.append(f"gateway client cases exit {gw.returncode}")
    else:
        for name, error in cases:
            tests += 1
            if error is not None:
                failures.append(f"gateway client: {name}: {error}")

    for f in failures:
        print(f"FAIL {f}", flush=True)
    print(f"PAXEER_X_GATE tests={tests} skipped={skipped}", flush=True)
    return 1 if failures or skipped else 0


if __name__ == "__main__":
    sys.exit(main())
