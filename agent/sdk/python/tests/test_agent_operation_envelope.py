import json
import os
import secrets
import ssl
import sys
import unittest
from collections.abc import Callable
from http.client import HTTPSConnection
from pathlib import Path
from urllib.error import HTTPError
from urllib.parse import urlsplit
from urllib.request import HTTPSHandler, Request, build_opener

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

from layerx_sdk.agent_http import (  # noqa: E402
    AgentEnvelopeSuccess,
    AgentEnvelopeTransport,
    AgentSessionCredential,
    LayerXKeyCredential,
)
from layerx_sdk.production import (  # noqa: E402
    IdempotencyKey,
    PlatformSdkError,
    ProductionClient,
    SdkErrorCode,
    SecretBytes,
)

_CASE_ENV = "PAXEER_X_AGENT_ENVELOPE_CASE"
_ROUTE = "/v1/agent/rpc"
_KEYS = frozenset({
    "endpoint", "server_name", "ca_pem", "ca_der", "gateway_api_key_file", "program_bearer_file",
    "credential_file", "requests", "operations", "cases", "phase", "state_file", "response_dir",
    "retry_state_file",
})
_PHASES = frozenset({"read", "pre-restart", "post-restart"})
_REFUSALS = frozenset({
    SdkErrorCode.POLICY_REFUSAL,
    SdkErrorCode.CAPABILITY_REFUSAL,
    SdkErrorCode.BUDGET_REFUSAL,
})


class ProbeRefused(Exception):
    pass


def _absolute_file(case: dict[str, object], name: str) -> Path:
    value = case.get(name)
    if not isinstance(value, str) or not os.path.isabs(value) or not os.path.isfile(value):
        raise ProbeRefused(f"{name} must name an existing absolute file")
    return Path(value)


