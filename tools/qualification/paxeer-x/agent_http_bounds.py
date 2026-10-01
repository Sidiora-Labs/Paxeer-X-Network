#!/usr/bin/env python3
"""Process-boundary qualification of the agentd program HTTP bounds.

Launches the built layerx-agentd in full mode against provisioned qualification authority and
disposable state, then drives it with real concurrent sockets: silent clients, clients that
trickle partial headers, admission exhaustion, oversized and body-bearing requests, and
legitimate authenticated requests. Every case asserts the daemon's declared latency and
resource bounds. Shutdown with requests in flight and a restart over the same durable store
close the run.

Inputs (all required; the harness fails rather than skipping when any is absent):
  PAXEER_X_CANDIDATE_MANIFEST         candidate manifest (schema paxeer-x.candidate.v1) for HEAD
  PAXEER_X_EVIDENCE_DIR               owner-only evidence directory outside the source tree
  CARGO_TARGET_DIR                    target directory holding debug/layerx-agentd
plus the artifact manifests fixtures/agentd_fixture.py reads to own the daemon's real upstream.
The fixture generates the daemon credentials; they are passed to the daemon only, never printed.
"""

import importlib.util
import json
import os
import signal
import socket
import stat
import subprocess
import sys
import threading
import time
from pathlib import Path

sys.dont_write_bytecode = True
_FIXTURE_SPEC = importlib.util.spec_from_file_location(
    "agentd_fixture", Path(__file__).resolve().parent / "fixtures" / "agentd_fixture.py")
_FIXTURE = importlib.util.module_from_spec(_FIXTURE_SPEC)
_FIXTURE_SPEC.loader.exec_module(_FIXTURE)
AgentdFixture, FixtureRefused = _FIXTURE.AgentdFixture, _FIXTURE.FixtureRefused

ROOT = Path(__file__).resolve().parents[3]
COMMAND = "timeout 30m python3 tools/qualification/paxeer-x/agent_http_bounds.py"

# Bounds the daemon declares in agent/crates/layerx-agentd/src/main.rs.
WORKERS = 8
QUEUE = 16
DEADLINE = 5.0
HEADER_LIMIT = 16 * 1024
SLACK = 1.5
READY_TIMEOUT = 180.0


class Failure(Exception):
    pass


def fail(message):
    raise Failure(message)


def require_env(name):
    value = os.environ.get(name, "")
    if not value:
        fail(f"{name} is required (missing authority/configuration)")
    return value


def owner_only(path, kind):
    mode = path.stat().st_mode
    if mode & (stat.S_IRWXG | stat.S_IRWXO):
        fail(f"{kind} must be owner-only")


def load_manifest(head):
    path = Path(require_env("PAXEER_X_CANDIDATE_MANIFEST"))
    if not path.is_file():
        fail("candidate manifest is missing")
    document = json.loads(path.read_text())
    if document.get("schema") != "paxeer-x.candidate.v1":
        fail("candidate manifest schema is not paxeer-x.candidate.v1")
    source = document.get("source", {})
    if source.get("revision") != head or source.get("dirty") is not False:
        fail("candidate manifest does not bind the clean candidate revision")
    return path


def evidence_dir():
    path = Path(require_env("PAXEER_X_EVIDENCE_DIR")).resolve()
    if path == ROOT or ROOT in path.parents:
        fail("PAXEER_X_EVIDENCE_DIR must be outside the source tree")
    path.mkdir(mode=0o700, parents=True, exist_ok=True)
    owner_only(path, "PAXEER_X_EVIDENCE_DIR")
    return path


def daemon_config(fixture):
    try:
        values = fixture.start()
    except FixtureRefused as error:
        fail(str(error))
    for key in (
        "LAYERX_AGENT_PROGRAM_BEARER_TOKEN",
        "LAYERX_AGENT_PROGRAM_PROBE_ID",
        "LAYERX_AGENT_HUMAN_SOCKET",
        "LAYERX_AGENT_HUMAN_STORE",
    ):
        if not values.get(key):
            fail(f"qualification configuration omits {key}")
    if values.get("LAYERX_AGENT_MODE", "full") != "full":
        fail("qualification configuration must select the full agent mode")
    return values


