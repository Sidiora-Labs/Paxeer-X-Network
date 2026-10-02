#!/usr/bin/env python3
import sys

sys.dont_write_bytecode = True

import argparse
import hashlib
import http.client
import http.server
import json
import os
from pathlib import Path
import shutil
import socket
import ssl
import subprocess
import tempfile
import threading
import time
import urllib.error
import urllib.request

ROOT = Path(__file__).resolve().parents[3]
CASES = ("archive-origin-freshness", "ramp-journal-readiness", "mirror-checkpoint-acquisition")
BUDGET_SECONDS = 3.0
POLL_SECONDS = 0.2


class Failure(Exception):
    pass


def require(condition, message):
    if not condition:
        raise Failure(message)


def now_ms():
    return int(time.time() * 1000)


def unused_port():
    with socket.socket() as connection:
        connection.bind(("127.0.0.1", 0))
        return connection.getsockname()[1]


def write_json(path, value):
    path.write_text(json.dumps(value, sort_keys=True) + "\n")
    path.chmod(0o644)


def env_file(path):
    return dict(line.split("=", 1) for line in path.read_text().splitlines() if line)


def load_candidate(path):
    sys.path.insert(0, str(ROOT / "tools/paxeer-x"))
    from candidate import Invalid, catalogue, load_private, validate
    try:
        data = load_private(path)
        validate(data, catalogue(ROOT / "spec/paxeer-x/spec.kvx"))
    except (Invalid, OSError, ValueError, KeyError, TypeError) as error:
        raise Failure(f"candidate manifest is absent or invalid: {error}") from error
    services = {service["id"]: service for service in data["services"]}
    require("archive" in services, "candidate manifest does not declare the archive service")
    archive = services["archive"]
    return {
        "sha256": hashlib.sha256(json.dumps(data, sort_keys=True, separators=(",", ":")).encode()).hexdigest(),
        "source_revision": data["source"]["revision"],
        "archive_action": archive["action"],
        "archive_bindings": archive["bindings"],
        "archive_observations": len(archive["observations"]),
    }


class HostileOrigin:
    def __init__(self, upstream, context):
        self.upstream = upstream
        self.context = context
        self.mode = "offline"
        self.fork_head = None
        outer = self

        class Handler(http.server.BaseHTTPRequestHandler):
            protocol_version = "HTTP/1.1"

            def log_message(self, format, *args):
                del format, args

            def do_GET(self):
                mode = outer.mode
                if mode == "offline":
                    self.close_connection = True
                    return
                path = self.path
                batch_prefix = "/v1/sync/batches/"
                if mode == "discontinuity" and path.startswith(batch_prefix):
                    path = batch_prefix + str(int(path[len(batch_prefix):]) - 1)
                status, headers, body = outer.fetch(path)
                if status == 200 and path in ("/v1/sync/network", "/v1/sync/head"):
                    body = json.dumps(outer.mutate(mode, path, json.loads(body))).encode()
                if status == 200 and mode == "signature" and path.startswith(batch_prefix):
                    body = body[:-1] + bytes([body[-1] ^ 1])
                    headers["X-Content-SHA256"] = hashlib.sha256(body).hexdigest()
                self.send_response(status)
                for name, value in headers.items():
                    self.send_header(name, value)
                self.send_header("Content-Length", str(len(body)))
                self.send_header("Connection", "close")
                self.end_headers()
                self.wfile.write(body)
                self.close_connection = True

        self.server = http.server.ThreadingHTTPServer(("127.0.0.1", unused_port()), Handler)
        self.server.daemon_threads = True
        self.url = f"http://127.0.0.1:{self.server.server_address[1]}"
        self.thread = threading.Thread(target=self.server.serve_forever, daemon=True)
        self.thread.start()

    def fetch(self, path):
        try:
            response = urllib.request.urlopen(self.upstream + path, context=self.context, timeout=8)
        except urllib.error.HTTPError as error:
            response = error
        with response:
            headers = {}
            for name in ("Content-Type", "X-Content-SHA256"):
                value = response.headers.get(name)
                if value is not None:
                    headers[name] = value
            return response.status, headers, response.read()

    def mutate(self, mode, path, document):
        if path == "/v1/sync/network":
            if mode == "network":
                document["network_id"] = document["network_id"] + 1
            elif mode == "genesis":
                document["genesis_sha256"] = "01" * 32
            elif mode == "sequencer":
                document["sequencer_id"] = "02" * 32
        elif mode == "malformed_head" and document.get("head_batch") is not None:
            document["head_batch"] = "0" + document["head_batch"]
        elif mode == "fork":
            document["head_batch"] = self.fork_head
            document["head_batch_id"] = "ee" * 32
            document["head_raw_sha256"] = "dd" * 32
        return document

    def close(self):
        self.server.shutdown()
        self.server.server_close()