class Probe:
    def __init__(self, case_path: str) -> None:
        checker = unittest.TestCase()
        self.assertEqual = checker.assertEqual
        self.assertIn = checker.assertIn
        self.assertIsInstance = checker.assertIsInstance
        self.assertIsNotNone = checker.assertIsNotNone
        self.assertTrue = checker.assertTrue
        self.assertRaises = checker.assertRaises
        with open(case_path, "rb") as source:
            case = json.loads(source.read().decode("utf-8"))
        if not isinstance(case, dict) or not set(case) <= _KEYS or not _KEYS - {"state_file", "retry_state_file"} <= set(case):
            raise ProbeRefused("case file keys do not match the frozen protocol")
        if case["phase"] not in _PHASES:
            raise ProbeRefused("unknown phase")
        endpoint = case["endpoint"]
        if not isinstance(endpoint, str) or not endpoint.startswith("https://") or not endpoint.endswith(_ROUTE):
            raise ProbeRefused("endpoint must be the https gateway agent route")
        self.base = endpoint[: -len(_ROUTE)]
        self.endpoint = endpoint
        self.ca_pem = str(_absolute_file(case, "ca_pem"))
        _absolute_file(case, "ca_der")
        key_line = _absolute_file(case, "gateway_api_key_file").read_bytes().decode("ascii")
        if not key_line.endswith("\n") or key_line.count("\n") != 1 or ":" not in key_line:
            raise ProbeRefused("gateway_api_key_file must be one line <key_id>:<secret>")
        self.key_id, secret = key_line[:-1].split(":", 1)
        self.secret = secret.encode("ascii")
        credential = json.loads(_absolute_file(case, "credential_file").read_bytes().decode("utf-8"))
        if not isinstance(credential, dict) or set(credential) != {"tenant", "session_id", "token_id", "generation"}:
            raise ProbeRefused("credential_file must hold exactly tenant, session_id, token_id, generation")
        generation = credential["generation"]
        if not isinstance(generation, str) or not generation.isascii() or not generation.isdigit() or str(int(generation)) != generation:
            raise ProbeRefused("credential generation must be a canonical decimal string")
        self.credential = credential
        requests = case["requests"]
        cases = case["cases"]
        operations = case["operations"]
        if not isinstance(requests, dict) or not isinstance(cases, list) or not cases or any(not isinstance(item, str) for item in cases):
            raise ProbeRefused("requests and cases must be an object and a non-empty array of ids")
        if not isinstance(operations, list) or len(operations) != 50 or operations != sorted(operations):
            raise ProbeRefused("operations must be the 50 sorted catalogue names")
        self.requests = requests
        self.cases = cases
        response_dir = case["response_dir"]
        if not isinstance(response_dir, str) or not os.path.isabs(response_dir) or not os.path.isdir(response_dir):
            raise ProbeRefused("response_dir must name an existing absolute directory")
        self.response_dir = Path(response_dir)
        self.phase = case["phase"]
        state_file = case.get("state_file")
        if self.phase != "read" and (not isinstance(state_file, str) or not os.path.isabs(state_file)):
            raise ProbeRefused("state_file must name an absolute path for restart phases")
        self.state_file = None if state_file is None else Path(str(state_file))
        retry_state_file = case.get("retry_state_file")
        if retry_state_file is not None and (not isinstance(retry_state_file, str) or not os.path.isabs(retry_state_file)):
            raise ProbeRefused("retry_state_file must name an absolute path")
        self.retry_state_file = None if retry_state_file is None else Path(retry_state_file)

    def _key(self) -> LayerXKeyCredential:
        return LayerXKeyCredential(self.key_id, SecretBytes(self.secret))

    def _session(self, **override: object) -> AgentSessionCredential:
        coordinates: dict[str, object] = {
            "tenant": self.credential["tenant"],
            "session_id": self.credential["session_id"],
            "token_id": self.credential["token_id"],
            "generation": int(self.credential["generation"]),
        }
        coordinates.update(override)
        return AgentSessionCredential(**coordinates)  # type: ignore[arg-type]

    def _client(self, session: AgentSessionCredential | None = None) -> ProductionClient:
        return ProductionClient(AgentEnvelopeTransport(
            self.base,
            gateway_key=self._key(),
            session=session if session is not None else self._session(),
            ca_file=self.ca_pem,
        ))

    def _request(self, case_id: str) -> dict[str, object]:
        request = self.requests.get(case_id)
        if not isinstance(request, dict) or not {"operation", "request"} <= set(request) <= {"operation", "request", "idempotency_key"}:
            raise ProbeRefused(f"requests has no valid entry for {case_id}")
        if not isinstance(request["operation"], str) or not isinstance(request["request"], dict):
            raise ProbeRefused(f"requests entry for {case_id} is malformed")
        return request

    def _record(self, case_id: str, status: int, body: object) -> None:
        (self.response_dir / f"{case_id}.json").write_text(json.dumps({"status": status, "body": body}))

    def _success(self, case_id: str, entry: str | None = None) -> AgentEnvelopeSuccess:
        request = self._request(entry or case_id)
        key = request.get("idempotency_key")
        result = self._client().agent(
            request["operation"],  # type: ignore[arg-type]
            request["request"],
            idempotency_key=None if key is None else IdempotencyKey(str(key)),
        )
        self.assertIsInstance(result, AgentEnvelopeSuccess)
        assert isinstance(result, AgentEnvelopeSuccess)
        self.assertTrue(result.request_id.isdigit())
        self.assertIn(result.verification_status.get("state"), {"achieved", "unverified"})
        self._record(case_id, 200, {
            "request_id": result.request_id,
            "value": result.value,
            "verification_status": dict(result.verification_status),
        })
        return result

    def _refusal(self, case_id: str, session: AgentSessionCredential) -> None:
        request = self._request("read")
        with self.assertRaises(PlatformSdkError) as raised:
            self._client(session).agent(request["operation"], request["request"])  # type: ignore[arg-type]
        self.assertIn(raised.exception.code, _REFUSALS)
        self.assertIsNotNone(raised.exception.request_id)
        self._record(case_id, 0, raised.exception.to_dict())

    def _raw(self, case_id: str, body: bytes) -> dict[str, object]:
        context = ssl.create_default_context(cafile=self.ca_pem)
        request = Request(
            self.endpoint,
            data=body,
            method="POST",
            headers={
                "Accept": "application/json",
                "Content-Type": "application/json",
                "Content-Length": str(len(body)),
                "Authorization": self._key().use(),
            },
        )
        try:
            with build_opener(HTTPSHandler(context=context)).open(request, timeout=30) as response:
                status, parsed = response.status, json.loads(response.read())
        except HTTPError as error:
            try:
                status, parsed = error.code, json.loads(error.read())
            finally:
                error.close()
        self._record(case_id, status, parsed)
        return {"status": status, "body": parsed}

    def _raw_envelope(self, **override: object) -> bytes:
        request = self._request("read")
        envelope: dict[str, object] = {
            "version": 1,
            "request_id": "7",
            "operation": request["operation"],
            "request": request["request"],
            "credential": dict(self.credential),
            "idempotency_key": None,
        }
        envelope.update(override)
        return json.dumps(envelope).encode()

    def _raw_refusal(self, case_id: str, body: bytes, status: int, reason: str, request_id: str | None = None) -> None:
        raw = self._raw(case_id, body)
        error = raw["body"]
        assert isinstance(error, dict)
        self.assertEqual((raw["status"], error.get("class"), error.get("reason")), (status, "ProtocolIncompatibility", reason))
        if request_id is not None:
            self.assertEqual(error.get("request_id"), request_id)

    def case_read(self) -> None:
        self._success("read")

    def case_program_read(self) -> None:
        self._success("program_read")

    def case_approval_list(self) -> None:
        self._success("approval_list")

    def case_allowed_mutation(self) -> None:
        self._success("allowed_mutation")

    def case_mutation_duplicate_same_result(self) -> None:
        first = self._success("mutation_duplicate_same_result", "allowed_mutation")
        second = self._success("mutation_duplicate_same_result", "allowed_mutation")
        self.assertEqual(first.value, second.value)
        self.assertEqual(first.verification_status, second.verification_status)

    def _restart_envelope(self, state: dict[str, object]) -> bytes:
        return json.dumps({
            "version": 1,
            "request_id": state["request_id"],
            "operation": state["operation"],
            "request": state["request"],
            "credential": dict(self.credential),
            "idempotency_key": state["idempotency_key"],
        }, ensure_ascii=True, separators=(",", ":")).encode()

    def case_restart_unknown_pending(self) -> None:
        if self.phase != "pre-restart" or self.state_file is None:
            raise ProbeRefused("restart_unknown_pending needs the pre-restart phase")
        request = self._request("allowed_mutation")
        key = request.get("idempotency_key")
        if not isinstance(key, str):
            raise ProbeRefused("allowed_mutation carries no idempotency_key")
        state: dict[str, object] = {
            "request_id": str(secrets.randbits(64)),
            "idempotency_key": key,
            "operation": request["operation"],
            "request": request["request"],
        }
        body = self._restart_envelope(state)
        parsed = urlsplit(self.endpoint)
        connection = HTTPSConnection(str(parsed.hostname), parsed.port or 443, context=ssl.create_default_context(cafile=self.ca_pem), timeout=30)
        try:
            connection.request("POST", parsed.path, body=body, headers={
                "Accept": "application/json",
                "Content-Type": "application/json",
                "Content-Length": str(len(body)),
                "Authorization": self._key().use(),
            })
        finally:
            connection.close()
        self.state_file.write_text(json.dumps(state, sort_keys=True))
        self._record("restart_unknown_pending", 0, state)

    def case_restart_unknown_reconcile(self) -> None:
        if self.phase != "post-restart" or self.state_file is None:
            raise ProbeRefused("restart_unknown_reconcile needs the post-restart phase")
        if not self.state_file.is_file():
            raise ProbeRefused("post-restart phase found no pre-restart state_file")
        state = json.loads(self.state_file.read_text())
        if not isinstance(state, dict) or set(state) != {"request_id", "idempotency_key", "operation", "request"}:
            raise ProbeRefused("state_file is not the restart_unknown_pending record")
        first = self._raw("restart_unknown_reconcile", self._restart_envelope(state))
        second = self._raw("restart_unknown_reconcile", self._restart_envelope(state))
        for raw in (first, second):
            body = raw["body"]
            assert isinstance(body, dict)
            self.assertEqual(raw["status"], 200)
            self.assertEqual(set(body), {"request_id", "value", "verification_status"})
            self.assertEqual(body["request_id"], state["request_id"])
            self.assertIn(body["verification_status"].get("state"), {"achieved", "unverified"})
        self.assertEqual(first["body"]["value"], second["body"]["value"])
        self.assertEqual(first["body"]["verification_status"], second["body"]["verification_status"])

    def case_restart_retry_same_result(self) -> None:
        if self.retry_state_file is None:
            raise ProbeRefused("restart_retry_same_result needs retry_state_file in the case file")
        if self.phase == "read":
            raise ProbeRefused("restart_retry_same_result needs the pre-restart or post-restart phase")
        result = self._success("restart_retry_same_result", "allowed_mutation")
        observed = {"value": result.value, "verification_status": dict(result.verification_status)}
        if self.phase == "pre-restart":
            self.retry_state_file.write_text(json.dumps(observed, sort_keys=True))
            return
        if not self.retry_state_file.is_file():
            raise ProbeRefused("post-restart phase found no pre-restart retry_state_file")
        self.assertEqual(json.loads(self.retry_state_file.read_text()), json.loads(json.dumps(observed, sort_keys=True)))

    def case_missing_idempotency_key(self) -> None:
        request = self._request("allowed_mutation")
        with self.assertRaises(PlatformSdkError) as raised:
            self._client().agent(request["operation"], request["request"])  # type: ignore[arg-type]
        self.assertEqual(raised.exception.code, SdkErrorCode.IDEMPOTENCY_REQUIRED)
        self._record("missing_idempotency_key", 0, raised.exception.to_dict())

    def case_wrong_generation(self) -> None:
        generation = int(self.credential["generation"])
        self._refusal("wrong_generation", self._session(generation=generation + 1 if generation < (1 << 64) - 1 else generation - 1))

    def case_wrong_tenant(self) -> None:
        tenant = str(self.credential["tenant"])
        self._refusal("wrong_tenant", self._session(tenant=tenant[:-1] + ("a" if tenant[-1] != "a" else "b")))

    def case_wrong_session(self) -> None:
        session_id = str(self.credential["session_id"])
        self._refusal("wrong_session", self._session(session_id=("1" if session_id[0] == "0" else "0") + session_id[1:]))

    def case_wrong_token(self) -> None:
        token_id = str(self.credential["token_id"])
        self._refusal("wrong_token", self._session(token_id=("1" if token_id[0] == "0" else "0") + token_id[1:]))

    def case_faucet_retired(self) -> None:
        with self.assertRaises(PlatformSdkError) as raised:
            self._client().agent("faucet.claim", {})
        self.assertEqual(raised.exception.code, SdkErrorCode.UNAVAILABLE_CAPABILITY)
        self.assertEqual(raised.exception.retry, "never")
        self.assertIsNotNone(raised.exception.request_id)
        self._record("faucet_retired", 0, raised.exception.to_dict())

    def case_unknown_operation(self) -> None:
        with self.assertRaises(PlatformSdkError) as raised:
            self._client().agent("faucet.unknown", {})  # type: ignore[arg-type]
        self.assertEqual(raised.exception.code, SdkErrorCode.UNAVAILABLE_CAPABILITY)
        self._raw_refusal("unknown_operation", self._raw_envelope(operation="faucet.unknown"), 404, "envelope.unknown_operation")

    def case_malformed_body(self) -> None:
        self._raw_refusal("malformed_body", b'{"version":1,', 400, "envelope.malformed", "0")

    def case_unknown_field(self) -> None:
        self._raw_refusal("unknown_field", self._raw_envelope(principal="forged"), 400, "envelope.unknown_field")

    def case_bad_version(self) -> None:
        self._raw_refusal("bad_version", self._raw_envelope(version=2), 400, "envelope.version")

    def case_noncanonical_integer(self) -> None:
        credential = dict(self.credential)
        credential["generation"] = "0" + str(credential["generation"])
        self._raw_refusal("noncanonical_integer", self._raw_envelope(credential=credential), 400, "envelope.noncanonical_integer")