def daemon_binary():
    target = Path(require_env("CARGO_TARGET_DIR"))
    binary = target / "debug" / "layerx-agentd"
    if not os.access(binary, os.X_OK):
        fail("the built layerx-agentd candidate artifact is missing")
    return binary


def revision():
    head = subprocess.run(
        ["git", "-C", str(ROOT), "rev-parse", "HEAD"], capture_output=True, text=True, check=True
    ).stdout.strip()
    dirty = subprocess.run(
        ["git", "-C", str(ROOT), "status", "--porcelain"], capture_output=True, text=True, check=True
    ).stdout.strip()
    return head, bool(dirty)


def free_port():
    with socket.socket() as probe:
        probe.bind(("127.0.0.1", 0))
        return probe.getsockname()[1]


class Daemon:
    def __init__(self, binary, config, evidence, port):
        self.binary = binary
        self.config = config
        self.evidence = evidence
        self.port = port
        self.process = None
        self.starts = 0

    def start(self):
        self.starts += 1
        environment = {
            key: value
            for key, value in os.environ.items()
            if not key.startswith(("LAYERX_", "PAXEER_X_"))
        }
        environment.update(self.config)
        environment["LAYERX_AGENT_PROGRAM_LISTEN"] = f"127.0.0.1:{self.port}"
        log = open(self.evidence / f"agentd-{self.starts}.log", "wb")
        self.process = subprocess.Popen(
            [str(self.binary)], env=environment, stdout=log, stderr=subprocess.STDOUT
        )
        log.close()
        deadline = time.monotonic() + READY_TIMEOUT
        while time.monotonic() < deadline:
            if self.process.poll() is not None:
                fail(f"layerx-agentd exited with {self.process.returncode} before serving")
            try:
                status, body, _ = request(self.port, health(self.config), timeout=DEADLINE + SLACK)
                if status == 200 and '"ready":true' in body:
                    return
            except OSError:
                pass
            time.sleep(0.25)
        fail("layerx-agentd did not report ready within the readiness bound")

    def alive(self):
        return self.process is not None and self.process.poll() is None

    def status(self, field):
        for line in Path(f"/proc/{self.process.pid}/status").read_text().splitlines():
            if line.startswith(field + ":"):
                return int(line.split()[1])
        fail(f"/proc status omits {field}")

    def stop(self, timeout=10.0):
        if not self.alive():
            return None
        self.process.send_signal(signal.SIGTERM)
        try:
            return self.process.wait(timeout=timeout)
        except subprocess.TimeoutExpired:
            self.process.kill()
            self.process.wait()
            fail("layerx-agentd did not stop within its shutdown bound")


def health(config, bearer=True):
    lines = ["GET /healthz HTTP/1.1", "Host: 127.0.0.1"]
    if bearer:
        lines.append("Authorization: Bearer " + config["LAYERX_AGENT_PROGRAM_BEARER_TOKEN"])
    return ("\r\n".join(lines) + "\r\n\r\n").encode()


def get(config, path, extra=()):
    lines = [
        f"GET {path} HTTP/1.1",
        "Host: 127.0.0.1",
        "Authorization: Bearer " + config["LAYERX_AGENT_PROGRAM_BEARER_TOKEN"],
        *extra,
    ]
    return ("\r\n".join(lines) + "\r\n\r\n").encode()


def read_response(connection):
    chunks = []
    while True:
        chunk = connection.recv(65536)
        if not chunk:
            break
        chunks.append(chunk)
    data = b"".join(chunks)
    if not data:
        return None, "", data
    head, _, body = data.partition(b"\r\n\r\n")
    parts = head.split(b" ", 2)
    if len(parts) < 2 or not parts[0].startswith(b"HTTP/1.1"):
        fail("daemon answered with a malformed status line")
    return int(parts[1]), body.decode(errors="replace"), data


def request(port, payload, timeout):
    with socket.create_connection(("127.0.0.1", port), timeout=timeout) as connection:
        connection.settimeout(timeout)
        started = time.monotonic()
        connection.sendall(payload)
        status, body, _ = read_response(connection)
        return status, body, time.monotonic() - started


