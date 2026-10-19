// layerx:begin integration
import { LayerXPaymentHttpTransport } from "@sidiora/layerx-buyer-middleware";
import { ProductionClient, SecretBytes, idempotencyKey } from "@sidiora/layerx-sdk";
export const openLayerX = (apiUrl, apiToken) => new ProductionClient(
  new LayerXPaymentHttpTransport({ baseUrl: apiUrl, bearerToken: new SecretBytes(new TextEncoder().encode(apiToken)) }));
export const pay = async (layerx, source, destination, money, paymentKey) => {
  const quote = await layerx.human("move.quote", { source, destination, money });
  return layerx.human("move.commit", { quote_id: quote.quote_id }, { idempotencyKey: idempotencyKey(paymentKey) });
};
// layerx:end integration

const required = (name) => {
  const value = process.env[name];
  if (value === undefined || value.length === 0) throw new Error(`missing_${name.toLowerCase()}`);
  return value;
};

const settled = new Set(["done", "done-finalised", "refused"]);

const layerx = openLayerX(required("LAYERX_API_URL"), required("LAYERX_API_TOKEN"));
const started = await pay(
  layerx,
  required("LAYERX_SOURCE"),
  required("LAYERX_DESTINATION"),
  { amount: required("LAYERX_AMOUNT"), currency: required("LAYERX_CURRENCY") },
  required("LAYERX_PAYMENT_KEY"),
);

let journey = started;
for (let attempt = 0; attempt < 40 && !settled.has(journey.state); attempt += 1) {
  await new Promise((resolve) => setTimeout(resolve, 250));
  journey = await layerx.human("journey.get", { journey_id: journey.journey_id });
}

const receipts = journey.evidence.filter((reference) => reference.class === "layerx-receipt");
process.stdout.write(JSON.stringify({
  journey: journey.journey_id,
  state: journey.state,
  receipts: receipts.map((reference) => ({ evidence: reference.evidence_id, verification: reference.verification })),
  ...(journey.refusal === undefined ? {} : { refused_by: journey.refusal.refused_by, money_left: journey.refusal.money_left }),
}) + "\n");
if (journey.state !== "done" && journey.state !== "done-finalised") process.exitCode = 2;
