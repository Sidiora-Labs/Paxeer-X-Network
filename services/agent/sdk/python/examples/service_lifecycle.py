from dataclasses import dataclass
from typing import Literal

from layerx_sdk import Client, IdempotentMutation, SubmissionState


@dataclass(frozen=True)
class ServiceActivity:
    stage: Literal["commit", "deliver", "accept"]
    canonical_bytes: bytes


def service_lifecycle(
    client: Client,
    activities: list[IdempotentMutation[ServiceActivity]],
) -> list[SubmissionState]:
    observed: list[SubmissionState] = []
    for activity in activities:
        result = client.call("submit", activity)
        if not isinstance(result, SubmissionState.__args__):
            raise TypeError("invalid_submission_state")
        observed.append(result)
    return observed