def silent(port):
    connection = socket.create_connection(("127.0.0.1", port), timeout=5)
    connection.settimeout(DEADLINE * 4 + SLACK)
    return connection


def await_refusal(connection):
    """Waits for the daemon to refuse or close a connection; returns (status, elapsed)."""
    started = time.monotonic()
    try:
        status, _, _ = read_response(connection)
    except ConnectionResetError:
        status = None
    except socket.timeout:
        fail("daemon held a connection past every bound without answering")
    return status, time.monotonic() - started


def legit_health(daemon, bound):
    status, body, elapsed = request(daemon.port, health(daemon.config), timeout=bound + SLACK)
    if status != 200 or '"ready":true' not in body:
        fail(f"authenticated health answered {status}")
    if elapsed > bound:
        fail(f"authenticated health took {elapsed:.2f}s, beyond {bound:.2f}s")
    return elapsed


def close_all(connections):
    for connection in connections:
        try:
            connection.close()
        except OSError:
            pass


def case_authenticated_health(daemon):
    return {"elapsed_s": round(legit_health(daemon, 2.0), 3)}


def case_unauthenticated_refused(daemon):
    status, body, _ = request(daemon.port, health(daemon.config, bearer=False), timeout=5)
    if status != 401 or "unauthorized" not in body:
        fail(f"health without the bearer answered {status}")
    return {"status": status}


def case_authenticated_read(daemon):
    probe = daemon.config["LAYERX_AGENT_PROGRAM_PROBE_ID"]
    status, body, elapsed = request(
        daemon.port, get(daemon.config, f"/v1/programs/{probe}/balances"), timeout=10
    )
    if status != 200 or '"program"' not in body or '"freshness"' not in body:
        fail(f"authenticated balance read answered {status}")
    return {"status": status, "elapsed_s": round(elapsed, 3)}


def case_silent_client_isolated(daemon):
    held = silent(daemon.port)
    try:
        time.sleep(0.2)
        elapsed = legit_health(daemon, 2.0)
        status, waited = await_refusal(held)
        if status not in (408, None) or waited > DEADLINE + SLACK:
            fail(f"silent client was answered {status} after {waited:.2f}s")
        return {"health_elapsed_s": round(elapsed, 3), "silent_status": status,
                "silent_closed_after_s": round(waited, 3)}
    finally:
        close_all([held])


def case_trickle_bounded(daemon):
    payload = b"GET /healthz HTTP/1.1\r\nX-Trickle: " + b"a" * 4096
    connection = silent(daemon.port)
    outcome = {}

    def trickle():
        sent = 0
        try:
            for byte in payload:
                connection.sendall(bytes([byte]))
                sent += 1
                time.sleep(0.25)
        except OSError:
            pass
        outcome["sent"] = sent

    started = time.monotonic()
    writer = threading.Thread(target=trickle, daemon=True)
    writer.start()
    try:
        time.sleep(0.5)
        health_elapsed = legit_health(daemon, 2.0)
        status, _ = await_refusal(connection)
        elapsed = time.monotonic() - started
    finally:
        close_all([connection])
        writer.join(timeout=2)
    if status not in (408, None):
        fail(f"trickling client was answered {status}")
    if elapsed > DEADLINE + SLACK:
        fail(f"trickling client held its worker for {elapsed:.2f}s, beyond the absolute deadline")
    if elapsed < DEADLINE - SLACK:
        fail(f"trickling client was cut after {elapsed:.2f}s, before the declared deadline")
    return {"status": status, "elapsed_s": round(elapsed, 3), "bytes_trickled": outcome.get("sent"),
            "health_elapsed_s": round(health_elapsed, 3)}