class Contract:
    def __init__(self, build, candidate):
        self.build = build
        self.candidate = candidate
        self.work = Path(tempfile.mkdtemp(prefix="layerx-archive-freshness-"))
        self.work.chmod(0o755)
        self.children = []
        self.logs = []
        self.cases = []
        self.refusals = []
        self.context = None
        self.proxy = None
        self.fixture = None

    def passed(self, name, assertions):
        self.cases.append({"case": name, "assertions": assertions})
        print("PASS " + name, flush=True)

    def execute(self, args, *, data=None, env=None, uid=None, success=True):
        identity = {} if uid is None else {"user": uid, "group": uid, "extra_groups": []}
        result = subprocess.run([str(arg) for arg in args], input=data, env=env,
                                stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                                timeout=90, cwd=ROOT, **identity)
        if success:
            require(result.returncode == 0, f"{Path(str(args[0])).name} failed ({result.returncode}): "
                    + result.stderr.decode(errors="replace")[-4000:])
        else:
            require(result.returncode != 0, f"{args} unexpectedly succeeded")
        return result

    def start(self, name, args, *, env=None, pass_fds=()):
        output = (self.work / (name + ".log")).open("ab")
        process = subprocess.Popen([str(arg) for arg in args], stdout=output, stderr=subprocess.STDOUT,
                                   env=env, cwd=ROOT, pass_fds=pass_fds)
        self.children.append(process)
        self.logs.append(output)
        return process

    def stop(self, process):
        if process.poll() is None:
            process.terminate()
            try:
                process.wait(timeout=15)
            except subprocess.TimeoutExpired:
                process.kill()
                process.wait(timeout=15)

    def close(self):
        if self.proxy is not None:
            self.proxy.close()
        for process in reversed(self.children):
            self.stop(process)
        if self.fixture is not None:
            self.fixture.cleanup()
        for output in self.logs:
            output.close()

    def request(self, url, path, *, status=None, raw=False):
        try:
            response = urllib.request.urlopen(url + path, context=self.context if url.startswith("https") else None,
                                              timeout=8)
        except urllib.error.HTTPError as error:
            response = error
        with response:
            body = response.read()
            if status is not None:
                require(response.status == status, f"{url}{path}: expected HTTP {status}, got {response.status}")
            return (response.status, body) if raw else (response.status, json.loads(body))

    def ready(self, url):
        return self.request(url, "/readyz")

    def until(self, condition, label, seconds=60):
        deadline = time.monotonic() + seconds
        last = None
        while time.monotonic() < deadline:
            try:
                value = condition()
                if value:
                    return value
                last = f"condition returned {value!r}"
            except (OSError, Failure, urllib.error.URLError, ValueError, KeyError, TypeError) as error:
                last = error
            time.sleep(0.05)
        raise Failure(f"timed out: {label}; last: {last}")

    def runtime_env(self):
        return dict(os.environ, LAYERX_RELAY_ARCHIVE_RUNTIME=str(self.runtime / "runtime.py"),
                    PYTHONDONTWRITEBYTECODE="1")

    def setup(self):
        require(os.geteuid() == 0, "native LNI UID-isolation setup requires root")
        for name in ("net", "pid", "mnt"):
            parent = os.environ.get("PAXEER_X_PARENT_" + name.upper())
            require(parent is not None and os.readlink("/proc/self/ns/" + name) != parent,
                    f"{name} namespace is not isolated from the invoking process")
        os.setgroups([])
        os.setgid(4020)
        require(os.geteuid() == 0 and os.getegid() == 4020, "real LNI admission requires UID0/GID4020")
        sys.path.insert(0, str(ROOT / "tests/daemon"))
        import paxeer_x_runtime_fixture as fixture
        try:
            bundle = fixture.artifacts(os.environ.get("PAXEER_X_RUNTIME_ARTIFACTS", ""))
            client = fixture.client_artifact(os.environ.get("PAXEER_X_RUNTIME_CLIENT_MANIFEST", ""))
        except (RuntimeError, OSError, ValueError, KeyError, subprocess.SubprocessError) as error:
            raise Failure(f"PAXEER_X_RUNTIME_ARTIFACTS/PAXEER_X_RUNTIME_CLIENT_MANIFEST refused: {error}") from error
        self.bin = self.work / "bin"
        self.bin.mkdir()
        for name in ("layerxd", "layerx-genesis-build", "layerx-archive-codec"):
            shutil.copy2(self.build / "bin" / name, self.bin / name)
        self.runtime = self.work / "runtime"
        shutil.copytree(ROOT / "platform/relay_archive", self.runtime,
                        ignore=shutil.ignore_patterns("__pycache__"))
        self.execute(["ip", "link", "set", "lo", "up"])
        source = self.work / "source"
        python_root = self.work / "python"
        for mount, target in ((ROOT, source), (Path(sys.prefix), python_root)):
            target.mkdir(mode=0o755)
            self.execute(["mount", "--bind", mount, target])
            self.execute(["mount", "-o", "remount,bind,ro,nosuid,nodev", target])
        fixture.ROOT = source
        os.environ["PATH"] = str(python_root / "bin") + ":/usr/local/bin:/usr/bin:/bin"
        try:
            self.fixture = fixture.RuntimeFixture(self.work / "fixture", bundle, client)
            self.fixture.generate()
        except (RuntimeError, OSError, ValueError, KeyError, subprocess.SubprocessError) as error:
            raise Failure(f"runtime fixture bring-up failed: {error}") from error
        self.data = self.fixture.directory / "node"
        self.node = env_file(self.data / "node.env")
        self.submit("0")
        self.source_log = self.data / "checkpoints/da-bodies.log"
        self.until(lambda: self.source_log.exists() and self.source_log.stat().st_size > 0,
                   "native canonical availability body")
        self.manifest = self.data / "genesis/genesis.manifest"
        self.snapshot = self.data / "genesis/00000000000000000000.lxs"
        self.execute(["openssl", "req", "-x509", "-newkey", "rsa:2048", "-nodes",
                      "-keyout", self.work / "tls.key", "-out", self.work / "tls.crt",
                      "-days", "1", "-subj", "/CN=localhost",
                      "-addext", "subjectAltName=IP:127.0.0.1,DNS:localhost"])
        self.context = ssl.create_default_context(cafile=str(self.work / "tls.crt"))
        self.pins = {
            "network_id": 77,
            "genesis_sha256": hashlib.sha256(self.manifest.read_bytes()).hexdigest(),
            "sequencer_id": self.node["LAYERX_NODE_SEQUENCER_ID"],
            "sequencer_public_key": self.node["LAYERX_NODE_SEQUENCER_PUBLIC_KEY"],
            "sequencer_first_batch": 1,
            "allow_loopback_dev": True,
            "poll_interval_seconds": POLL_SECONDS,
            "freshness_budget_seconds": BUDGET_SECONDS,
            "codec": str(self.bin / "layerx-archive-codec"),
            "ca_file": str(self.work / "tls.crt"),
        }

    def submit(self, index):
        operation = {"0": "register", "1": "open"}[index]
        try:
            result = self.fixture.invoke(operation, int(index), "archive-" + operation)
        except (RuntimeError, OSError, subprocess.SubprocessError) as error:
            raise Failure(f"native signed {operation} submission failed: {error}") from error
        require(any(line.startswith(b"receipt ") and b" result=0 " in line for line in result.stdout.splitlines()),
                f"native signed {operation} submission produced no successful verified receipt")
        return result

    def origin(self, name, source_log=None):
        port = unused_port()
        url = f"https://127.0.0.1:{port}"
        config = dict(self.pins, data_dir=str(self.work / name), listen=f"127.0.0.1:{port}", public_url=url,
                      genesis_manifest=str(self.manifest), genesis_snapshot=str(self.snapshot),
                      source_log=str(source_log or self.source_log), sync_mode="local",
                      tls_cert=str(self.work / "tls.crt"), tls_key=str(self.work / "tls.key"))
        path = self.work / (name + ".json")
        write_json(path, config)
        process = self.start(name, [self.bin / "layerxd", "--relay-archive", path], env=self.runtime_env())
        return url, process

    def relay_config(self, name, upstreams, **extra):
        port = unused_port()
        return dict(self.pins, data_dir=str(self.work / name), listen=f"127.0.0.1:{port}",
                    public_url=f"http://127.0.0.1:{port}", upstreams=upstreams, sync_mode="remote", **extra)

    def launch(self, name, config):
        path = self.work / (name + ".json")
        write_json(path, config)
        return self.start(name, [self.bin / "layerxd", "--relay-archive", path], env=self.runtime_env())

    def config_refusals(self):
        refused = []
        base = self.relay_config("refused-config", ["http://127.0.0.1:9"])
        variants = {
            "local-with-upstreams": (dict(base, sync_mode="local", source_log=str(self.source_log)),
                                     "sync_mode local must not configure remote upstreams"),
            "local-without-source": (dict(base, sync_mode="local", upstreams=[]),
                                     "sync_mode local requires source_log"),
            "remote-without-origins": (dict(base, upstreams=[]),
                                       "sync_mode remote requires upstreams or enabled peer discovery"),
            "budget-below-poll": (dict(base, freshness_budget_seconds=0.05),
                                  "freshness_budget_seconds must be from"),
            "unknown-mode": (dict(base, sync_mode="hybrid"), "sync_mode must be remote or local"),
        }
        for name, (config, message) in variants.items():
            path = self.work / ("config-" + name + ".json")
            write_json(path, config)
            result = self.execute([self.bin / "layerxd", "--relay-archive", path], env=self.runtime_env(),
                                  success=False)
            stderr = result.stderr.decode(errors="replace")
            require(message in stderr, f"{name}: refusal did not name the violated contract: {stderr[-500:]}")
            require(not (self.work / "refused-config/archive.sqlite3").exists(),
                    f"{name}: refused configuration created archive state")
            refused.append({"variant": name, "exit_code": result.returncode, "message": message})
        self.refusals.extend(refused)
        self.passed("served daemon refuses contradictory sync mode and freshness configuration",
                    [f"{item['variant']} exits {item['exit_code']}" for item in refused])

    def local_contract(self):
        self.origin1, self.origin1_process = self.origin("origin-one")
        self.origin2, self.origin2_process = self.origin("origin-two")
        for url in (self.origin1, self.origin2):
            self.until(lambda: self.ready(url)[0] == 200, url + " local readiness")
        code, status = self.ready(self.origin1)
        require(status["mode"] == "local" and status["mode_configured"] is True, "origin misreports its mode")
        require(status["local_source"] == {"present": True, "remote_origins_required": False},
                "local source health contract missing")
        require(status["last_attempt"]["outcome"] == "current" and status["last_attempt"]["origins"] == 1,
                "local attempt did not record a current outcome")
        observation = status["last_observation"]
        require(observation["source"] == "local-source" and observation["head_batch"] == "1",
                "local observation does not name the local source head")
        head = self.request(self.origin1, "/v1/sync/head")[1]
        require(observation["head_batch_id"] == head["head_batch_id"], "local observation disagrees with head")
        require(status["progress"]["head_batch"] == "1" and status["progress"]["at_ms"] is not None,
                "local durable progress missing")
        self.passed("explicit local-source mode is ready from its own source contract without remote origins",
                    ["mode=local mode_configured=true", "local_source.present=true",
                     "last_observation.source=local-source head_batch=1", "readyz=200"])
        url, _process = self.origin("origin-absent-source", self.work / "absent-source.log")
        self.until(lambda: self.ready(url)[1].get("last_attempt") is not None, "absent-source attempt")
        code, status = self.ready(url)
        require(code == 503 and status["ready"] is False, "absent local source reported ready")
        require(status["mode"] == "local" and status["local_source"]["present"] is False,
                "absent local source misreported")
        require(status["last_attempt"]["outcome"] == "source_unavailable", "absent source outcome is untyped")
        require([item["code"] for item in status["last_attempt"]["refusals"]] == ["source_unavailable"],
                "absent source refusal missing")
        require(status["last_observation"] is None and status["freshness"] == "unobserved",
                "absent local source manufactured an observation")
        self.passed("local mode with an absent source fails readiness with a typed source_unavailable outcome",
                    ["readyz=503", "outcome=source_unavailable", "freshness=unobserved"])

    def never_observed(self):
        self.proxy = HostileOrigin(self.origin2, self.context)
        config = self.relay_config("never-observed", [self.proxy.url],
                                   genesis_manifest=str(self.manifest), genesis_snapshot=str(self.snapshot))
        process = self.launch("never-observed", config)
        url = config["public_url"]
        self.until(lambda: self.ready(url)[1].get("last_attempt") is not None, "never-observed attempt")
        code, status = self.ready(url)
        require(code == 503 and status["ready"] is False, "relay without any observation reported ready")
        require(status["freshness"] == "unobserved" and status["last_observation"] is None,
                "relay without an observation reported one")
        require(status["last_attempt"]["outcome"] == "unavailable", "unreachable origin outcome is not unavailable")
        require({item["code"] for item in status["last_attempt"]["refusals"]} == {"unreachable"},
                "unreachable origin refusal missing")
        require(status["progress"]["head_batch"] is None and status["progress"]["at_ms"] is None,
                "relay without origins reported batch progress")
        require(status["mode"] == "remote" and "local_source" not in status, "remote relay misreports mode")
        self.stop(process)
        self.passed("remote relay with no reachable origin is unavailable and unobserved, not ready",
                    ["readyz=503", "freshness=unobserved", "outcome=unavailable", "refusal=unreachable"])

    def equal_head(self):
        self.proxy.mode = "pass"
        self.relay = self.relay_config("relay", [self.origin1, self.proxy.url])
        self.relay_process = self.launch("relay", self.relay)
        url = self.relay["public_url"]
        self.until(lambda: self.ready(url)[0] == 200, "relay fresh readiness")
        code, status = self.ready(url)
        origin_head = self.request(self.origin1, "/v1/sync/head")[1]
        observation = status["last_observation"]
        require(status["freshness"] == "fresh" and status["degraded"] is False, "fresh relay misreported")
        require(observation["head_batch"] == "1" and observation["head_batch_id"] == origin_head["head_batch_id"]
                and observation["head_raw_sha256"] == origin_head["head_raw_sha256"],
                "observation does not prove the pinned origin head")
        require(observation["source"] in (self.origin1, self.proxy.url), "observation source is not an origin")
        progress = status["progress"]
        require(progress["head_batch"] == "1" and progress["at_ms"] is not None, "durable progress missing")
        self.passed("relay backfills and records a proven pinned-head observation",
                    ["readyz=200", "last_observation.head_batch=1 batch_id matches origin",
                     "progress.head_batch=1"])
        first = observation["at_ms"]

        def later():
            value = self.ready(url)[1]
            attempt = value["last_attempt"]
            if (value["last_observation"]["at_ms"] > first and attempt["outcome"] == "current"
                    and attempt["advanced_batches"] == 0):
                return value
            return None
        status = self.until(later, "equal-head observation refresh")
        require(status["progress"]["at_ms"] == progress["at_ms"], "equal head moved durable progress time")
        require(status["last_attempt"]["at_ms"] >= status["last_observation"]["at_ms"],
                "attempt time precedes the observation it produced")
        self.observed = status["last_observation"]
        self.progress = status["progress"]
        self.passed("equal-head polls refresh the observation while durable progress time stays separate",
                    ["outcome=current advanced_batches=0", "observation.at_ms advanced",
                     "progress.at_ms unchanged"])

    def total_loss(self):
        url = self.relay["public_url"]
        self.stop(self.origin1_process)
        self.proxy.mode = "offline"
        lost = now_ms()

        def unavailable():
            value = self.ready(url)[1]
            attempt = value["last_attempt"]
            return value if attempt["outcome"] == "unavailable" and attempt["at_ms"] > lost else None
        status = self.until(unavailable, "unavailable outcome after total origin loss")
        refusals = status["last_attempt"]["refusals"]
        require({item["origin"] for item in refusals} == {self.origin1, self.proxy.url}
                and {item["code"] for item in refusals} == {"unreachable"},
                "total loss did not record every origin as unreachable")
        require(status["last_observation"]["at_ms"] <= lost and status["degraded"] is True,
                "total loss refreshed the observation or cleared degradation")
        require(status["progress"] == self.progress, "total loss changed durable progress")
        age = status["observation_age_ms"]
        require(status["ready"] == (age <= status["freshness_budget_ms"]),
                "readiness disagrees with the declared freshness budget")
        self.passed("total origin loss records unavailable without refreshing the proven observation",
                    ["outcome=unavailable for 2 origins", "observation.at_ms <= loss time",
                     "ready iff age <= budget"])
        self.until(lambda: self.ready(url)[0] == 503, "readiness failure after the freshness budget",
                   seconds=BUDGET_SECONDS * 4)
        status = self.ready(url)[1]
        require(status["freshness"] == "stale" and status["observation_age_ms"] > status["freshness_budget_ms"],
                "readiness failed before the freshness budget elapsed")
        require(status["last_observation"]["at_ms"] <= lost, "stale relay manufactured an observation")
        self.stale_observation = status["last_observation"]
        self.passed("readyz fails once the declared freshness budget elapses",
                    ["readyz=503", "freshness=stale", f"budget_ms={status['freshness_budget_ms']}"])

    def offline_restart(self):
        url = self.relay["public_url"]
        self.stop(self.relay_process)
        sys.path.insert(0, str(self.runtime))
        from protocol import load_config
        from store import ArchiveStore
        config = load_config(self.work / "relay.json")
        persisted = ArchiveStore(config).load_sync_state("remote")
        require(persisted.observation is not None
                and persisted.observation.document() == self.stale_observation,
                "persisted observation differs from the served observation")
        require(persisted.attempt is not None and persisted.attempt.outcome.value == "unavailable",
                "persisted attempt does not retain the unavailable outcome")
        restarted = now_ms()
        self.relay_process = self.launch("relay-restarted", self.relay)

        def attempted():
            value = self.ready(url)[1]
            return value if value["last_attempt"]["at_ms"] > restarted else None
        status = self.until(attempted, "restarted relay attempt")
        code = self.ready(url)[0]
        require(code == 503 and status["ready"] is False and status["freshness"] == "stale",
                "offline restart reported fresh readiness")
        require(status["last_observation"] == self.stale_observation,
                "offline restart manufactured or lost the proven observation")
        require(status["last_attempt"]["outcome"] == "unavailable" and status["degraded"] is True,
                "offline restart cleared degradation without an origin")
        require(status["progress"] == self.progress, "offline restart changed durable progress")
        require(self.request(url, "/v1/sync/head")[1]["head_batch"] == "1", "offline restart lost archived head")
        self.passed("offline restart retains the last proven head and stale freshness from durable storage",
                    ["ArchiveStore.load_sync_state observation == served observation",
                     "readyz=503 freshness=stale after restart", "head_batch=1 retained"])

    def hostile(self):
        url = self.relay["public_url"]
        self.submit("1")
        self.until(lambda: self.request(self.origin2, "/v1/sync/head")[1]["head_batch"] == "2",
                   "origin advanced to batch 2")
        require(self.request(url, "/v1/sync/head")[1]["head_batch"] == "1", "relay advanced without an origin")
        self.proxy.fork_head = "1"
        expected = (("network", "pin_mismatch"), ("genesis", "pin_mismatch"), ("sequencer", "pin_mismatch"),
                    ("malformed_head", "malformed_head"), ("signature", "verification_failed"),
                    ("discontinuity", "discontinuity"), ("fork", "discontinuity"))
        for mode, code in expected:
            self.proxy.mode = mode
            switched = now_ms()

            def refused():
                value = self.ready(url)[1]
                attempt = value["last_attempt"]
                codes = {item["code"] for item in attempt["refusals"] if item["origin"] == self.proxy.url}
                return value if attempt["at_ms"] > switched and codes == {code} else None
            status = self.until(refused, f"{mode} refusal")
            ready_code = self.ready(url)[0]
            require(status["last_attempt"]["outcome"] == "refused", f"{mode} outcome is not refused")
            require(status["progress"] == self.progress, f"{mode} advanced durable progress")
            require(status["last_observation"] == self.stale_observation, f"{mode} produced an observation")
            require(ready_code == 503 and status["degraded"] is True, f"{mode} cleared degradation")
            require(self.request(url, "/v1/sync/head")[1]["head_batch"] == "1", f"{mode} advanced the head")
            self.passed(f"{mode} origin is refused as {code} without advancing canonical progress",
                        [f"refusal={code}", "outcome=refused", "progress unchanged", "readyz=503"])

    def recovery(self):
        url = self.relay["public_url"]
        self.proxy.mode = "pass"
        recovered = now_ms()
        self.until(lambda: self.ready(url)[0] == 200, "recovered readiness")
        status = self.ready(url)[1]
        origin_head = self.request(self.origin2, "/v1/sync/head")[1]
        observation = status["last_observation"]
        require(observation["at_ms"] > recovered and observation["source"] == self.proxy.url,
                "recovery observation does not come from the reconnected origin")
        require(observation["head_batch"] == "2" and observation["head_batch_id"] == origin_head["head_batch_id"],
                "recovery observation does not match the continued origin head")
        require(status["progress"]["head_batch"] == "2" and status["progress"]["at_ms"] > self.progress["at_ms"],
                "recovery did not record durable progress")
        require(status["degraded"] is False and status["last_attempt"]["outcome"] == "current",
                "recovery did not clear degradation")
        require(self.request(url, "/v1/sync/batches/2", raw=True)[1]
                == self.request(self.origin2, "/v1/sync/batches/2", raw=True)[1],
                "recovered batch is not byte exact")
        self.passed("reconnection validates continuity through batch 2 before clearing degradation",
                    ["head_batch=2 batch_id matches origin", "progress.at_ms advanced", "readyz=200"])

    def run(self):
        self.setup()
        self.config_refusals()
        self.local_contract()
        self.never_observed()
        self.equal_head()
        self.total_loss()
        self.offline_restart()
        self.hostile()
        self.recovery()


