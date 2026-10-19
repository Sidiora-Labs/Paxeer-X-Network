import { readFile } from "node:fs/promises";
import { createHash } from "node:crypto";
import { parseArgs } from "node:util";
import { decodePxResolvedIdentities } from "@sidiora/layerx-sdk";
import {
  PaymentRpc,
  PaymentRpcError,
  decodePaymentRequiredHeader,
  verifyRpcPayment,
  rpcBatchEvidence,
  rpcHex,
  rpcObject,
} from "../seller/dist/index.js";

class PublicRpc extends PaymentRpc {
  #id = 0;
  #endpoint;
  #headers;

  constructor(endpoint, headers) {
    super(endpoint, headers);
    this.#endpoint = new URL(endpoint);
    this.#headers = headers;
  }

  async call(method, params) {
    const id = ++this.#id;
    const response = await fetch(this.#endpoint, {
      method: "POST",
      redirect: "error",
      signal: AbortSignal.timeout(30_000),
      headers: { ...this.#headers, "content-type": "application/json" },
      body: JSON.stringify({ jsonrpc: "2.0", id, method, params }),
    });
    const reader = response.body?.getReader();
    if (!reader) throw new Error("missing-rpc-body");
    const chunks = [];
    let size = 0;
    for (;;) {
      const part = await reader.read();
      if (part.done) break;
      size += part.value.length;
      if (size > 8_388_608) {
        await reader.cancel();
        throw new Error("rpc-body-too-large");
      }
      chunks.push(part.value);
    }
    const bytes = new Uint8Array(size);
    let offset = 0;
    for (const chunk of chunks) {
      bytes.set(chunk, offset);
      offset += chunk.length;
    }
    let body;
    try {
      body = rpcObject(
        JSON.parse(new TextDecoder("utf-8", { fatal: true }).decode(bytes)),
      );
    } catch (error) {
      if (!response.ok) throw new Error(`rpc-http-${response.status}`);
      throw error;
    }
    if (
      body.jsonrpc !== "2.0" ||
      body.id !== id ||
      "error" in body === "result" in body
    )
      throw new Error("invalid-rpc-envelope");
    if ("error" in body) {
      const failure = rpcObject(body.error);
      if (
        !Number.isInteger(failure.code) ||
        typeof failure.message !== "string"
      )
        throw new Error("invalid-rpc-error");
      const error = new PaymentRpcError(failure.code, failure.data);
      error.message = `${method}: ${error.message}`;
      throw error;
    }
    if (!response.ok) throw new Error(`rpc-http-${response.status}`);
    if (method === "eth_getTransactionCount") {
      if (
        typeof body.result !== "string" ||
        !/^0x(?:0|[1-9a-f][0-9a-f]{0,15})$/u.test(body.result)
      )
        throw new Error("invalid-rpc-nonce");
      return body.result;
    }
    return rpcObject(body.result);
  }
}