def case_admission_exhaustion(daemon):
    baseline_threads = daemon.status("Threads")
    held = []
    try:
        for _ in range(WORKERS + QUEUE):
            held.append(silent(daemon.port))
        time.sleep(0.5)
        refusals = []
        for _ in range(4):
            extra = silent(daemon.port)
            extra.settimeout(SLACK)
            try:
                status, waited = await_refusal(extra)
            finally:
                close_all([extra])
            if status not in (503, None) or waited > SLACK:
                fail(f"excess connection answered {status} after {waited:.2f}s")
            refusals.append(status)
        threads = daemon.status("Threads")
        if threads > baseline_threads:
            fail(f"admission exhaustion grew the daemon from {baseline_threads} to {threads} threads")
        if not daemon.alive():
            fail("daemon stopped under admission exhaustion")
    finally:
        close_all(held)
    started = time.monotonic()
    while True:
        try:
            status, body, _ = request(daemon.port, health(daemon.config), timeout=DEADLINE + SLACK)
        except ConnectionError:
            status, body = None, ""
        if status == 200 and '"ready":true' in body:
            break
        if status not in (503, None) or time.monotonic() - started > DEADLINE + SLACK:
            fail(f"authenticated health answered {status} after admission was released")
        time.sleep(0.05)
    recovery = time.monotonic() - started
    return {"refusals": refusals, "threads": threads, "recovery_s": round(recovery, 3)}


def case_flood_resource_bound(daemon):
    time.sleep(DEADLINE + SLACK)
    threads = daemon.status("Threads")
    rss = daemon.status("VmRSS")
    for _ in range(300):
        try:
            connection = socket.create_connection(("127.0.0.1", daemon.port), timeout=2)
            connection.close()
        except OSError:
            pass
    peak_threads = daemon.status("Threads")
    peak_rss = daemon.status("VmRSS")
    if peak_threads > threads:
        fail(f"connection flood grew the daemon from {threads} to {peak_threads} threads")
    if peak_rss - rss > 16 * 1024:
        fail(f"connection flood grew resident memory by {peak_rss - rss} KiB")
    recovery = legit_health(daemon, 2 * DEADLINE + 2 * SLACK)
    return {"threads": peak_threads, "rss_growth_kib": peak_rss - rss,
            "recovery_s": round(recovery, 3)}


def case_oversized_headers(daemon):
    payload = b"GET /healthz HTTP/1.1\r\nX-Fill: " + b"x" * HEADER_LIMIT
    status, body, _ = request(daemon.port, payload, timeout=DEADLINE + SLACK)
    if status != 431 or "headers_too_large" not in body:
        fail(f"oversized headers answered {status}")
    return {"status": status}


def case_declared_body_refused(daemon):
    status, body, _ = request(
        daemon.port, get(daemon.config, "/healthz", ["Content-Length: 1"]), timeout=5
    )
    if status != 413 or "body_too_large" not in body:
        fail(f"declared request body answered {status}")
    chunked, _, _ = request(
        daemon.port, get(daemon.config, "/healthz", ["Transfer-Encoding: chunked"]), timeout=5
    )
    if chunked != 400:
        fail(f"chunked request answered {chunked}")
    return {"content_length_status": status, "chunked_status": chunked}


def case_human_supervision_independent(daemon):
    held = [silent(daemon.port) for _ in range(WORKERS)]
    try:
        time.sleep(0.3)
        socket_path = daemon.config["LAYERX_AGENT_HUMAN_SOCKET"]
        started = time.monotonic()
        with socket.socket(socket.AF_UNIX, socket.SOCK_STREAM) as owner:
            owner.settimeout(1.0)
            owner.connect(socket_path)
        connected = time.monotonic() - started
        if connected > 1.0:
            fail(f"human owner socket took {connected:.2f}s while program workers were saturated")
        if not daemon.alive():
            fail("daemon stopped while program workers were saturated")
        return {"owner_connect_s": round(connected, 3)}
    finally:
        close_all(held)


def store_snapshot(root):
    root = Path(root)
    if root.is_file():
        return {root.name: root.stat().st_size}
    return {
        str(path.relative_to(root)): path.stat().st_size
        for path in root.rglob("*")
        if path.is_file()
    }


