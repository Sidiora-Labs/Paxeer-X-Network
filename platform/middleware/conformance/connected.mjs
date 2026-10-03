import assert from "node:assert/strict";
import { spawn } from "node:child_process";
import { createHash } from "node:crypto";
import { constants } from "node:fs";
import { open, realpath } from "node:fs/promises";
import { isAbsolute, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { authenticatedAgentTransport, loadAgentServiceProviders } from "@sidiora/layerx-agent-integrations";
import { PlatformSdkError, idempotencyKey } from "@sidiora/layerx-sdk";
import { decodePaymentRequiredHeader, decodeSettlementHeader, encodePaymentPayloadHeader, verifyPaymentReceipt } from "@sidiora/layerx-seller-middleware";

const agentEntry = fileURLToPath(new URL("../../examples/agent-spend/index.mjs", import.meta.url));
const merchantEntry = fileURLToPath(new URL("../../examples/merchant-checkout/index.mjs", import.meta.url));
const servicesEntry = fileURLToPath(new URL("../../examples/agent-spend/services.mjs", import.meta.url));
const deadline = Date.now() + 1200000;
let tests = 0;
let phase = "protected inputs";
const check = (condition, name) => { assert.ok(condition, name); tests += 1; process.stdout.write(`ok connected: ${name}\n`); };
const remaining = () => {
  const value = deadline - Date.now();
  assert.ok(value > 0, "connected acceptance deadline");
  return Math.min(value, 30000);
};
const object = (value) => { assert.ok(value && typeof value === "object" && !Array.isArray(value), "object required"); return value; };

async function privateJson(path) {
  assert.ok(typeof path === "string" && isAbsolute(path) && resolve(path) === path && await realpath(path) === path, "canonical private fixture path");
  const file = await open(path, constants.O_RDONLY | constants.O_NOFOLLOW);
  try {
    const stat = await file.stat();
    assert.ok(stat.isFile() && stat.nlink === 1 && stat.uid === process.getuid() && (stat.mode & 0o077) === 0 && stat.size <= 1048576, "protected fixture input");
    return object(JSON.parse(await file.readFile("utf8")));
  } finally { await file.close(); }
}

function endpoint(value, loopback = false) {
  const url = new URL(value);
  const local = ["127.0.0.1", "localhost", "[::1]"].includes(url.hostname);
  assert.ok(!url.username && !url.password && !url.search && !url.hash
    && (url.protocol === "https:" || url.protocol === "http:" && local) && (!loopback || local), "declared secure service endpoint");
  return url;
}

async function example(action, request, additional = {}) {
  const child = spawn(process.execPath, [agentEntry, action], {
    env: { ...process.env, LAYERX_AGENT_SERVICES_MODULE: servicesEntry, LAYERX_SPEND_REQUEST_JSON: JSON.stringify(request), ...additional },
    stdio: ["ignore", "pipe", "pipe"],
  });
  return new Promise((accept, reject) => {
    let output = "", bytes = 0, settled = false;
    const finish = (error, result) => {
      if (settled) return;
      settled = true; clearTimeout(timer);
      if (error) { child.kill("SIGKILL"); reject(error); } else accept(result);
    };
    const timer = setTimeout(() => finish(new Error("agent_example_deadline")), remaining());
    child.stdout.on("data", (chunk) => {
      bytes += chunk.length;
      if (bytes > 4194304) finish(new Error("agent_example_output_bound")); else output += chunk;
    });
    child.stderr.on("data", (chunk) => { bytes += chunk.length; if (bytes > 4194304) finish(new Error("agent_example_output_bound")); });
    child.on("error", (error) => finish(error));
    child.on("close", (code) => {
      if (code !== 0) return finish(new Error(`agent_example_${action}_exit_${code}`));
      try { finish(undefined, object(JSON.parse(output.trim().split("\n").at(-1)))); }
      catch { finish(new Error("agent_example_invalid_result")); }
    });
  });
}

async function post(base, path, body, headers = {}) {
  const response = await fetch(new URL(path, base), {
    method: "POST", headers: { "content-type": "application/json", ...headers },
    body: typeof body === "string" ? body : JSON.stringify(body), signal: AbortSignal.timeout(remaining()), redirect: "error",
  });
  const text = await response.text();
  assert.ok(Buffer.byteLength(text) <= 1048576, "merchant response bound");
  return { response, value: object(JSON.parse(text)) };
}

async function main() {
  const fixture = await privateJson(process.env.LAYERX_CONNECTED_MIDDLEWARE_FIXTURE);
  assert.equal(fixture.version, 1);
  const request = object(fixture.request);
  assert.ok(typeof fixture.walletReviewId === "string" && /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/u.test(fixture.walletReviewId));
  assert.ok(typeof fixture.payerAccount === "string" && /^[0-9a-f]{64}$/u.test(fixture.payerAccount));
  assert.equal(request.walletApprovalId, undefined);
  assert.ok("variant" in object(request.preparation), "native daemon preparation required");
  assert.equal(request.tenant, process.env.LAYERX_TENANT);
  endpoint(process.env.LAYERX_AGENT_RPC_URL);
  const base = endpoint(fixture.merchantUrl, true);
  const config = await privateJson(fixture.merchantConfig);
  assert.equal(config.application, "merchant-checkout");
  assert.ok(["emulator", "beta"].includes(fixture.merchantEnvironment));
  const selected = object(object(config.environments)[fixture.merchantEnvironment]);
  endpoint(selected.settlementUrl); endpoint(selected.receiptAuthorityUrl);
  assert.equal(selected.protocolVersion, Number(process.env.LAYERX_PROTOCOL_VERSION));
  assert.equal(base.port, String(selected.port));
  assert.equal(process.env.LAYERX_EXAMPLE_ENDPOINT, undefined, "acceptance must use its declared service origins");
  const checkout = object(fixture.checkout);
  assert.ok(typeof checkout.checkout_key === "string" && /^[A-Za-z0-9._-]{1,200}$/u.test(checkout.checkout_key));
  const webhook = object(fixture.webhook);
  assert.ok(typeof webhook.body === "string" && Buffer.byteLength(webhook.body) <= 65536);
  const webhookHeaders = object(webhook.headers);
  for (const name of ["layerx-webhook-id", "layerx-webhook-timestamp", "layerx-webhook-key-id", "layerx-webhook-signature"]) assert.equal(typeof webhookHeaders[name], "string");

  phase = "native preparation";
  const consent = await example("spend", request);
  check(consent.kind === "owner-budget" && consent.ownerState === "wallet-consent" && consent.admissionObserved === true, "native preparation remains unsigned pending original-wallet consent");
  const prepared = object(consent.prepared);
  phase = "original wallet consent";
  const status = await example("wallet-status", request, {
    LAYERX_WALLET_PREPARATION_REF: prepared.preparation_ref, LAYERX_WALLET_REVIEW_ID: fixture.walletReviewId,
  });
  const artifact = object(status.artifact);
  check(artifact.state === "approved" && artifact.kind === "lx_activity" && artifact.activity === prepared.unsigned_canonical_bytes
    && artifact.signing_preimage === prepared.signing_preimage && artifact.public_key === request.signerPublicKey,
  "explicit original-wallet approval binds the retained native preparation");
  const approved = { ...request, walletApprovalId: fixture.walletReviewId };
  phase = "approved submission";
  const settled = await example("spend", approved);
  check(settled.kind === "owner-budget" && settled.ownerState === "settled" && /^[0-9a-f]{64}$/u.test(settled.receiptDigest), "approved native submission resolves a verified receipt");
  const authenticated = authenticatedAgentTransport(process.env, {
    agentRpcUrl: process.env.LAYERX_AGENT_RPC_URL, tenant: request.tenant, requestTimeoutMs: remaining(),
  });
  try {
    const balance = async () => {
      const response = await authenticated.transport.call({ plane: "agent", operation: "read.balance", request: { tenant: request.tenant, agent: process.env.LAYERX_ACTOR } });
      const value = object(response.value);
      assert.equal(response.verification_status?.state, "achieved");
      assert.ok(["CheckpointFinalised", "SettlementAnchored"].includes(response.verification_status.level));
      assert.equal(value.account, fixture.payerAccount); assert.equal(value.asset, request.asset.replace(/^0x/u, ""));
      assert.ok(typeof value.amount === "string" && /^(0|[1-9][0-9]*)$/u.test(value.amount));
      return value.amount;
    };
    phase = "durable recovery without a second debit";
    const before = await balance();
    const recovered = await example("spend", approved);
    check(recovered.kind === "owner-budget" && recovered.ownerState === "settled" && recovered.preparationId === settled.preparationId
      && recovered.receiptDigest === settled.receiptDigest && await balance() === before, "fresh agent process reopens durable preparation and receipt without a second debit");
    const refused = async (input) => {
      try { await authenticated.transport.call(input); }
      catch (error) {
        assert.ok(error instanceof PlatformSdkError && error.retry === "never"
          && ["policy-refusal", "capability-refusal", "core-rejection", "verification-failure"].includes(error.code), "genuine typed daemon refusal required");
        return true;
      }
      return false;
    };
    phase = "daemon cross-tenant refusal";
    const cross = object(fixture.crossTenantPreparation);
    assert.ok(cross.actor !== request.preparation.actor && cross.purpose?.purpose?.tenant !== request.tenant, "genuine other-tenant preparation input required");
    check(await refused({ plane: "agent", operation: "prepare", request: cross, idempotencyKey: idempotencyKey(cross.idempotency_key) }), "daemon refuses another tenant's native preparation");
    phase = "daemon preparation and signed activity binding";
    const signed = await example("wallet-status", approved, { LAYERX_WALLET_PREPARATION_REF: prepared.preparation_ref, LAYERX_WALLET_REVIEW_ID: fixture.walletReviewId });
    assert.equal(signed.artifact?.state, "signed");
    assert.ok(typeof fixture.mismatchedPreparationRef === "string" && /^[0-9a-f]{64}$/u.test(fixture.mismatchedPreparationRef)
      && fixture.mismatchedPreparationRef !== prepared.preparation_ref, "distinct genuine preparation reference required");
    check(await refused({ plane: "agent", operation: "submit", request: {
      preparation_ref: fixture.mismatchedPreparationRef, signature: signed.artifact.signature,
      signer_public_key: request.signerPublicKey, approval_release_ref: request.approvalReleaseRef ?? null,
    }, idempotencyKey: idempotencyKey(createHash("sha256").update("LayerX/middleware/rejected-preparation\0").update(request.submitIdempotencyKey).update(fixture.mismatchedPreparationRef).digest("hex")) }), "daemon refuses signed activity against a mismatched preparation");
    check(await balance() === before, "refused cross-tenant and preparation mismatches cannot debit the payer");
  } finally { authenticated.destroy(); }
  const providers = await loadAgentServiceProviders({ ...process.env, LAYERX_AGENT_SERVICES_MODULE: servicesEntry });
  let evidence;
  try { evidence = await providers.receipts.resolve(settled.receiptDigest); }
  finally { await providers.destroy?.(); }
  const paymentDigest = createHash("sha256").update("LXP/v1/merkle-leaf\0").update(evidence.canonicalReceipt).digest("hex");

  let child;
  try {
    phase = "merchant application";
    child = spawn(process.execPath, [merchantEntry, "--environment", fixture.merchantEnvironment], {
      env: { ...process.env, LAYERX_EXAMPLE_CONFIG: fixture.merchantConfig }, stdio: ["ignore", "pipe", "pipe"],
    });
    await new Promise((accept, reject) => {
      const timer = setTimeout(() => { child.kill("SIGKILL"); reject(new Error("merchant_example_deadline")); }, remaining());
      let output = "";
      const fail = () => { clearTimeout(timer); reject(new Error("merchant_example_not_listening")); };
      child.once("error", fail); child.once("exit", fail);
      child.stderr.resume();
      child.stdout.on("data", (chunk) => {
        output += chunk;
        if (output.length > 65536) { child.kill("SIGKILL"); fail(); return; }
        if (output.includes("\n")) {
          try {
            const ready = object(JSON.parse(output.split("\n")[0]));
            assert.equal(ready.application, "merchant-checkout"); assert.equal(ready.listening, selected.port);
            clearTimeout(timer); child.removeListener("exit", fail); child.removeListener("error", fail); child.stdout.resume(); accept();
          } catch { child.kill("SIGKILL"); fail(); }
        }
      });
    });
    const unpaid = await post(base, "/checkout", checkout);
    check(unpaid.response.status === 402 && unpaid.value.state === "payment-required" && unpaid.value.order?.state === "awaiting-payment", "catalog checkout cannot render paid before receipt evidence");
    const offer = decodePaymentRequiredHeader(unpaid.response.headers.get("payment-required"));
    const requirements = offer.accepts.find((row) => row.scheme === "exact" && row.amount === request.amount && row.asset.replace(/^0x/u, "") === request.asset.replace(/^0x/u, "") && row.payTo.replace(/^0x/u, "") === request.recipient.replace(/^0x/u, ""));
    assert.ok(requirements, "checkout offer must match actual native payment");
    const verification = await verifyPaymentReceipt(evidence, requirements, undefined, { protocolVersion: Number(process.env.LAYERX_PROTOCOL_VERSION) });
    check(Buffer.from(verification.receiptDigest).toString("hex") === settled.receiptDigest, "merchant payment verifies the same daemon receipt");
    const payload = { x402Version: 2, resource: offer.resource, accepted: requirements, extensions: offer.extensions ?? {}, payload: {
      receipt: Buffer.from(evidence.canonicalReceipt).toString("base64"), receiptDigest: paymentDigest,
      verificationLevel: verification.level, idempotencyKey: request.submitIdempotencyKey,
    } };
    const tamperCheckout = { ...checkout, checkout_key: `${checkout.checkout_key}-tamper` };
    const tamperOfferResponse = await post(base, "/checkout", tamperCheckout);
    assert.equal(tamperOfferResponse.response.status, 402);
    const tamperOffer = decodePaymentRequiredHeader(tamperOfferResponse.response.headers.get("payment-required"));
    const corrupted = Uint8Array.from(evidence.canonicalReceipt); corrupted[corrupted.length - 1] ^= 1;
    const bad = await post(base, "/checkout", tamperCheckout, { "payment-signature": encodePaymentPayloadHeader({ ...payload, resource: tamperOffer.resource, extensions: tamperOffer.extensions ?? {}, payload: { ...payload.payload, receipt: Buffer.from(corrupted).toString("base64") } }) });
    check(bad.response.status !== 200 && bad.value.order?.state !== "paid-verified", "tampered genuine receipt cannot pay checkout");
    const absent = await post(base, "/webhooks/settlement", webhook.body);
    check(absent.response.status >= 400, "unsigned settlement webhook is rejected");
    const headers = { "payment-signature": encodePaymentPayloadHeader(payload) };
    phase = "merchant settlement";
    const paid = await post(base, "/checkout", checkout, headers);
    check(paid.response.status === 200 && paid.value.state === "paid" && paid.value.order?.state === "paid-verified"
      && paid.value.order.receiptDigest === paymentDigest, "real settlement authority pays receipt-backed merchant order");
    const settlement = decodeSettlementHeader(paid.response.headers.get("payment-response"));
    check(settlement.success === true && settlement.extensions?.layerx?.receiptDigest === paymentDigest, "checkout returns matching verified settlement evidence");
    const replay = await post(base, "/checkout", checkout, headers);
    check(replay.response.status === 200 && replay.value.order?.receiptDigest === paymentDigest, "checkout replay retains the same receipt-backed order");
    phase = "signed settlement webhooks";
    const delivery = await post(base, "/webhooks/settlement", webhook.body, webhookHeaders);
    check(delivery.response.status === 200 && delivery.value.state === "processed", "genuine signed settlement webhook is verified and completed");
    const redelivery = await post(base, "/webhooks/settlement", webhook.body, webhookHeaders);
    check(redelivery.response.status === 200 && redelivery.value.state === "duplicate", "at-least-once webhook redelivery is idempotent");
    const invalid = await post(base, "/webhooks/settlement", `${webhook.body} `, webhookHeaders);
    check(invalid.response.status >= 400, "changed signed webhook payload is rejected");
  } finally {
    if (child && child.exitCode === null) { child.kill("SIGTERM"); await new Promise((accept) => {
      const timer = setTimeout(() => { child.kill("SIGKILL"); accept(); }, 2000);
      child.once("exit", () => { clearTimeout(timer); accept(); });
    }); }
  }
  process.stdout.write(`PAXEER_X_GATE tests=${tests} skipped=0\n`);
}

main().catch((error) => { process.stderr.write(`connected middleware acceptance failed at ${phase}: ${error.code ?? error.name}; genuine service evidence is required\n`); process.exitCode = 1; });