const { values } = parseArgs({
  options: {
    help: { type: "boolean" },
    rpc: { type: "string" },
    did: { type: "string" },
    faucet: { type: "string" },
    "public-key": { type: "string" },
    "claim-key": { type: "string" },
    activity: { type: "string" },
    offer: { type: "string" },
    authority: { type: "string" },
    payer: { type: "string" },
  },
});
values.rpc ??= process.env.LAYERX_RPC_URL;
values.faucet ??= process.env.LAYERX_FAUCET_URL;
values.did ??= process.env.LAYERX_DID;
if (values.help) {
  process.stdout.write(
    "Usage: node public-rpc.mjs --rpc https://HOST/rpc --did DID [--faucet https://HOST/v1/faucet/claims --public-key HEX32 --claim-key KEY] [--activity SIGNED_HEX_FILE --offer PAYMENT_REQUIRED_HEADER_FILE --authority TRUSTED_AUTHORITY_JSON --payer ACCOUNT_HEX32]\nURLs may come from LAYERX_RPC_URL and LAYERX_FAUCET_URL; DID from LAYERX_DID. Use LAYERX_RPC_TOKEN for submission and LAYERX_FAUCET_TOKEN for faucet authentication. The payment example verifies executed or batched commitment selected by the offer.\n",
  );
} else {
  if (!values.rpc || !values.did) throw new Error("rpc-and-did-required");
  if (
    new URL(values.rpc).port === "18545" ||
    (values.faucet && new URL(values.faucet).port === "18545")
  )
    throw new Error("persistent-host-chain-forbidden");
  const rpc = new PublicRpc(
    values.rpc,
    process.env.LAYERX_RPC_TOKEN
      ? { authorization: `Bearer ${process.env.LAYERX_RPC_TOKEN}` }
      : {},
  );
  if (values.faucet) {
    const url = new URL(values.faucet);
    if (
      url.username ||
      url.password ||
      url.search ||
      url.hash ||
      url.pathname !== "/v1/faucet/claims" ||
      (url.protocol !== "https:" &&
        !(
          url.protocol === "http:" &&
          ["localhost", "127.0.0.1", "[::1]"].includes(url.hostname)
        ))
    )
      throw new Error("invalid-faucet-url");
    rpcHex(values["public-key"], 32);
    if (!values["claim-key"] || !process.env.LAYERX_FAUCET_TOKEN)
      throw new Error("faucet-claim-key-and-token-required");
    const response = await fetch(url, {
      method: "POST",
      redirect: "error",
      signal: AbortSignal.timeout(30_000),
      headers: {
        "content-type": "application/json",
        authorization: `Bearer ${process.env.LAYERX_FAUCET_TOKEN}`,
        "idempotency-key": values["claim-key"],
      },
      body: JSON.stringify({
        did: values.did,
        public_key: values["public-key"],
      }),
    });
    await response.body?.cancel();
    process.stdout.write(
      `Faucet HTTP status: ${response.status}; confirm the account balance before spending.\n`,
    );
    if (!response.ok) process.exitCode = 1;
  }
  const account = decodePxResolvedIdentities(
    await rpc.call("px_resolveAccount", [values.did]),
  );
  if (
    !account.bound ||
    account.layerxDid !== values.did ||
    account.evmAddress === null
  )
    throw new Error("did-evm-binding-required");
  const sequence = await rpc.call("eth_getTransactionCount", [
    account.evmAddress,
    "pending",
  ]);
  process.stdout.write(JSON.stringify({ sequence }) + "\n");
  if (values.activity) {
    if (!values.offer || !values.authority || !values.payer)
      throw new Error("offer-authority-and-payer-required");
    const required = decodePaymentRequiredHeader(
      (await readFile(values.offer, "utf8")).trim(),
    );
    const offer = required.accepts.find(
      (v) =>
        ["exact", "metered", "subscription"].includes(v.scheme) &&
        ["executed", "batched"].includes(
          v.extra?.layerx?.commitment ?? "executed",
        ),
    );
    if (!offer) throw new Error("executed-or-batched-offer-required");
    const commitment = offer.extra?.layerx?.commitment ?? "executed";
    const canonical = (await readFile(values.activity, "utf8")).trim();
    const activityId = createHash("sha256")
      .update("LXP/v1/activity-id\0")
      .update(rpcHex(canonical))
      .digest("hex");
    const configured = JSON.parse(await readFile(values.authority, "utf8"));
    const authority = Object.fromEntries(
      [
        "batchId",
        "asset",
        "previousStateRoot",
        "resultingStateRoot",
        "sequencerPublicKey",
      ].map((key) => [key, rpcHex(configured[key], 32)]),
    );
    const result = await rpc.send(canonical, commitment);
    const commitments =
      commitment === "executed"
        ? undefined
        : {
            async resolve(receipt, network, requested) {
              if (
                network !== offer.network ||
                requested !== "batched" ||
                !Number.isSafeInteger(configured.networkId) ||
                configured.networkId <= 0 ||
                typeof configured.firstBatchNumber !== "string" ||
                !/^(0|[1-9][0-9]*)$/u.test(configured.firstBatchNumber) ||
                typeof configured.lastBatchNumber !== "string" ||
                !/^(0|[1-9][0-9]*)$/u.test(configured.lastBatchNumber)
              )
                throw new Error("invalid-batch-authority");
              return rpcBatchEvidence(result, activityId, receipt, {
                networkId: configured.networkId,
                authorization: {
                  sequencerId: rpcHex(configured.sequencerId, 32),
                  publicKey: authority.sequencerPublicKey,
                  firstBatchNumber: BigInt(configured.firstBatchNumber),
                  lastBatchNumber: BigInt(configured.lastBatchNumber),
                },
              });
            },
          };
    const payment = await verifyRpcPayment(
      result,
      activityId,
      values.payer,
      offer,
      authority,
      commitments,
    );
    if (payment.kind === "pending") {
      process.stdout.write(
        JSON.stringify({ state: "pending", activity_id: activityId }) + "\n",
      );
      process.exitCode = 2;
    } else {
      if (
        offer.scheme !== "exact" &&
        (payment.verification.receipt.moduleId !== 1 ||
          payment.verification.receipt.operation !== 6)
      )
        throw new Error("grant-receive-receipt-required");
      const digest = createHash("sha256")
        .update("LXP/v1/merkle-leaf\0")
        .update(payment.canonicalReceipt)
        .digest("hex");
      process.stdout.write(
        JSON.stringify({ transaction: `lxp:${digest}`, commitment }) + "\n",
      );
    }
  }
}