def revision():
    result = subprocess.run(["git", "--no-optional-locks", "-C", str(ROOT), "rev-parse", "HEAD"],
                            capture_output=True, text=True, timeout=30)
    return result.stdout.strip() if result.returncode == 0 else "unknown"


def isolated():
    env = dict(os.environ)
    for name in ("net", "pid", "mnt"):
        env["PAXEER_X_PARENT_" + name.upper()] = os.readlink("/proc/self/ns/" + name)
    try:
        return subprocess.run(["unshare", "--mount", "--net", "--pid", "--fork", "--kill-child=KILL", "--mount-proc",
                               "--propagation", "private", sys.executable, str(Path(__file__).resolve()),
                               "--worker", *sys.argv[1:]], env=env, cwd=ROOT, timeout=1140).returncode
    except subprocess.TimeoutExpired:
        print("FAIL isolated archive contract exceeded 1140 seconds", flush=True)
        return 1


class RampJournalContract:
    def __init__(self, manifest):
        import stat
        sys.path.insert(0, str(ROOT / "tools/paxeer-x"))
        from candidate import catalogue, load_private, validate
        self.private = load_private
        candidate = load_private(manifest)
        validate(candidate, catalogue(ROOT / "spec/paxeer-x/spec.kvx"))
        self.revision = candidate["source"]["revision"]
        require(self.revision == revision(), "ramp candidate source revision does not match checkout")
        ramp = next((service for service in candidate["services"] if service["id"] == "ramp"), None)
        require(ramp is not None, "candidate ramp service absent")
        require(ramp["bindings"]["source_revision"] == self.revision, "ramp source binding absent")
        self.inputs = load_private(os.environ.get("PAXEER_X_RAMP_READINESS_INPUTS", ""))
        self.binary = Path(self.inputs["binary"])
        require(self.binary.is_file() and os.access(self.binary, os.X_OK), "ramp binary absent")
        require(hashlib.sha256(self.binary.read_bytes()).hexdigest() == self.inputs["binary_sha256"],
                "ramp binary digest mismatch")
        self.config = load_private(self.inputs["config"])
        config_digest = "sha256:" + hashlib.sha256(Path(self.inputs["config"]).read_bytes()).hexdigest()
        require(config_digest == ramp["bindings"]["config_digest"], "ramp configuration binding mismatch")
        require(self.config["paxeer"]["rpc_chain_id"] == candidate["foundation"]["chain_id"],
                "ramp chain pin differs from candidate")
        require(self.config["layerx"]["network_id"] == self.inputs["network_id"]
                and self.config["layerx"]["protocol_version"] == self.inputs["wire_version"],
                "explicit LayerX network or wire pin missing")
        self.tokens = {}
        for actor, key in (("customer", "customer_token_file"), ("operator", "operator_control_token_file")):
            path = Path(self.inputs[key] if actor == "customer" else self.config[key])
            info = path.lstat()
            require(stat.S_ISREG(info.st_mode) and info.st_uid == os.geteuid()
                    and info.st_nlink == 1 and info.st_mode & 0o077 == 0,
                    "ramp credential input is not private")
            self.tokens[actor] = path.read_text().strip()
            require(bool(self.tokens[actor]), "ramp credential empty")
        self.work = Path(tempfile.mkdtemp(prefix="ramp-journal-", dir=self.inputs["evidence_directory"]))
        self.work.chmod(0o700)
        self.processes = []
        self.cases = []
        self.port = unused_port()
        self.config.update(listen=f"127.0.0.1:{self.port}", listener="plain",
                           server_identity_pkcs12=None, server_identity_password_file=None)
        self.url = f"http://127.0.0.1:{self.port}"
        self.journal = self.work / "journal.jsonl"
        self.config["journal_path"] = str(self.journal)
        self.config_path = self.work / "config.json"
        self.save(self.config_path, self.config)
        self.process = None

    def save(self, path, value):
        with path.open("x") as stream:
            json.dump(value, stream)
        path.chmod(0o600)

    def request(self, path, body=None, actor="operator"):
        data = None if body is None else json.dumps(body).encode()
        request = urllib.request.Request(self.url + path, data=data,
                    headers={"Authorization": "Bearer " + self.tokens[actor], "Content-Type": "application/json"})
        try:
            with urllib.request.urlopen(request, timeout=30) as response:
                return response.status, json.load(response)
        except urllib.error.HTTPError as error:
            return error.code, json.loads(error.read())

    def start(self, limit=None):
        import resource
        import signal
        def restricted():
            resource.setrlimit(resource.RLIMIT_CORE, (0, 0))
            if limit is not None:
                signal.signal(signal.SIGXFSZ, signal.SIG_IGN)
                resource.setrlimit(resource.RLIMIT_FSIZE, (limit, limit))
        process = subprocess.Popen([str(self.binary), str(self.config_path)], stdin=subprocess.DEVNULL,
                     stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, preexec_fn=restricted)
        self.processes.append(process)
        self.process = process
        return process

    def stop(self):
        if self.process and self.process.poll() is None:
            self.process.terminate()
            try:
                self.process.wait(timeout=10)
            except subprocess.TimeoutExpired:
                self.process.kill()
                self.process.wait(timeout=10)

    def ready(self, expected):
        deadline = time.monotonic() + 90
        while time.monotonic() < deadline:
            require(self.process.poll() is None, "ramp exited before readiness observation")
            try:
                code, body = self.request("/readyz")
                if code == expected:
                    require(body["ready"] == (expected == 200), "readiness body disagrees with status")
                    return body
            except (OSError, ValueError):
                pass
            time.sleep(0.1)
        raise Failure("ramp readiness deadline")

    def passed(self, name, assertions):
        self.cases.append({"name": name, "assertions": assertions})

    def provider_callback_recovery(self):
        import stat
        callback = self.private(self.inputs["provider_callback"])
        source = Path(self.inputs["callback_journal"])
        info = source.lstat()
        require(stat.S_ISREG(info.st_mode) and info.st_uid == os.geteuid()
                and info.st_nlink == 1 and info.st_mode & 0o077 == 0,
                "callback journal input is not private")
        saved_config = dict(self.config)
        saved_path = self.config_path
        journal = self.work / "callback-recovery.jsonl"
        journal.write_bytes(source.read_bytes())
        journal.chmod(0o600)
        self.config = dict(self.config, journal_path=str(journal), reconcile_seconds=3600)
        self.config_path = self.work / "callback-recovery-config.json"
        self.save(self.config_path, self.config)
        digest = bytes(callback["result"]["order_digest"]).hex()
        self.start()
        self.ready(200)
        code, before = self.request("/v1/orders/" + digest, actor="customer")
        require(code == 200 and before["stage"] == "awaiting_external_credit",
                "real callback recovery order is not awaiting external credit")
        original = journal.read_bytes()
        for index in range(1, 9):
            if len(original) > len(json.dumps(callback).encode()) + 1024:
                break
            seed = dict(self.inputs["create_order"])
            seed["order_id"] += "-callback-prefix-" + str(index)
            code, _ = self.request("/v1/orders", seed, "customer")
            require(code == 201, "real callback journal prefix creation refused")
            original = journal.read_bytes()
        require(len(original) > len(json.dumps(callback).encode()) + 1024,
                "callback journal prefix too short for real OS interruption")
        self.stop()
        self.start(len(original) + 64)
        self.ready(200)
        code, _ = self.request("/v1/provider-callbacks", callback)
        require(code == 503, "signed provider callback append did not encounter OS refusal")
        self.ready(503)
        damaged = journal.read_bytes()
        require(damaged.startswith(original) and len(damaged) == len(original) + 64
                and not damaged.endswith(b"\n"), "signed callback append did not retain its exact torn prefix")
        intent_path = Path(str(journal) + ".append-intent")
        raw = intent_path.read_bytes()
        domain = b"LXP/market-maker-ramp/append-intent/v1\0"
        header = len(domain) + 12
        require(raw.startswith(domain) and len(raw) > header + 32
                and hashlib.sha256(raw[:-32]).digest() == raw[-32:], "provider pending intent invalid")
        length = int.from_bytes(raw[len(domain) + 8:header], "big")
        require(len(raw) == header + length + 32
                and int.from_bytes(raw[len(domain):len(domain) + 8], "big") == len(original),
                "provider pending intent window differs")
        pending = raw[header:-32]
        record = json.loads(pending)
        event = record["event"]["provider_callback_applied"]
        require(pending.startswith(damaged[len(original):])
                and event["callback_id"] == callback["callback_id"]
                and event["order_digest"] == callback["result"]["order_digest"]
                and event["provider_sequence"] == callback["provider_sequence"]
                and event["evidence"]["provider_operation_id"] == callback["result"]["operation_id"]
                and event["evidence"]["provider_evidence_digest"] == callback["result"]["evidence_digest"],
                "retained pending callback lost authenticated provider identity")
        code, _ = self.request("/v1/provider-callbacks", callback)
        require(code == 503 and journal.read_bytes() == damaged,
                "provider callback retry bypassed persistent halt")
        self.stop()
        self.start()
        self.ready(200)
        expected = original + pending
        require(journal.read_bytes() == expected and expected.startswith(damaged)
                and not intent_path.exists(), "provider recovery rewrote history or duplicated the event")
        code, recovered = self.request("/v1/orders/" + digest, actor="customer")
        require(code == 200 and recovered["stage"] == "provider_settled"
                and recovered["presentation"]["provider_evidence_digest"] == callback["result"]["evidence_digest"]
                and recovered["presentation"]["status"] == "pending",
                "recovery did not bind real provider settlement or falsely completed the LayerX leg")
        code, response = self.request("/v1/provider-callbacks", callback)
        require(code == 200 and response["accepted"] is True and journal.read_bytes() == expected,
                "recovered provider callback retry duplicated its event")
        callbacks = [json.loads(line)["event"].get("provider_callback_applied", {})
                     for line in journal.read_bytes().splitlines()]
        require(sum(event.get("callback_id") == callback["callback_id"] for event in callbacks) == 1,
                "recovered provider callback identity is not unique")
        self.passed("interrupted_provider_callback", ["real signed callback and OS append refusal",
                    "awaiting_external_credit retains exact callback request/provider identity",
                    "restart validates actual provider settlement before completing the missing bytes",
                    "one callback record and duplicate retry no-op", "LayerX leg remains pending"])
        self.stop()
        self.config = saved_config
        self.config_path = saved_path

    def run(self):
        self.start()
        self.ready(200)
        code, _ = self.request("/v1/orders", self.inputs["create_order"], "customer")
        require(code == 201, "real authenticated order creation refused")
        original = self.journal.read_bytes()
        require(original.endswith(b"\n") and bool(original), "real journal record absent")
        require(len(self.inputs["create_order"]["order_id"]) <= 80, "journal case order identity too long")
        for index in range(1, 9):
            if len(original) > len(original.splitlines(keepends=True)[-1]) + 1024:
                break
            seed = dict(self.inputs["create_order"])
            seed["order_id"] += "-journal-prefix-" + str(index)
            code, _ = self.request("/v1/orders", seed, "customer")
            require(code == 201, "real journal prefix order creation refused")
            original = self.journal.read_bytes()
        require(len(original) > len(original.splitlines(keepends=True)[-1]) + 1024,
                "durable journal prefix too short for the real append interruption")
        conflict = subprocess.run([str(self.binary), str(self.config_path)], stdin=subprocess.DEVNULL,
                    stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, timeout=90)
        require(conflict.returncode != 0 and self.journal.read_bytes() == original,
                "exclusive writer conflict failed to refuse or changed history")
        self.passed("exclusive_writer", ["second process nonzero", "history unchanged"])
        self.stop()
        self.start()
        self.ready(200)
        require(self.journal.read_bytes() == original, "verified restart changed journal history")
        self.passed("verified_restart", ["readyz=200", "history unchanged after replay and external checks"])
        code, _ = self.request("/internal/v1/journal/recover", {})
        require(code == 200, "explicit verified recovery refused")
        self.passed("explicit_recovery", ["authenticated recovery=200", "external settlement verified"])
        self.stop()
        self.start(len(original) + 64)
        self.ready(200)
        interrupted = dict(self.inputs["create_order"])
        interrupted["order_id"] += "-interrupted"
        code, _ = self.request("/v1/orders", interrupted, "customer")
        require(code == 503, "OS interrupted append did not return durable refusal")
        state = self.ready(503)
        damaged = self.journal.read_bytes()
        require(damaged.startswith(original) and len(damaged) > len(original)
                and not damaged.endswith(b"\n"), "OS interrupted append did not retain a torn tail")
        intent_path = Path(str(self.journal) + ".append-intent")
        raw = intent_path.read_bytes()
        domain = b"LXP/market-maker-ramp/append-intent/v1\0"
        header = len(domain) + 12
        require(raw.startswith(domain) and len(raw) > header + 32, "durable exact append intent absent")
        offset = int.from_bytes(raw[len(domain):len(domain) + 8], "big")
        length = int.from_bytes(raw[len(domain) + 8:header], "big")
        require(len(raw) == header + length + 32 and hashlib.sha256(raw[:-32]).digest() == raw[-32:],
                "durable pending record framing or digest invalid")
        pending = raw[header:-32]
        require(offset == len(original) and pending.startswith(damaged[len(original):])
                and pending.endswith(b"\n"), "torn bytes do not match exact retained pending record")
        require(state["journal"]["halted"] and state["journal"]["recovery_required"],
                "interrupted append lost its halt")
        for _ in range(3):
            code, health = self.request("/internal/v1/journal")
            require(code == 200 and health["halted"] and not health["ready"], "status read cleared halt")
        code, _ = self.request("/v1/orders", interrupted, "customer")
        require(code == 503, "halted mutation was admitted")
        code, _ = self.request("/internal/v1/journal/recover", {})
        require(code == 503 and self.journal.read_bytes() == damaged and intent_path.read_bytes() == raw,
                "recovery under the same OS write refusal changed retained history or intent")
        self.stop()
        original_config_path = self.config_path
        mismatch_journal = self.work / "mismatched-tail.jsonl"
        mismatch_bytes = bytearray(damaged)
        mismatch_bytes[len(original)] ^= 1
        mismatch_journal.write_bytes(mismatch_bytes)
        mismatch_journal.chmod(0o600)
        mismatch_intent = Path(str(mismatch_journal) + ".append-intent")
        mismatch_intent.write_bytes(raw)
        mismatch_intent.chmod(0o600)
        self.config_path = self.work / "mismatched-tail-config.json"
        self.save(self.config_path, dict(self.config, journal_path=str(mismatch_journal)))
        self.start()
        self.ready(503)
        code, _ = self.request("/internal/v1/journal/recover", {})
        require(code == 503 and mismatch_journal.read_bytes() == mismatch_bytes
                and mismatch_intent.read_bytes() == raw, "unexplained tail was rewritten or admitted")
        self.passed("unexplained_tail", ["nonmatching retained prefix refuses recovery=503",
                    "journal and pending intent unchanged"])
        self.stop()
        self.config_path = original_config_path
        self.start()
        self.ready(200)
        recovered = original + pending
        require(self.journal.read_bytes() == recovered and recovered.startswith(damaged)
                and not intent_path.exists(), "restart did not complete exactly the retained record")
        code, _ = self.request("/v1/orders", interrupted, "customer")
        require(code == 201 and self.journal.read_bytes() == recovered,
                "recovered retry duplicated the order or its durable event")
        self.passed("interrupted_append", ["real RLIMIT_FSIZE interruption", "halted readyz=503",
                    "status reads retain halt", "mutation=503", "bounded recovery=503 preserves bytes",
                    "restart completes exact retained record without truncation", "retry adds no event"])
        self.stop()
        invalid = self.work / "invalid.jsonl"
        invalid.write_bytes(original.replace(b'"record_hash":', b'"invalid_hash":', 1))
        invalid.chmod(0o600)
        invalid_config = dict(self.config, journal_path=str(invalid))
        invalid_path = self.work / "invalid-config.json"
        self.save(invalid_path, invalid_config)
        refusal = subprocess.run([str(self.binary), str(invalid_path)], stdin=subprocess.DEVNULL,
                    stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, timeout=90)
        require(refusal.returncode != 0, "invalid journal process did not refuse")
        self.passed("invalid_journal", ["corrupt record process exits nonzero", "no history repair"])
        self.provider_callback_recovery()
        source = Path(self.inputs["provider_journal"])
        import stat
        info = source.lstat()
        require(stat.S_ISREG(info.st_mode) and info.st_uid == os.geteuid()
                and info.st_nlink == 1 and info.st_mode & 0o077 == 0,
                "provider journal input is not private")
        provider_journal = self.work / "provider.jsonl"
        provider_journal.write_bytes(source.read_bytes())
        provider_journal.chmod(0o600)
        self.config["journal_path"] = str(provider_journal)
        self.config_path = self.work / "provider-config.json"
        self.save(self.config_path, self.config)
        self.start()
        self.ready(200)
        required = {"pending", "reversal", "chargeback"}
        require(set(self.inputs["provider_cases"]) == required, "required real provider cases missing")
        for name, expected in self.inputs["provider_cases"].items():
            code, order = self.request("/v1/orders/" + expected["order_digest"], actor="customer")
            require(code == 200 and order["stage"] == expected["stage"], "provider typed stage changed")
            presentation = order["presentation"]
            require(presentation["external_custody_label"] and presentation["status"] == expected["status"],
                    "provider custody or aggregate state lost")
            require(expected["status"] == ("pending" if name == "pending" else "reversed"),
                    "provider expected state weakens required case")
            if name == "chargeback":
                require(presentation["refusal_code"] == "chargeback", "chargeback reason lost")
            self.passed(name, ["real provider recovery succeeded", "declared typed status retained"])
        self.stop()
        self.start()
        self.ready(200)
        environment = dict(os.environ)
        environment.update(self.private(self.inputs["journey_environment"]))
        verifier = self.work / "receipt-verifier.json"
        self.save(verifier, {"client_tls": self.config["client_tls"], "layerx": self.config["layerx"]})
        environment.update(LAYERX_RAMP_URL=self.url, LAYERX_RAMP_OPERATOR_URL=self.url,
                           LAYERX_RAMP_CUSTOMER_TOKEN=self.tokens["customer"],
                           LAYERX_RAMP_OPERATOR_TOKEN=self.tokens["operator"],
                           LAYERX_RAMP_REFERENCE_BIN=str(self.binary),
                           LAYERX_RAMP_RECEIPT_VERIFIER_CONFIG=str(verifier))
        journey = subprocess.run([str(ROOT / "platform/ramps/sandbox-journey.sh")], env=environment,
                    stdin=subprocess.DEVNULL, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, timeout=600)
        require(journey.returncode == 0, "recovered real provider journey or receipt verification failed")
        self.passed("recovered_journey", ["real sandbox on/off ramp done", "independent LayerX receipt verified",
                    "external custody declared"])
        require(len(self.cases) == 11, "required ramp execution cases absent")

    def close(self):
        for process in self.processes:
            if process.poll() is None:
                process.kill()
                process.wait(timeout=10)


