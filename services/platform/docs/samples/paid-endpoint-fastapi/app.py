from __future__ import annotations

import json
from base64 import b64decode, b64encode
from os import environ, fsync
from pathlib import Path
from typing import Awaitable, Callable, Mapping

from fastapi import FastAPI, Request
from fastapi.responses import JSONResponse

from layerx_fastapi import (
    JsonValue,
    LayerXResource,
    MiddlewareError,
    MiddlewareErrorCode,
    ProposedFulfillment,
    StoredFulfillment,
)


# layerx:begin storage
class FileFulfillmentRepository:
    def __init__(self, directory: Path) -> None:
        self.directory = directory

    async def fulfill(
        self,
        proposed: ProposedFulfillment,
        release: Callable[[], Awaitable[LayerXResource]],
    ) -> StoredFulfillment[LayerXResource]:
        self.directory.mkdir(parents=True, exist_ok=True, mode=0o700)
        path = self.directory / f"{proposed.idempotency_key}.json"
        if path.exists():
            return self.stored(path, proposed)
        resource = await release()
        record = json.dumps(
            {
                "requestDigest": proposed.request_digest,
                "receipt": b64encode(proposed.canonical_receipt).decode("ascii"),
                "contentType": resource.content_type,
                "body": b64encode(resource.body).decode("ascii"),
            }
        ).encode("utf-8")
        try:
            descriptor = path.open("xb")
        except FileExistsError:
            return self.stored(path, proposed)
        with descriptor as handle:
            handle.write(record)
            handle.flush()
            fsync(handle.fileno())
        return StoredFulfillment(
            idempotency_key=proposed.idempotency_key,
            request_digest=proposed.request_digest,
            canonical_receipt=proposed.canonical_receipt,
            authorized_batch=proposed.authorized_batch,
            resource=resource,
        )

    def stored(self, path: Path, proposed: ProposedFulfillment) -> StoredFulfillment[LayerXResource]:
        record = json.loads(path.read_text(encoding="utf-8"))
        if record["requestDigest"] != proposed.request_digest:
            raise MiddlewareError(MiddlewareErrorCode.FULFILLMENT_CONFLICT)
        return StoredFulfillment(
            idempotency_key=proposed.idempotency_key,
            request_digest=proposed.request_digest,
            canonical_receipt=b64decode(record["receipt"], validate=True),
            authorized_batch=proposed.authorized_batch,
            resource=LayerXResource(
                content_type=record["contentType"],
                body=b64decode(record["body"], validate=True),
            ),
        )
# layerx:end storage


class ReleaseReport:
    def __init__(self, body: bytes) -> None:
        self.body = body

    async def release(self, request: Request) -> LayerXResource:
        return LayerXResource(content_type="application/json", body=self.body)


class SettlementLog:
    def __init__(self) -> None:
        self.settlements: list[dict[str, JsonValue]] = []

    async def handle(self, event: Mapping[str, JsonValue], delivery_id: str) -> None:
        self.settlements.append({"deliveryId": delivery_id, "event": dict(event)})


report_body = Path(environ.get("LAYERX_RESOURCE_FILE", "./resource.json")).read_bytes()
fulfillment_directory = Path(environ.get("LAYERX_FULFILLMENT_DIR", "./fulfillments"))
settlements = SettlementLog()
app = FastAPI(title="Paid API", openapi_url=None)

# layerx:begin integration
from layerx_fastapi import LayerXMountOptions, SingleProcessWebhookDeliveryStore, mount_layerx

mount = mount_layerx(app, LayerXMountOptions(
    resources=ReleaseReport(report_body),
    fulfillments=FileFulfillmentRepository(fulfillment_directory),
    deliveries=SingleProcessWebhookDeliveryStore(),
    events=settlements,
))
# layerx:end integration


@app.get("/settlements")
async def settled() -> JSONResponse:
    return JSONResponse({"settlements": settlements.settlements})


@app.get("/mount")
async def described() -> JSONResponse:
    return JSONResponse(
        {
            "resource": mount.config.protected_path,
            "webhook": mount.config.webhook.path,
            "price": mount.runtime.requirements.amount,
            "asset": mount.runtime.requirements.asset,
        }
    )