def case_shutdown_restart(daemon):
    store = daemon.config["LAYERX_AGENT_HUMAN_STORE"]
    before = store_snapshot(store)
    if not before:
        fail("the qualification store holds no durable state")
    held = [silent(daemon.port) for _ in range(WORKERS + 2)]
    try:
        time.sleep(0.3)
        started = time.monotonic()
        code = daemon.stop(timeout=DEADLINE)
        stopped = time.monotonic() - started
        for connection in held:
            status, _ = await_refusal(connection)
            if status is not None:
                fail(f"in-flight connection received {status} from a stopped daemon")
    finally:
        close_all(held)
    after_stop = store_snapshot(store)
    lost = sorted(set(before) - set(after_stop))
    if lost:
        fail(f"shutdown removed {len(lost)} durable store files")
    started = time.monotonic()
    daemon.start()
    restart = time.monotonic() - started
    after = store_snapshot(store)
    if set(before) - set(after):
        fail("restart lost durable store files")
    legit_health(daemon, 2.0)
    return {"exit_code": code, "stop_s": round(stopped, 3), "restart_ready_s": round(restart, 3),
            "store_files": len(after)}


CASES = [
    ("authenticated_health", case_authenticated_health),
    ("unauthenticated_refused", case_unauthenticated_refused),
    ("authenticated_read", case_authenticated_read),
    ("silent_client_isolated", case_silent_client_isolated),
    ("trickle_bounded", case_trickle_bounded),
    ("admission_exhaustion", case_admission_exhaustion),
    ("flood_resource_bound", case_flood_resource_bound),
    ("oversized_headers", case_oversized_headers),
    ("declared_body_refused", case_declared_body_refused),
    ("human_supervision_independent", case_human_supervision_independent),
    ("shutdown_restart", case_shutdown_restart),
]


def main():
    record = {"command": COMMAND, "cases": [], "bounds": {
        "workers": WORKERS, "queue": QUEUE, "deadline_s": DEADLINE, "header_limit": HEADER_LIMIT}}
    evidence = None
    daemon = None
    fixture = None
    head = None
    try:
        evidence = evidence_dir()
        head, dirty = revision()
        record.update({"revision": head, "dirty": dirty})
        if dirty:
            fail("the candidate source tree is dirty")
        manifest = load_manifest(head)
        binary = daemon_binary()
        fixture = AgentdFixture(evidence / f"fixture-{os.getpid()}")
        record.update({"manifest": str(manifest), "binary": str(binary),
                       "fixture": str(fixture.directory), "evidence": str(evidence)})
        config = daemon_config(fixture)
        daemon = Daemon(binary, config, evidence, free_port())
        daemon.start()
        for name, case in CASES:
            started = time.monotonic()
            try:
                detail = case(daemon)
                result = "PASS"
            except Failure as error:
                detail = {"error": str(error)}
                result = "FAIL"
            if result == "PASS" and not daemon.alive():
                detail = {"error": "daemon stopped during the case"}
                result = "FAIL"
            entry = {"case": name, "result": result,
                     "elapsed_s": round(time.monotonic() - started, 3), **detail}
            record["cases"].append(entry)
            print(f"CASE {name} {result} {json.dumps(detail, sort_keys=True)}", flush=True)
            if result == "FAIL" and not daemon.alive():
                break
    except Failure as error:
        record["error"] = str(error)
        print(f"agent_http_bounds: {error}", file=sys.stderr, flush=True)
    finally:
        if daemon is not None:
            try:
                daemon.stop()
            except Failure as error:
                record.setdefault("error", str(error))
        if fixture is not None:
            fixture.cleanup()
    passed = sum(1 for case in record["cases"] if case["result"] == "PASS")
    executed = len(record["cases"])
    complete = executed == len(CASES) and passed == executed and "error" not in record
    record["result"] = "pass" if complete else "fail"
    record["exit_code"] = 0 if complete else 1
    if evidence is not None:
        target = evidence / "agent_http_bounds.json"
        target.write_text(json.dumps(record, indent=2, sort_keys=True) + "\n")
        target.chmod(0o600)
        print(f"agent_http_bounds: evidence {target}", flush=True)
    print(f"agent_http_bounds: revision={head} command={COMMAND!r} exit_code={record['exit_code']}",
          flush=True)
    print(f"agent_http_bounds: cases={executed} passed={passed} failed={executed - passed} "
          f"declared={len(CASES)} skipped=0", flush=True)
    print(f"PAXEER_X_GATE tests={executed} skipped=0", flush=True)
    return record["exit_code"]


if __name__ == "__main__":
    sys.exit(main())
