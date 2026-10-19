#!/usr/bin/env python3
from __future__ import annotations

import json
import os
import sys
import time

# layerx:begin integration
from layerx_sdk import IdempotencyKey, ProductionClient
from layerx_transport import HumanApiTransport

def open_layerx(api_url: str, api_token: str) -> ProductionClient:
    return ProductionClient(HumanApiTransport(api_url, api_token))

def pay(layerx, source, destination, money, payment_key):
    quote = layerx.human("move.quote", {"source": source, "destination": destination, "money": money})
    return layerx.human("move.commit", {"quote_id": quote["quote_id"]}, idempotency_key=IdempotencyKey(payment_key))
# layerx:end integration

SETTLED = frozenset({"done", "done-finalised", "refused"})
COMPLETED = frozenset({"done", "done-finalised"})


def required(name: str) -> str:
    value = os.environ.get(name)
    if not value:
        raise SystemExit(f"first-payment-python: missing {name}")
    return value


def main() -> int:
    layerx = open_layerx(required("LAYERX_API_URL"), required("LAYERX_API_TOKEN"))
    journey = pay(
        layerx,
        required("LAYERX_SOURCE"),
        required("LAYERX_DESTINATION"),
        {"amount": required("LAYERX_AMOUNT"), "currency": required("LAYERX_CURRENCY")},
        required("LAYERX_PAYMENT_KEY"),
    )
    for _ in range(40):
        if journey["state"] in SETTLED:
            break
        time.sleep(0.25)
        journey = layerx.human("journey.get", {"journey_id": journey["journey_id"]})
    report: dict[str, object] = {
        "journey_id": journey["journey_id"],
        "state": journey["state"],
        "receipts": [
            evidence["evidence_id"]
            for evidence in journey["evidence"]
            if evidence["class"] == "layerx-receipt"
        ],
    }
    refusal = journey.get("refusal")
    if refusal is not None:
        report["refused_by"] = refusal["refused_by"]
        report["money_left"] = refusal["money_left"]
    json.dump(report, sys.stdout, sort_keys=True)
    sys.stdout.write("\n")
    return 0 if journey["state"] in COMPLETED else 2


if __name__ == "__main__":
    raise SystemExit(main())
