from dataclasses import dataclass
from typing import Literal

from layerx_sdk import Client, IdempotentMutation


@dataclass(frozen=True)
class BudgetRequest:
    limit: int
    enforcement: Literal["ProtocolBudget", "DaemonLimit"]


def create_budget(client: Client, request: IdempotentMutation[BudgetRequest]) -> object:
    if request.operation.limit <= 0:
        raise ValueError("budget_limit")
    return client.call("budget.create", request)
