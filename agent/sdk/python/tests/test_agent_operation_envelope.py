import json
import os
import ssl
import unittest
from urllib.error import HTTPError
from urllib.request import HTTPSHandler, Request, build_opener

from layerx_sdk.agent_http import (
    AgentEnvelopeSuccess,
    AgentEnvelopeTransport,
    AgentSessionCredential,
    LayerXKeyCredential,
)
from layerx_sdk.production import (
    IdempotencyKey,
    PlatformSdkError,
    ProductionClient,
    SdkErrorCode,
    SecretBytes,
)

_REQUIRED = (
    "LAYERX_AGENT_ENVELOPE_ENDPOINT",
    "LAYERX_AGENT_ENVELOPE_CA_FILE",
    "LAYERX_AGENT_ENVELOPE_KEY_ID",
    "LAYERX_AGENT_ENVELOPE_KEY_SECRET",
    "LAYERX_AGENT_ENVELOPE_TENANT",
    "LAYERX_AGENT_ENVELOPE_SESSION_ID",
    "LAYERX_AGENT_ENVELOPE_TOKEN_ID",
    "LAYERX_AGENT_ENVELOPE_GENERATION",
    "LAYERX_AGENT_ENVELOPE_CASES",
)
_CASES = ("read_account", "program_read", "approval_list", "allowed_mutation")
_REFUSALS = frozenset({
    SdkErrorCode.POLICY_REFUSAL,
    SdkErrorCode.CAPABILITY_REFUSAL,
    SdkErrorCode.BUDGET_REFUSAL,
})


def _environment() -> dict[str, str]:
    missing = [name for name in _REQUIRED if not os.environ.get(name)]
    if missing:
        raise AssertionError("agent operation envelope probe refused: missing " + ", ".join(missing))
    return {name: os.environ[name] for name in _REQUIRED}


def _cases(path: str) -> dict[str, dict[str, object]]:
    with open(path, "rb") as source:
        cases = json.loads(source.read().decode("utf-8"))
    if not isinstance(cases, dict) or set(cases) != set(_CASES):
        raise AssertionError("agent operation envelope probe refused: cases file must name exactly " + ", ".join(_CASES))
    for name, case in cases.items():
        expected = {"operation", "request", "idempotency_key"} if name == "allowed_mutation" else {"operation", "request"}
        if not isinstance(case, dict) or set(case) != expected or not isinstance(case["operation"], str) or not isinstance(case["request"], dict):
            raise AssertionError(f"agent operation envelope probe refused: malformed case {name}")
    return cases


