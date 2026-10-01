#!/usr/bin/env python3
import sys

sys.dont_write_bytecode = True

import argparse
import hashlib
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
CASES = ("archive-origin-freshness",)
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


def main():
    parser = argparse.ArgumentParser(prog="interop-archive-contract")
    parser.add_argument("--case", required=True, choices=CASES)
    parser.add_argument("--candidate-manifest", required=True)
    parser.add_argument("--build-dir", default=str(ROOT / "build"))
    parser.add_argument("--worker", action="store_true", help=argparse.SUPPRESS)
    arguments = parser.parse_args()
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