def ramp_journal_main(manifest):
    import http.client
    contract = None
    try:
        contract = RampJournalContract(manifest)
        contract.run()
        contract.save(contract.work / "result.json", {"status": "passed", "case": "ramp-journal-readiness",
                      "revision": contract.revision, "cases": contract.cases})
        print("RESULT passed case=ramp-journal-readiness cases=11")
        return 0
    except (Failure, OSError, ValueError, KeyError, TypeError, subprocess.TimeoutExpired) as error:
        print("FAIL ramp-journal-readiness " + type(error).__name__)
        if contract is not None:
            contract.save(contract.work / "result.json", {"status": "failed", "cases": contract.cases,
                          "failure_type": type(error).__name__})
        return 1
    finally:
        if contract is not None:
            contract.close()
            print("Evidence: " + str(contract.work))


class CheckpointInterposer:
    def __init__(self, upstream, path):
        self.upstream = str(upstream)
        self.path = path
        self.mode = "pass"
        self.seen = {}
        self.authentic = []
        self.errors = []
        self.closed = False
        self.connections = []
        self.server = socket.socket(socket.AF_UNIX)
        self.server.bind(str(path))
        self.server.listen(16)
        threading.Thread(target=self.accept, daemon=True).start()

    @staticmethod
    def frame(connection):
        def exact(length):
            result = bytearray()
            while len(result) < length:
                chunk = connection.recv(length - len(result))
                if not chunk:
                    raise EOFError()
                result.extend(chunk)
            return bytes(result)
        length = int.from_bytes(exact(4), "big")
        require(22 <= length <= 67108864, "LNI frame bound")
        return exact(length)

    @staticmethod
    def decode(body):
        require(len(body) >= 22, "LNI envelope truncated")
        length = int.from_bytes(body[14:18], "big")
        end = 18 + length
        require(end + 4 <= len(body), "LNI payload truncated")
        proof_length = int.from_bytes(body[end:end + 4], "big")
        require(end + 4 + proof_length == len(body), "LNI proof length")
        return int.from_bytes(body[4:6], "big"), body[18:end], body[end + 4:]

    @staticmethod
    def mutate(mode, payload, proof):
        data = bytearray(payload)
        if mode in ("authority", "legacy-authority"):
            require(proof[:2] == b"\x00\x02" and len(proof) > 32, "CXv2 authority missing")
            proof = (b"\x00\x01" + proof[2:-32]) if mode == "legacy-authority" else proof[:-1] + bytes([proof[-1] ^ 1])
            return payload, proof
        require(data[:2] == b"\x00\x01", "native checkpoint CP1 missing")
        header_length = int.from_bytes(data[2:6], "big")
        require(header_length == 354, "canonical header length")
        proof_at = 6 + header_length
        proof_length = int.from_bytes(data[proof_at:proof_at + 4], "big")
        count_at = proof_at + 4 + proof_length
        require(count_at < len(data), "native certificate truncated")
        count = data[count_at]
        threshold_at = count_at + 1 + count * 274
        require(count > 0 and threshold_at + 3 < len(data), "native attestations missing")
        positions = {"domain": 6 + 9, "sequence": 6 + 32, "root": 6 + 91,
                     "membership": count_at + 1 + 189}
        if mode in positions:
            data[positions[mode]] ^= 1
        elif mode == "threshold":
            data[threshold_at] = 0
        return bytes(data), proof

    def accept(self):
        while not self.closed:
            try:
                client, _ = self.server.accept()
            except OSError:
                return
            threading.Thread(target=self.session, args=(client,), daemon=True).start()

    def session(self, client):
        node = socket.socket(socket.AF_UNIX)
        self.connections.extend((client, node))
        try:
            node.connect(self.upstream)
            threading.Thread(target=self.pump, args=(client, node, False), daemon=True).start()
            self.pump(node, client, True)
        except OSError:
            pass
        finally:
            client.close()
            node.close()

    def pump(self, source, target, response):
        try:
            while not self.closed:
                body = self.frame(source)
                tag, payload, proof = self.decode(body)
                mode = self.mode
                if not response and tag == 14 and mode == "bare":
                    self.seen[mode] = self.seen.get(mode, 0) + 1
                    target.shutdown(socket.SHUT_RDWR)
                    source.shutdown(socket.SHUT_RDWR)
                    return
                if response and tag == 2 and mode in ("no-proof", "head-behind", "selector"):
                    require(len(payload) >= 91, "node information truncated")
                    data = bytearray(payload)
                    if mode == "no-proof":
                        data[27:59] = bytes(32)
                    elif mode == "selector":
                        data[58] ^= 1
                    else:
                        data[11:19] = bytes(8)
                    payload = bytes(data)
                    self.seen[mode] = self.seen.get(mode, 0) + 1
                if response and tag == 15 and mode == "pass":
                    self.authentic.append((payload, proof))
                if response and tag == 15 and mode not in ("pass", "no-proof", "head-behind", "selector"):
                    payload, proof = self.mutate(mode, payload, proof)
                    self.seen[mode] = self.seen.get(mode, 0) + 1
                body = body[:14] + len(payload).to_bytes(4, "big") + payload + len(proof).to_bytes(4, "big") + proof
                target.sendall(len(body).to_bytes(4, "big") + body)
        except (OSError, EOFError):
            return
        except (Failure, ValueError, IndexError) as error:
            self.errors.append(type(error).__name__)

    def close(self):
        self.closed = True
        self.server.close()
        for connection in self.connections:
            try:
                connection.shutdown(socket.SHUT_RDWR)
                connection.close()
            except OSError:
                pass