class AgentOperationEnvelopeProbe(unittest.TestCase):
    env: dict[str, str]
    cases: dict[str, dict[str, object]]

    @classmethod
    def setUpClass(cls) -> None:
        cls.env = _environment()
        cls.cases = _cases(cls.env["LAYERX_AGENT_ENVELOPE_CASES"])

    def _key(self) -> LayerXKeyCredential:
        return LayerXKeyCredential(
            self.env["LAYERX_AGENT_ENVELOPE_KEY_ID"],
            SecretBytes(self.env["LAYERX_AGENT_ENVELOPE_KEY_SECRET"].encode("ascii")),
        )

    def _session(self, **override: object) -> AgentSessionCredential:
        coordinates: dict[str, object] = {
            "tenant": self.env["LAYERX_AGENT_ENVELOPE_TENANT"],
            "session_id": self.env["LAYERX_AGENT_ENVELOPE_SESSION_ID"],
            "token_id": self.env["LAYERX_AGENT_ENVELOPE_TOKEN_ID"],
            "generation": int(self.env["LAYERX_AGENT_ENVELOPE_GENERATION"]),
        }
        coordinates.update(override)
        return AgentSessionCredential(**coordinates)  # type: ignore[arg-type]

    def _client(self, session: AgentSessionCredential | None = None) -> ProductionClient:
        return ProductionClient(AgentEnvelopeTransport(
            self.env["LAYERX_AGENT_ENVELOPE_ENDPOINT"],
            gateway_key=self._key(),
            session=session if session is not None else self._session(),
            ca_file=self.env["LAYERX_AGENT_ENVELOPE_CA_FILE"],
        ))

    def _success(self, name: str, client: ProductionClient | None = None) -> AgentEnvelopeSuccess:
        case = self.cases[name]
        key = case.get("idempotency_key")
        result = (client or self._client()).agent(
            case["operation"],  # type: ignore[arg-type]
            case["request"],
            idempotency_key=None if key is None else IdempotencyKey(str(key)),
        )
        self.assertIsInstance(result, AgentEnvelopeSuccess)
        assert isinstance(result, AgentEnvelopeSuccess)
        self.assertTrue(result.request_id.isdigit())
        self.assertIn(result.verification_status.get("state"), {"achieved", "unverified"})
        return result

    def _refusal(self, name: str, session: AgentSessionCredential) -> PlatformSdkError:
        case = self.cases[name]
        with self.assertRaises(PlatformSdkError) as raised:
            self._client(session).agent(case["operation"], case["request"])  # type: ignore[arg-type]
        self.assertIn(raised.exception.code, _REFUSALS)
        self.assertIsNotNone(raised.exception.request_id)
        return raised.exception

    def _raw(self, body: bytes) -> tuple[int, dict[str, object]]:
        context = ssl.create_default_context(cafile=self.env["LAYERX_AGENT_ENVELOPE_CA_FILE"])
        request = Request(
            self.env["LAYERX_AGENT_ENVELOPE_ENDPOINT"].rstrip("/") + "/v1/agent/rpc",
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
                return response.status, json.loads(response.read())
        except HTTPError as error:
            try:
                return error.code, json.loads(error.read())
            finally:
                error.close()

    def _raw_envelope(self, **override: object) -> dict[str, object]:
        case = self.cases["read_account"]
        envelope: dict[str, object] = {
            "version": 1,
            "request_id": "7",
            "operation": case["operation"],
            "request": case["request"],
            "credential": self._session().coordinates(),
            "idempotency_key": None,
        }
        envelope.update(override)
        return envelope

    def test_sdk_python_read(self) -> None:
        self._success("read_account")

    def test_program_read(self) -> None:
        self._success("program_read")

    def test_approval_list(self) -> None:
        self._success("approval_list")

    def test_allowed_mutation_and_duplicate_same_result(self) -> None:
        first = self._success("allowed_mutation")
        second = self._success("allowed_mutation")
        self.assertEqual(first.value, second.value)
        self.assertEqual(first.verification_status, second.verification_status)

    def test_mutation_requires_idempotency_key(self) -> None:
        case = self.cases["allowed_mutation"]
        with self.assertRaises(PlatformSdkError) as raised:
            self._client().agent(case["operation"], case["request"])  # type: ignore[arg-type]
        self.assertEqual(raised.exception.code, SdkErrorCode.IDEMPOTENCY_REQUIRED)

    def test_wrong_generation(self) -> None:
        generation = int(self.env["LAYERX_AGENT_ENVELOPE_GENERATION"])
        self._refusal("read_account", self._session(generation=generation + 1 if generation < (1 << 64) - 1 else generation - 1))

    def test_wrong_tenant(self) -> None:
        self._refusal("read_account", self._session(tenant=self.env["LAYERX_AGENT_ENVELOPE_TENANT"] + ".other"))

    def test_wrong_session(self) -> None:
        session_id = self.env["LAYERX_AGENT_ENVELOPE_SESSION_ID"]
        self._refusal("read_account", self._session(session_id=("1" if session_id[0] == "0" else "0") + session_id[1:]))

    def test_wrong_token(self) -> None:
        token_id = self.env["LAYERX_AGENT_ENVELOPE_TOKEN_ID"]
        self._refusal("read_account", self._session(token_id=("1" if token_id[0] == "0" else "0") + token_id[1:]))

    def test_faucet_retired(self) -> None:
        with self.assertRaises(PlatformSdkError) as raised:
            self._client().agent("faucet.claim", {})
        self.assertEqual(raised.exception.code, SdkErrorCode.UNAVAILABLE_CAPABILITY)
        self.assertEqual(raised.exception.retry, "never")
        self.assertIsNotNone(raised.exception.request_id)

    def test_unknown_operation_refused_by_sdk_and_daemon(self) -> None:
        with self.assertRaises(PlatformSdkError) as raised:
            self._client().agent("faucet.unknown", {})  # type: ignore[arg-type]
        self.assertEqual(raised.exception.code, SdkErrorCode.UNAVAILABLE_CAPABILITY)
        status, error = self._raw(json.dumps(self._raw_envelope(operation="faucet.unknown")).encode())
        self.assertEqual((status, error.get("class"), error.get("reason")), (404, "ProtocolIncompatibility", "envelope.unknown_operation"))

    def test_malformed_body(self) -> None:
        status, error = self._raw(b'{"version":1,')
        self.assertEqual((status, error.get("class"), error.get("reason"), error.get("request_id")), (400, "ProtocolIncompatibility", "envelope.malformed", "0"))

    def test_unknown_field(self) -> None:
        status, error = self._raw(json.dumps(self._raw_envelope(principal="forged")).encode())
        self.assertEqual((status, error.get("class"), error.get("reason")), (400, "ProtocolIncompatibility", "envelope.unknown_field"))

    def test_bad_version(self) -> None:
        status, error = self._raw(json.dumps(self._raw_envelope(version=2)).encode())
        self.assertEqual((status, error.get("class"), error.get("reason")), (400, "ProtocolIncompatibility", "envelope.version"))

    def test_noncanonical_generation(self) -> None:
        credential = dict(self._session().coordinates())
        credential["generation"] = "0" + credential["generation"]
        status, error = self._raw(json.dumps(self._raw_envelope(credential=credential)).encode())
        self.assertEqual((status, error.get("class"), error.get("reason")), (400, "ProtocolIncompatibility", "envelope.noncanonical_integer"))


if __name__ == "__main__":
    unittest.main()
