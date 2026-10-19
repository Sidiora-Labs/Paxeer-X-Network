import { readFile } from "node:fs/promises";
import express from "express";
import { FileFulfillmentRepository } from "./fulfillments.mjs";

const app = express();
const reportBody = await readFile(process.env.LAYERX_RESOURCE_FILE ?? "./resource.json", "utf8");
const fulfillmentDirectory = process.env.LAYERX_FULFILLMENT_DIR ?? "./fulfillments";
const settlements = [];

// layerx:begin integration
import { SingleProcessWebhookDeliveryStore, mountLayerX } from "@sidiora/layerx-express";
const layerx = mountLayerX(app, {
  environment: process.env,
  resources: { release: async () => ({ contentType: "application/json", body: reportBody }) },
  fulfillments: new FileFulfillmentRepository(fulfillmentDirectory),
  deliveries: new SingleProcessWebhookDeliveryStore(),
  events: { handle: async (event, deliveryId) => { settlements.push({ deliveryId, event }); } },
});
// layerx:end integration

app.get("/settlements", (request, response) => {
  response.status(200).set("content-type", "application/json").send(JSON.stringify({ settlements }));
});

const port = Number(process.env.PORT ?? "8080");
if (!Number.isSafeInteger(port) || port <= 0 || port > 65535) {
  throw new Error("paid-endpoint-express: invalid PORT");
}

const server = app.listen(port, "127.0.0.1", () => {
  process.stdout.write(`${JSON.stringify({
    listening: port,
    paid: layerx.config.protectedPath,
    webhook: layerx.config.webhook.path,
  })}\n`);
});

for (const signal of ["SIGINT", "SIGTERM"]) {
  process.on(signal, () => {
    layerx.destroy();
    server.close(() => process.exit(0));
  });
}