class MirrorCheckpointContract:
    def __init__(self, manifest):
        sys.path.insert(0, str(ROOT / "tools/paxeer-x"))
        from candidate import catalogue, load_private, validate
        candidate = load_private(manifest)
        validate(candidate, catalogue(ROOT / "spec/paxeer-x/spec.kvx"))
        self.revision = candidate["source"]["revision"]
        require(self.revision == revision(), "mirror source revision differs from checkout")
        service = next((item for item in candidate["services"] if item["id"] == "mirror"), None)
        require(service is not None and service["bindings"]["source_revision"] == self.revision,
                "mirror candidate source binding absent")
        self.inputs = load_private(os.environ.get("PAXEER_X_MIRROR_CHECKPOINT_INPUTS", ""))
        require(self.inputs["purpose"] == "disposable-real-checkpoint-mirror-qualification",
                "disposable funded chain and node authority input absent")
        require(self.inputs["source_revision"] == self.revision, "artifact source revision mismatch")
        self.binaries = {}
        for name in ("layerxd", "layerx-mirror-publisher", "layerx-mirror-verify"):
            artifact = self.inputs["artifacts"][name]
            path = Path(artifact["path"])
            require(path.is_file() and os.access(path, os.X_OK), "built executable absent")
            require(hashlib.sha256(path.read_bytes()).hexdigest() == artifact["sha256"], "artifact digest mismatch")
            self.binaries[name] = path
        self.config = load_private(self.inputs["publisher_config"])
        digest = "sha256:" + hashlib.sha256(Path(self.inputs["publisher_config"]).read_bytes()).hexdigest()
        require(digest == service["bindings"]["config_digest"], "publisher config binding mismatch")
        require(self.config.get("solana") is not None, "both independent mirror lanes required")
        require(self.config["node"]["checkpoint_policy"]["chain_id"] == candidate["foundation"]["chain_id"],
                "native finality chain pin mismatch")
        self.verifier = load_private(self.inputs["verifier_config"])
        require([source["kind"] for source in self.verifier["sources"]] == ["ethereum", "solana"],
                "independent exact readback sources missing")
        require(self.verifier["layerx_network_id"] == self.config["node"]["expected_network_id"]
                and self.verifier["sequencer_public_key_hex"] == self.config["node"]["checkpoint_policy"]["sequencer_public_key_hex"],
                "offline verifier domain pin mismatch")
        self.work = Path(tempfile.mkdtemp(prefix="mirror-checkpoint-", dir=self.inputs["evidence_directory"]))
        self.work.chmod(0o700)
        self.children = []
        self.logs = []
        self.cases = []
        self.proxy = None
        self.publisher = None
        self.node = None
        self.state = self.work / "publisher-state"
        self.port = unused_port()
        self.url = f"http://127.0.0.1:{self.port}"
        self.save(self.work / "inventory.json", {
            "source_revision": self.revision,
            "ethereum_archive_contract": self.config["ethereum"]["archive_contract_hex"],
            "solana_archive_program": self.config["solana"]["archive_program_base58"],
            "rpc_backends": {chain: [endpoint["independent_backend"] for endpoint in self.config[chain]["rpc"]["endpoints"]]
                             for chain in ("ethereum", "solana")},
            "native_authority_backends": [endpoint["independent_backend"] for endpoint in self.config["node"]["checkpoint_policy"]["rpc"]["endpoints"]],
            "readback_sources": [source["id"] for source in self.verifier["sources"]],
        })
        self.config.update(state_directory=str(self.state), status_listen=f"127.0.0.1:{self.port}", poll_interval_ms=200)
        self.batch = int(self.inputs["receipt_batch"])
        self.receipt = Path(self.inputs["receipt_canonical"]).read_bytes()
        require(self.batch > 0 and self.receipt, "real canonical receipt input absent")
        self.config["first_batch_number"] = self.batch

    @staticmethod
    def save(path, value):
        with path.open("w") as stream:
            json.dump(value, stream)
        path.chmod(0o600)

    def launch(self, name, argv, env=None):
        output = (self.work / (name + ".log")).open("ab")
        self.logs.append(output)
        process = subprocess.Popen([str(arg) for arg in argv], env=env, stdin=subprocess.DEVNULL,
                                   stdout=output, stderr=output)
        self.children.append(process)
        return process

    @staticmethod
    def stop(process):
        if process is not None and process.poll() is None:
            process.terminate()
            try:
                process.wait(timeout=10)
            except subprocess.TimeoutExpired:
                process.kill()
                process.wait(timeout=10)

    def until(self, predicate, label, seconds=150):
        deadline = time.monotonic() + seconds
        while time.monotonic() < deadline:
            if self.proxy is not None:
                require(not self.proxy.errors, "LNI mutation did not reach its real wire boundary")
            try:
                value = predicate()
                if value:
                    return value
            except (OSError, ValueError, KeyError, urllib.error.URLError):
                pass
            time.sleep(0.2)
        raise Failure("timed out: " + label)

    def http(self, path):
        try:
            response = urllib.request.urlopen(self.url + path, timeout=5)
        except urllib.error.HTTPError as error:
            response = error
        with response:
            return response.status, json.loads(response.read())

    def status(self):
        return self.http("/status")[1]

    def start_publisher(self):
        self.save(self.work / "publisher.json", self.config)
        self.publisher = self.launch("publisher", [self.binaries["layerx-mirror-publisher"], self.work / "publisher.json"])

    def ready(self):
        status = self.status()
        coordinate = status["latest_verified_checkpoint_batch"]
        if self.http("/readyz")[0] != 200 or coordinate is None or coordinate < self.batch:
            return False
        require(status["checkpoint_proof_boundary_ready"] and status["checkpoint_refusal"] is None,
                "ready without authentic checkpoint")
        for chain in ("ethereum", "solana"):
            lane = status[chain]
            require(lane["latest_checkpoint_batch_mirrored"] == coordinate
                    and lane["latest_checkpoint_id_mirrored"] == status["latest_verified_checkpoint_id"]
                    and lane["checkpoint_within_budget"] and lane["freshness"] == "current",
                    "ready without independent checkpoint readback and freshness")
        return status

    def offline(self, source, commitment, receipt, expected):
        request = {"batch_number": str(self.batch), "evidence": {"kind": "receipt", "canonical_hex": receipt.hex()},
                   "policy": {"kind": "exact", "candidate": {"source": source, "commitment_hex": commitment}}}
        result = subprocess.run([str(self.binaries["layerx-mirror-verify"]), str(self.work / "verifier.json")],
                                input=json.dumps(request), text=True, capture_output=True, timeout=90)
        require(result.returncode == 0 and json.loads(result.stdout).get("ok") is expected,
                "offline chain receipt verification disagreed")

    def run(self):
        snapshot = Path(self.inputs["node_snapshot"]).resolve()
        require(snapshot.is_dir() and not (snapshot / ".qualification-running").exists(), "stopped durable node snapshot required")
        local = self.work / "node"
        shutil.copytree(snapshot, local, symlinks=False)
        config_name = self.inputs["node_config_relative"]
        require(not Path(config_name).is_absolute() and ".." not in Path(config_name).parts, "node config escapes snapshot")
        config_path = local / config_name
        original = config_path.read_text()
        require(str(snapshot) in original, "node snapshot paths are not relocatable")
        config_path.write_text(original.replace(str(snapshot), str(local)))
        environment = dict(os.environ)
        for key, value in self.inputs["node_environment"].items():
            require(key.startswith("LAYERX_"), "node environment key outside real daemon interface")
            environment[key] = value.replace(str(snapshot), str(local))
        upstream = local / self.inputs["node_socket_relative"]
        require(upstream.is_relative_to(local), "node socket escapes snapshot")
        self.node = self.launch("layerxd", [self.binaries["layerxd"], "--serve", config_path], environment)
        self.until(lambda: upstream.exists() and self.node.poll() is None, "real durable node startup")
        self.proxy = CheckpointInterposer(upstream, self.work / "mirror.lni")
        self.config["node"]["socket"] = str(self.proxy.path)
        self.start_publisher()
        def pending_signed_publication():
            status = self.status()
            return any(status[chain]["phase"] in ("signed", "broadcast_unknown", "pending")
                       for chain in ("ethereum", "solana"))
        self.until(pending_signed_publication, "durable in-flight signed publication", 150)
        self.stop(self.publisher)
        pending = {str(path.relative_to(self.state)): path.read_bytes()
                   for path in self.state.rglob("*.journal") if path.is_file()}
        require(pending, "in-flight publication did not persist a journal")
        self.start_publisher()
        baseline = self.until(self.ready, "both finalized chain readbacks")
        for name, content in pending.items():
            require((self.state / name).read_bytes().startswith(content), "crash replaced persisted signed publication")
        self.cases.append("in-flight-signed-publication-crash-recovery")
        self.cases.append({"case": "authentic-native-final-checkpoint-both-chain-readback",
                           "status": baseline, "http_status": 200})
        for mode in ("bare", "selector", "root", "domain", "sequence", "membership", "threshold", "authority", "legacy-authority", "head-behind"):
            self.proxy.mode = mode
            def refused():
                status = self.status()
                return (self.proxy.seen.get(mode, 0) > 0 and self.http("/readyz")[0] == 503
                        and not status["checkpoint_proof_boundary_ready"] and status["checkpoint_refusal"] is not None)
            self.until(refused, mode + " refusal", 40)
            self.cases.append({"case": mode + "-refused", "http_status": self.http("/readyz")[0],
                               "typed_refusal": self.status()["checkpoint_refusal"],
                               "mutated_real_responses": self.proxy.seen[mode]})
            if mode == "authority":
                refusal = self.status()["checkpoint_refusal"]
                self.proxy.mode = "no-proof"
                self.until(lambda: self.proxy.seen.get("no-proof", 0) >= 3, "later no-proof observations", 30)
                status = self.status()
                require(status["checkpoint_refusal"] == refusal and not status["checkpoint_proof_boundary_ready"]
                        and self.http("/readyz")[0] == 503, "no-proof observation cleared authority refusal")
                self.cases.append("no-proof-cannot-clear-refusal")
            self.proxy.mode = "pass"
            self.until(self.ready, "authentic proof recovery", 90)
        self.stop(self.publisher)
        before = {str(path.relative_to(self.state)): path.read_bytes() for path in self.state.rglob("*") if path.is_file()}
        require(any(name.endswith(".journal") for name in before) and any(name.endswith(".archive") for name in before),
                "durable publication journal and archive absent")
        self.start_publisher()
        restored = self.until(self.ready, "restart canonical readback")
        require(restored["latest_verified_checkpoint_id"] == baseline["latest_verified_checkpoint_id"], "restart changed checkpoint")
        for name, content in before.items():
            require((self.state / name).read_bytes() == content, "settled restart changed archive or duplicated publication records")
        self.cases.append("restart-retains-signed-publication-and-reconciles")
        archived = list(self.state.glob("archives/*.archive"))
        require(self.proxy.authentic and archived, "native certificate preservation inputs absent")
        for path in archived:
            blob = path.read_bytes()
            require(b"LXMIRROR\x00\x03" in blob and b"LXCPAUTH\x00\x01" in blob,
                    "lossless native archive version absent")
            require(any(payload in blob and proof in blob for payload, proof in self.proxy.authentic),
                    "archive dropped or substituted native CP1/CX2 bytes")
        self.cases.append("native-certificate-context-retained-byte-exactly")
        self.stop(self.publisher)
        self.stop(self.node)
        self.proxy.close()
        require(self.node.poll() is not None, "LayerX remains reachable during offline proof")
        self.save(self.work / "verifier.json", self.verifier)
        archives = sorted(self.state.glob("archives/*.archive"))
        require(archives, "real published archive missing")
        # Exact commitments are derived from the production spool filenames.
        for source in (0, 1):
            matched = False
            for archive in archives:
                request = {"batch_number": str(self.batch), "evidence": {"kind": "receipt", "canonical_hex": self.receipt.hex()},
                           "policy": {"kind": "exact", "candidate": {"source": source, "commitment_hex": archive.stem}}}
                result = subprocess.run([str(self.binaries["layerx-mirror-verify"]), str(self.work / "verifier.json")],
                                        input=json.dumps(request), text=True, capture_output=True, timeout=90)
                require(result.returncode == 0, "real offline verifier failed")
                if json.loads(result.stdout).get("ok") is True:
                    mutated = bytearray(self.receipt)
                    mutated[-1] ^= 1
                    self.offline(source, archive.stem, bytes(mutated), False)
                    matched = True
                    break
            require(matched, "no independently published archive proves the real receipt")
        self.cases.append({"case": "both-chains-prove-receipt-with-layerx-stopped-and-reject-tamper",
                           "sources": [0, 1], "layerxd_exit_code": self.node.returncode,
                           "valid_receipt_ok": True, "tampered_receipt_ok": False})
        require(len(self.cases) == 16, "required mirror case execution absent")

    def close(self):
        if self.proxy is not None:
            self.proxy.close()
        for process in reversed(self.children):
            self.stop(process)
        for output in self.logs:
            output.close()


