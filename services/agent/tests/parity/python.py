from __future__ import annotations

import socket
import sys

from layerx_sdk import (
    APPROVAL_DECISION_OUTCOMES,
    APPROVAL_EVENT_KINDS,
    ApiError,
    Client,
    SubmissionFailed,
    SubmissionUnknown,
    VerificationLevel,
    VerifiedRead,
    require_verified,
)


class ParityTransport:
    def __init__(self, path: str) -> None:
        self.path = path

    def call(self, operation: str, request: object) -> object:
        if operation not in {
            "track",
            "approval.list",
            "approval.get",
            "approval.approve",
            "approval.reject",
        } or not isinstance(request, dict):
            raise ValueError("invalid parity request")
        scenario = request.get("scenario")
        if not isinstance(scenario, str):
            raise ValueError("missing parity scenario")
        with socket.socket(socket.AF_UNIX, socket.SOCK_STREAM) as connection:
            connection.connect(self.path)
            connection.sendall(f"python\t{scenario}\n".encode())
            chunks: list[bytes] = []
            while chunk := connection.recv(4096):
                chunks.append(chunk)
        return b"".join(chunks).decode().strip()


def fields(encoded: str) -> dict[str, str]:
    return dict(field.split("=", 1) for field in encoded.split(";"))


def validate(scenario: str, encoded: str) -> None:
    value = fields(encoded)
    if scenario == "unknown_submission":
        if SubmissionUnknown().kind != value["state"]:
            raise RuntimeError("unknown submission collapsed")
    elif scenario == "terminal_rejection":
        state = SubmissionFailed(int(value["result_code"]))
        error = ApiError("CoreRejection", state.protocol_result_code, False, 2, "terminal")
        if state.protocol_result_code != -77_777 or error.error_class != value["error"]:
            raise RuntimeError("terminal result code changed")
    elif scenario == "proven_read":
        read = VerifiedRead(1, VerificationLevel.STATE_PROVEN, 10, "22", "genesis", 10)
        require_verified(VerificationLevel.STATE_PROVEN, read)
        if value["verification"] != "StateProven":
            raise RuntimeError("proven read level changed")
    elif scenario == "availability_failure":
        error = ApiError("UnavailableCapability", None, False, 18, "capability_absent")
        if error.error_class != value["error"]:
            raise RuntimeError("availability error changed")
    elif scenario == "subscription_gap" and value["state"] != "Gap":
        raise RuntimeError("subscription gap hidden")
    elif scenario.startswith("idempotency_"):
        if value["receipt_count"] != "1" or value["economic_effects"] != "1":
            raise RuntimeError("idempotency duplicated an effect")
    elif scenario.startswith("approval_event_"):
        if value["state"] not in APPROVAL_EVENT_KINDS:
            raise RuntimeError("approval event vocabulary diverged")
    elif scenario.startswith("approval_outcome_"):
        if value["state"] not in APPROVAL_DECISION_OUTCOMES:
            raise RuntimeError("approval outcome vocabulary diverged")
    elif scenario.startswith("approval_") and value["state"] not in {
        "approval.list",
        "approval.get",
        "approval.approve",
        "approval.reject",
    }:
        raise RuntimeError("approval operation vocabulary diverged")


def main() -> None:
    if len(sys.argv) != 3:
        raise SystemExit("usage: python.py SOCKET SCENARIOS")
    client = Client(ParityTransport(sys.argv[1]))
    for scenario in sys.argv[2].split(","):
        operation = {
            "approval_list": "approval.list",
            "approval_get": "approval.get",
            "approval_approve": "approval.approve",
            "approval_reject": "approval.reject",
        }.get(scenario, "track")
        encoded = client.call(operation, {"scenario": scenario})  # type: ignore[arg-type]
        if not isinstance(encoded, str):
            raise RuntimeError("invalid parity response")
        validate(scenario, encoded)
        print(f"{scenario}\t{encoded}")


if __name__ == "__main__":
    main()