def main() -> int:
    path = os.environ.get(_CASE_ENV)
    if not path or not os.path.isabs(path) or not os.path.isfile(path):
        print(f"agent operation envelope probe refused: {_CASE_ENV} must name an absolute case file", file=sys.stderr)
        return 2
    try:
        probe = Probe(path)
    except (ProbeRefused, OSError, ValueError, UnicodeDecodeError) as refusal:
        print(f"agent operation envelope probe refused: {refusal}", file=sys.stderr)
        return 2
    passed = 0
    failed = False
    for case_id in probe.cases:
        handler: Callable[[], None] | None = getattr(probe, "case_" + case_id, None) if case_id.isidentifier() else None
        if handler is None:
            print(f"agent operation envelope probe: case {case_id} is not implemented by the Python probe", file=sys.stderr)
            failed = True
            continue
        try:
            handler()
        except (AssertionError, PlatformSdkError, ProbeRefused, OSError, ValueError) as failure:
            print(f"agent operation envelope probe: case {case_id} failed: {type(failure).__name__}: {failure}", file=sys.stderr)
            failed = True
            continue
        passed += 1
        print(f"PAXEER_X_AGENT_ENVELOPE_CASE {case_id} passed", flush=True)
    print(f"PAXEER_X_AGENT_ENVELOPE_CASES={passed}", flush=True)
    return 1 if failed else 0


if __name__ == "__main__":
    sys.exit(main())