def mirror_checkpoint_main(manifest):
    contract = None
    try:
        contract = MirrorCheckpointContract(manifest)
        contract.run()
        contract.save(contract.work / "result.json", {"status": "passed", "revision": contract.revision,
                      "case": "mirror-checkpoint-acquisition", "cases": contract.cases})
        print("RESULT passed case=mirror-checkpoint-acquisition")
        return 0
    except (Failure, OSError, ValueError, KeyError, TypeError, subprocess.TimeoutExpired) as error:
        print("FAIL mirror-checkpoint-acquisition " + type(error).__name__)
        if contract is not None:
            contract.save(contract.work / "result.json", {"status": "failed", "cases": contract.cases,
                          "failure_type": type(error).__name__, "assertion": str(error)})
        return 1
    finally:
        if contract is not None:
            contract.close()
            print("Evidence: " + str(contract.work))


def main():
    parser = argparse.ArgumentParser(prog="interop-archive-contract")
    parser.add_argument("--case", required=True, choices=CASES)
    parser.add_argument("--candidate-manifest", required=True)
    parser.add_argument("--build-dir", default=str(ROOT / "build"))
    parser.add_argument("--worker", action="store_true", help=argparse.SUPPRESS)
    arguments = parser.parse_args()
    if arguments.case == "mirror-checkpoint-acquisition":
        return mirror_checkpoint_main(arguments.candidate_manifest)
    if arguments.case == "ramp-journal-readiness":
        return ramp_journal_main(arguments.candidate_manifest)
    if not arguments.worker:
        return isolated()
    build = Path(arguments.build_dir).resolve()
    contract = None
    try:
        require(bool(arguments.candidate_manifest), "candidate manifest path is empty")
        candidate = load_candidate(arguments.candidate_manifest)
        for path in ("bin/layerxd", "bin/layerx-genesis-build", "bin/layerx-archive-codec",
                     "tests/relay-archive-sign"):
            require((build / path).is_file() and os.access(build / path, os.X_OK),
                    f"required built executable {path} is absent; run make relay-archive-build")
        contract = Contract(build, candidate)
        contract.run()
        require(len(contract.cases) == 17, f"expected 17 executed cases, ran {len(contract.cases)}")
        result = {"status": "passed", "case": arguments.case, "revision": revision(),
                  "candidate": candidate, "cases": contract.cases, "config_refusals": contract.refusals,
                  "binaries": "built from this checkout; candidate archive bindings are recorded as declared"}
        write_json(contract.work / "result.json", result)
        print(f"RESULT passed case={arguments.case} cases={len(contract.cases)}", flush=True)
        return 0
    except Failure as error:
        print("FAIL " + str(error), flush=True)
        if contract is not None:
            write_json(contract.work / "result.json", {"status": "failed", "case": arguments.case,
                       "revision": revision(), "cases": contract.cases, "failure": str(error)})
        return 1
    finally:
        if contract is not None:
            contract.close()
            print("Evidence: " + str(contract.work), flush=True)


if __name__ == "__main__":
    raise SystemExit(main())
