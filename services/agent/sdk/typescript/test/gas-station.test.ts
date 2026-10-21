import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { createServer } from "node:http";
import { once } from "node:events";
import { spawn } from "node:child_process";
import { ed25519 } from "@noble/curves/ed25519.js";
import { secp256k1 } from "@noble/curves/secp256k1.js";
import { keccak_256 } from "@noble/hashes/sha3.js";

import {
  SIDIORA_TOKEN,
  SIDIORA_DECIMALS,
  abiSelector,
  assembleEip7702Authorization,
  eip7702AuthorizationDigest,
  encodeAbiCall,
  discoverSponsoredNonce,
  gasQuoteDigest,
  readNativeFeePreference,
  requestGasQuote,
  submitSponsoredGasBatch,
  sponsoredBatchCall,
  sponsoredBatchDigest,
  type GasQuote,
  type GasRefusalCode,
  type GasResult,
  type GasStationConfig,
  type SponsoredBatch,
} from "../src/index.js";

function value<T>(result: GasResult<T>): T {
  assert.equal(
    result.ok,
    true,
    JSON.stringify(result, (_key, item: unknown) =>
      typeof item === "bigint" ? item.toString() : item,
    ),
  );
  return result.value;
}

function refused(
  result: GasResult<unknown>,
  code: GasRefusalCode,
  field?: string,
): void {
  assert.equal(result.ok, false);
  if (result.ok) throw new Error("expected refusal");
  assert.equal(result.refusal.code, code);
  if (field !== undefined) assert.equal(result.refusal.field, field);
}

const quote: GasQuote = {
  sponsor: `0x${"22".repeat(20)}`,
  token: SIDIORA_TOKEN,
  maxTokenAmount: 2_100_000n,
  tokenAmount: 2_000_000n,
  deadline: 1000n,
  quoteNonce: 7n,
  gasCost: 10n ** 18n,
  decimals: SIDIORA_DECIMALS,
};
const vector: SponsoredBatch = {
  chainId: 1325n,
  account: `0x${"11".repeat(20)}`,
  nonce: 0n,
  calls: [{ to: `0x${"33".repeat(20)}`, value: 0n, data: "0x1234" }],
  quote,
};
const quoteDigest =
  "0x6c11f34e7848d98b1ae328fe84bf47223eb14e5274ae04daf3dc64c304c813ba";
const batchDigest =
  "0xeba39a1c4de2cb2415a6e26ed362f8237cfe005c8d55ebe8f089cf6f7c636d50";
const foundry = readFileSync(
  new URL(
    "../../../../../../contracts/test/BatchCallAndSponsorTest.t.sol",
    import.meta.url,
  ),
  "utf8",
);
assert.ok(foundry.includes(quoteDigest));
assert.ok(foundry.includes(batchDigest));
assert.equal(
  value(gasQuoteDigest(vector.chainId, vector.account, quote)),
  quoteDigest,
);
assert.equal(value(sponsoredBatchDigest(vector)), batchDigest);
for (const field of [
  "maxTokenAmount",
  "tokenAmount",
  "deadline",
  "quoteNonce",
  "gasCost",
] as const) {
  const changed = { ...quote, [field]: quote[field] + 1n };
  assert.notEqual(
    value(gasQuoteDigest(vector.chainId, vector.account, changed)),
    quoteDigest,
  );
  assert.notEqual(
    value(sponsoredBatchDigest({ ...vector, quote: changed })),
    batchDigest,
  );
}
for (const field of ["token", "sponsor"] as const) {
  const changed = { ...quote, [field]: vector.account };
  assert.notEqual(
    value(gasQuoteDigest(vector.chainId, vector.account, changed)),
    quoteDigest,
  );
  assert.notEqual(
    value(sponsoredBatchDigest({ ...vector, quote: changed })),
    batchDigest,
  );
}
for (const changed of [
  { ...vector, chainId: 1326n },
  { ...vector, account: quote.sponsor },
  { ...vector, nonce: 1n },
  { ...vector, calls: [] },
  { ...vector, calls: [{ ...vector.calls[0]!, data: "0x1235" }] },
  { ...vector, calls: [{ ...vector.calls[0]!, value: 1n }] },
  { ...vector, calls: [{ ...vector.calls[0]!, to: vector.account }] },
])
  assert.notEqual(value(sponsoredBatchDigest(changed)), batchDigest);

const accountKey = keccak_256(
  new TextEncoder().encode("sponsored account fixture"),
);
const sponsorKey = keccak_256(
  new TextEncoder().encode("sponsored relayer fixture"),
);
const evmAddress = (key: Uint8Array): string =>
  `0x${Buffer.from(keccak_256(secp256k1.getPublicKey(key, false).slice(1)))
    .subarray(12)
    .toString("hex")}`;
function sign(digest: string, key: Uint8Array): string {
  const signed = secp256k1.sign(Buffer.from(digest.slice(2), "hex"), key, {
    prehash: false,
    format: "recovered",
  });
  return `0x${Buffer.from(signed.slice(1)).toString("hex")}${(27 + signed[0]!).toString(16)}`;
}
const batch: SponsoredBatch = {
  ...vector,
  account: evmAddress(accountKey),
  quote: { ...quote, sponsor: evmAddress(sponsorKey) },
};
const config: GasStationConfig = {
  chainId: vector.chainId,
  sponsor: batch.quote.sponsor,
  token: SIDIORA_TOKEN,
  decimals: SIDIORA_DECIMALS,
  paymaster: `0x${"44".repeat(20)}`,
  quoteUrl: "",
};
const accountSignature = sign(value(sponsoredBatchDigest(batch)), accountKey);
const relayerSignature = sign(
  value(gasQuoteDigest(batch.chainId, batch.account, batch.quote)),
  sponsorKey,
);
const call = value(
  sponsoredBatchCall(config, batch, accountSignature, relayerSignature, 1000n),
);
assert.equal(call.to, batch.account);
assert.equal(call.value, 0n);
assert.equal(
  call.data.slice(0, 10),
  abiSelector(
    "executeSponsored((address,uint256,bytes)[],(address,address,uint256,uint256,uint256,uint256,uint256),bytes,bytes)",
  ),
);
const body = call.data.slice(10);
const at = (offset: number): bigint =>
  BigInt(`0x${body.slice(offset * 2, (offset + 32) * 2)}`);
assert.equal(at(0), 320n);
assert.equal(at(32), BigInt(batch.quote.sponsor));
assert.equal(at(64), BigInt(SIDIORA_TOKEN));
assert.equal(at(96), quote.maxTokenAmount);
assert.equal(at(128), quote.tokenAmount);
assert.equal(at(160), quote.deadline);
assert.equal(at(192), quote.quoteNonce);
assert.equal(at(224), quote.gasCost);
assert.equal(at(256), 544n);
assert.equal(at(288), 672n);
assert.equal(at(320), 1n);
assert.equal(at(352), 32n);
assert.equal(at(384), BigInt(vector.calls[0]!.to));
assert.equal(at(416), 0n);
assert.equal(at(448), 96n);
assert.equal(at(480), 2n);
assert.equal(body.slice(1024, 1088), "1234".padEnd(64, "0"));
assert.equal(at(544), 65n);
assert.equal(body.slice(576 * 2, 641 * 2), accountSignature.slice(2));
assert.equal(at(672), 65n);
assert.equal(body.slice(704 * 2, 769 * 2), relayerSignature.slice(2));
assert.equal(body.length / 2, 800);

for (const calls of [
  [],
  [
    { to: vector.account, value: 2n, data: "0x" },
    { to: quote.sponsor, value: 3n, data: `0x${"ab".repeat(33)}` },
  ],
]) {
  const multiple = { ...batch, calls };
  const encoded = value(
    sponsoredBatchCall(
      config,
      multiple,
      sign(value(sponsoredBatchDigest(multiple)), accountKey),
      relayerSignature,
      999n,
    ),
  );
  const raw = encoded.data.slice(10);
  const read = (offset: number): number =>
    Number(BigInt(`0x${raw.slice(offset * 2, (offset + 32) * 2)}`));
  const array = read(0);
  assert.equal(read(array), calls.length);
  for (const [index, item] of calls.entries()) {
    const tuple = array + 32 + read(array + 32 + index * 32);
    assert.equal(
      `0x${raw.slice((tuple + 12) * 2, (tuple + 32) * 2)}`,
      item.to.toLowerCase(),
    );
    assert.equal(BigInt(read(tuple + 32)), item.value);
    const dataStart = tuple + read(tuple + 64);
    const length = read(dataStart);
    assert.equal(
      `0x${raw.slice((dataStart + 32) * 2, (dataStart + 32 + length) * 2)}`,
      item.data,
    );
  }
}

for (const [change, code] of [
  [{ deadline: 998n }, "expired_quote"],
  [{ maxTokenAmount: 1n }, "above_maximum"],
  [{ sponsor: vector.account }, "sponsor_mismatch"],
  [{ token: vector.account }, "token_mismatch"],
  [{ decimals: 18 }, "decimals_mismatch"],
  [{ tokenAmount: 0n }, "invalid_value"],
  [{ gasCost: 0n }, "invalid_value"],
  [{ quoteNonce: -1n }, "invalid_value"],
  [{ maxTokenAmount: 1n << 256n }, "invalid_value"],
] as const)
  refused(
    sponsoredBatchCall(
      config,
      { ...batch, quote: { ...batch.quote, ...change } },
      accountSignature,
      relayerSignature,
      999n,
    ),
    code,
  );
refused(
  sponsoredBatchCall(
    { ...config, decimals: 18 },
    batch,
    accountSignature,
    relayerSignature,
    999n,
  ),
  "decimals_mismatch",
);
refused(
  sponsoredBatchCall(
    { ...config, token: vector.account },
    batch,
    accountSignature,
    relayerSignature,
    999n,
  ),
  "token_mismatch",
);
refused(
  sponsoredBatchCall(
    config,
    { ...batch, chainId: 1n },
    accountSignature,
    relayerSignature,
    999n,
  ),
  "chain_mismatch",
);
refused(
  sponsoredBatchCall(config, batch, "0x", relayerSignature, 999n),
  "invalid_signature",
);
refused(
  sponsoredBatchCall(config, batch, accountSignature, "0x", 999n),
  "invalid_signature",
);
refused(
  sponsoredBatchCall(config, batch, relayerSignature, accountSignature, 999n),
  "invalid_signature",
);
refused(
  sponsoredBatchCall(
    config,
    { ...batch, nonce: 1n },
    accountSignature,
    relayerSignature,
    999n,
  ),
  "invalid_signature",
);
refused(
  sponsoredBatchCall(
    config,
    batch,
    accountSignature,
    sign(
      value(
        gasQuoteDigest(batch.chainId, batch.account, {
          ...batch.quote,
          quoteNonce: 8n,
        }),
      ),
      sponsorKey,
    ),
    999n,
  ),
  "invalid_signature",
);
refused(
  sponsoredBatchDigest({
    ...batch,
    calls: [{ to: vector.account, value: 0n, data: "0x1" }],
  }),
  "invalid_value",
);
refused(gasQuoteDigest(-1n, batch.account, quote), "invalid_value");

const authorization = {
  chainId: config.chainId,
  address: config.paymaster,
  nonce: 0n,
};
const authorizationDigest = value(eip7702AuthorizationDigest(authorization));
assert.equal(
  authorizationDigest,
  `0x${Buffer.from(keccak_256(Buffer.from(`05d982052d94${"44".repeat(20)}80`, "hex"))).toString("hex")}`,
);
assert.notEqual(authorizationDigest, value(sponsoredBatchDigest(batch)));
const authorizationSignature = sign(authorizationDigest, accountKey);
assert.deepEqual(
  value(
    assembleEip7702Authorization(
      config,
      batch.account,
      0n,
      authorizationSignature,
    ),
  ),
  {
    ...authorization,
    r: authorizationSignature.slice(0, 66),
    s: `0x${authorizationSignature.slice(66, 130)}`,
    yParity: Number.parseInt(authorizationSignature.slice(130), 16) - 27,
  },
);
for (const changed of [
  { ...authorization, nonce: 1n },
  { ...authorization, chainId: 1n },
  { ...authorization, address: vector.account },
]) {
  assert.notEqual(
    value(eip7702AuthorizationDigest(changed)),
    authorizationDigest,
  );
}
refused(
  eip7702AuthorizationDigest({ ...authorization, nonce: (1n << 64n) - 1n }),
  "invalid_value",
);
refused(
  assembleEip7702Authorization(
    config,
    batch.account,
    1n,
    authorizationSignature,
  ),
  "invalid_signature",
);
refused(
  assembleEip7702Authorization(config, batch.account, 0n, accountSignature),
  "invalid_signature",
);

const did = `did:layerx:${Buffer.from(ed25519.getPublicKey(accountKey)).toString("hex")}`;
const request = {
  account: batch.account,
  did,
  nonce: batch.nonce,
  calls: batch.calls,
  maxTokenAmount: quote.maxTokenAmount,
  gasCost: quote.gasCost,
};
const wireQuote = JSON.parse(
  JSON.stringify(batch.quote, (_key, item: unknown) =>
    typeof item === "bigint" ? item.toString() : item,
  ),
) as Record<string, unknown>;
const envelope = { quote: wireQuote, relayerSignature, feeDenom: "uhpx" };
let responseBody = JSON.stringify(envelope);
let responseStatus = 200;
let requestCount = 0;
let stallRequests = false;
let onStalledRequest: (() => void) | undefined;
const server = createServer(async (incoming, outgoing) => {
  try {
    assert.equal(incoming.method, "POST");
    assert.equal(incoming.url, "/quote");
    assert.equal(incoming.headers["content-type"], "application/json");
    const chunks: Buffer[] = [];
    for await (const chunk of incoming) chunks.push(Buffer.from(chunk));
    const payload = JSON.parse(
      Buffer.concat(chunks).toString("utf8"),
    ) as Record<string, unknown>;
    assert.equal(payload.chainId, "1325");
    assert.equal(payload.account, batch.account);
    assert.equal(payload.did, did);
    assert.equal(payload.nonce, batch.nonce.toString());
    assert.equal(payload.token, SIDIORA_TOKEN);
    assert.equal(payload.decimals, SIDIORA_DECIMALS);
    assert.equal(payload.gasCost, "1000000000000000000");
    assert.equal(payload.maxTokenAmount, "2100000");
    assert.deepEqual(
      payload.calls,
      batch.calls.map((item) => ({ ...item, value: item.value.toString() })),
    );
    requestCount++;
    if (stallRequests) {
      onStalledRequest?.();
      return;
    }
    outgoing.writeHead(responseStatus, { "content-type": "application/json" });
    outgoing.end(responseBody);
  } catch (error) {
    outgoing.destroy(
      error instanceof Error ? error : new Error("invalid request"),
    );
  }
});
server.listen(0);
await once(server, "listening");
const bound = server.address();
assert.ok(bound && typeof bound !== "string");
const quoteUrl = `http://${bound.family === "IPv6" ? `[${bound.address}]` : bound.address}:${bound.port}/quote`;
const httpConfig = { ...config, quoteUrl };
try {
  assert.deepEqual(
    value(await requestGasQuote(httpConfig, request, { now: 999n })),
    { quote: batch.quote, relayerSignature, feeDenom: "uhpx" },
  );
  refused(
    await requestGasQuote(httpConfig, request, { now: 1001n }),
    "expired_quote",
  );
  for (const [field, item, code] of [
    ["sponsor", vector.account, "sponsor_mismatch"],
    ["token", vector.account, "token_mismatch"],
    ["decimals", 18, "decimals_mismatch"],
    ["maxTokenAmount", "1", "above_maximum"],
    ["maxTokenAmount", "2100001", "above_maximum"],
    ["gasCost", "1", "invalid_response"],
    ["tokenAmount", 2_000_000, "invalid_response"],
    ["tokenAmount", "-1", "invalid_response"],
    ["tokenAmount", "2e6", "invalid_response"],
    ["tokenAmount", "02000000", "invalid_response"],
  ] as const) {
    responseBody = JSON.stringify({
      ...envelope,
      quote: { ...wireQuote, [field]: item },
    });
    refused(await requestGasQuote(httpConfig, request, { now: 999n }), code);
  }
  responseBody = JSON.stringify({
    ...envelope,
    relayerSignature: accountSignature,
  });
  refused(
    await requestGasQuote(httpConfig, request, { now: 999n }),
    "invalid_signature",
  );
  responseBody = "{";
  refused(
    await requestGasQuote(httpConfig, request, { now: 999n }),
    "invalid_response",
  );
  responseBody = "null";
  refused(
    await requestGasQuote(httpConfig, request, { now: 999n }),
    "invalid_response",
  );
  responseStatus = 403;
  refused(await requestGasQuote(httpConfig, request, { now: 999n }), "refused");
  responseStatus = 503;
  refused(
    await requestGasQuote(httpConfig, request, { now: 999n }),
    "unavailable",
  );
  refused(
    await requestGasQuote(httpConfig, request, {
      signal: AbortSignal.abort(),
      now: 999n,
    }),
    "cancelled",
  );
  refused(
    await requestGasQuote(config, request, { now: 999n }),
    "invalid_value",
  );
  assert.equal(requestCount, 17);
  for (const feeDenom of ["factory/issuer/usid", "a".repeat(64)]) {
    responseStatus = 200;
    responseBody = JSON.stringify({ ...envelope, feeDenom });
    const signed = value(
      await requestGasQuote(httpConfig, request, { now: 999n }),
    );
    assert.equal(signed.feeDenom, feeDenom);
    assert.equal(signed.quote.token, SIDIORA_TOKEN);
    assert.equal(signed.quote.tokenAmount, batch.quote.tokenAmount);
    assert.equal(
      value(gasQuoteDigest(config.chainId, batch.account, signed.quote)),
      value(gasQuoteDigest(config.chainId, batch.account, batch.quote)),
    );
  }
  for (const feeDenom of [
    null,
    6,
    "",
    "ab",
    "a".repeat(65),
    "usid ",
    "usid\n",
  ]) {
    responseBody = JSON.stringify({ ...envelope, feeDenom });
    refused(
      await requestGasQuote(httpConfig, request, { now: 999n }),
      "invalid_response",
      "feeDenom",
    );
  }
  responseBody = JSON.stringify({ quote: wireQuote, relayerSignature });
  refused(
    await requestGasQuote(httpConfig, request, { now: 999n }),
    "invalid_response",
    "feeDenom",
  );
  for (const status of [403, 422, 429]) {
    responseStatus = status;
    responseBody = JSON.stringify({ error: "budget_exhausted" });
    refused(
      await requestGasQuote(httpConfig, request, { now: 999n }),
      "budget_exhausted",
      "quoteUrl",
    );
  }
  responseStatus = 422;
  for (const code of [
    "incompatible_delegation",
    "replay_state_unavailable",
    "expired_quote",
  ] as const) {
    responseBody = JSON.stringify({ error: code });
    refused(
      await requestGasQuote(httpConfig, request, { now: 999n }),
      code,
      "quoteUrl",
    );
  }
  responseBody = JSON.stringify({
    error: "budget_exhausted",
    padding: "a".repeat(1024),
  });
  refused(
    await requestGasQuote(httpConfig, request, { now: 999n }),
    "invalid_response",
    "quoteUrl",
  );
  responseBody = "{";
  refused(
    await requestGasQuote(httpConfig, request, { now: 999n }),
    "invalid_response",
    "quoteUrl",
  );
  const beforeInvalidDid = requestCount;
  for (const invalidDid of [
    "",
    `did:layerx:${"0".repeat(64)}`,
    `did:layerx:${"1".repeat(63)}`,
    `did:layerx:${"a".repeat(65)}`,
    did.toUpperCase(),
    batch.account,
  ]) {
    refused(
      await requestGasQuote(
        httpConfig,
        { ...request, did: invalidDid },
        { now: 999n },
      ),
      "invalid_value",
      "did",
    );
  }
  const { did: _did, ...missingDid } = request;
  refused(
    await requestGasQuote(httpConfig, missingDid as typeof request, {
      now: 999n,
    }),
    "invalid_value",
    "did",
  );
  assert.equal(requestCount, beforeInvalidDid);
  refused(
    await submitSponsoredGasBatch(
      httpConfig,
      { ...batch, nonce: 3n },
      accountSignature,
      relayerSignature,
      value(
        assembleEip7702Authorization(
          config,
          batch.account,
          0n,
          authorizationSignature,
        ),
      ),
      { now: 999n },
    ),
    "invalid_signature",
    "signature",
  );
  assert.equal(requestCount, 35);
  stallRequests = true;
  const controller = new AbortController();
  const watchdog = setTimeout(() => controller.abort(), 20_000);
  try {
    const results = await Promise.all([
      requestGasQuote(httpConfig, request, {
        signal: controller.signal,
        now: 999n,
      }),
      requestGasQuote(httpConfig, request, { now: 999n }),
    ]);
    for (const result of results) refused(result, "unavailable");
    assert.equal(controller.signal.aborted, false);
  } finally {
    clearTimeout(watchdog);
  }
  const cancellation = new AbortController();
  onStalledRequest = () => cancellation.abort();
  refused(
    await requestGasQuote(httpConfig, request, {
      signal: cancellation.signal,
      now: 999n,
    }),
    "cancelled",
  );
  assert.equal(requestCount, 38);
} finally {
  const closed = once(server, "close");
  server.close();
  server.closeAllConnections();
  await closed;
}
refused(
  await requestGasQuote(httpConfig, request, { now: 999n }),
  "unavailable",
);

async function paymaster_nonce_stream(): Promise<void> {
  const evm = spawn(
    process.env.ANVIL_BIN ?? "anvil",
    [
      "--port",
      "0",
      "--accounts",
      "1",
      "--chain-id",
      config.chainId.toString(),
      "--hardfork",
      "prague",
    ],
    {
      stdio: ["ignore", "pipe", "pipe"],
    },
  );
  evm.stderr.resume();
  try {
    const rpcUrl = await new Promise<string>((resolve, reject) => {
      let output = "";
      const timer = setTimeout(
        () => reject(new Error("local EVM startup timed out")),
        15_000,
      );
      const fail = () => {
        clearTimeout(timer);
        reject(new Error("local EVM exited before readiness"));
      };
      evm.once("error", fail);
      evm.once("exit", fail);
      const ready = (chunk: Buffer) => {
        output = (output + chunk.toString("utf8")).slice(-8192);
        const listening = /Listening on ([^\s]+)/u.exec(output);
        if (listening?.[1] !== undefined) {
          clearTimeout(timer);
          evm.removeListener("error", fail);
          evm.removeListener("exit", fail);
          evm.stdout.removeListener("data", ready);
          evm.stdout.resume();
          resolve(new URL(`http://${listening[1]}`).toString());
        }
      };
      evm.stdout.on("data", ready);
    });
    let id = 0;
    const requests: Parameters<
      Parameters<typeof discoverSponsoredNonce>[2]
    >[0][] = [];
    const rpc: Parameters<typeof discoverSponsoredNonce>[2] = async (args) => {
      requests.push(args);
      const response = await fetch(rpcUrl, {
        method: "POST",
        headers: { "content-type": "application/json" },
        body: JSON.stringify({ jsonrpc: "2.0", id: ++id, ...args }),
        signal: AbortSignal.timeout(15_000),
      });
      assert.equal(response.status, 200);
      const payload = (await response.json()) as Record<string, unknown>;
      assert.equal(payload.jsonrpc, "2.0");
      assert.equal(payload.id, id);
      if (payload.error !== undefined)
        throw new Error(JSON.stringify(payload.error));
      return payload.result;
    };
    const accounts = await rpc({ method: "eth_accounts", params: [] });
    assert.ok(Array.isArray(accounts) && typeof accounts[0] === "string");
    const owner = accounts[0] as string;
    const source = readFileSync(
      new URL("../../../../../../node/sequencer/fee_test.go", import.meta.url),
      "utf8",
    );
    const creationCode = /const batchCallAndSponsorBin = "([0-9a-f]+)"/u.exec(
      source,
    )?.[1];
    assert.ok(creationCode);
    const deployment = await rpc({
      method: "eth_sendTransaction",
      params: [
        {
          from: owner,
          gas: "0x300000",
          data: `0x${creationCode}${encodeAbiCall("constructor", ["address", "uint256", "uint256"], [owner, 2_000_000n, 300n]).slice(10)}`,
        },
      ],
    });
    await rpc({ method: "evm_mine", params: [] });
    const receipt = (await rpc({
      method: "eth_getTransactionReceipt",
      params: [deployment],
    })) as Record<string, unknown>;
    assert.equal(receipt.status, "0x1");
    assert.equal(typeof receipt.contractAddress, "string");
    const paymaster = receipt.contractAddress as string;
    const liveConfig = { ...config, paymaster };
    const implementation = await rpc({
      method: "eth_getCode",
      params: [paymaster, "pending"],
    });
    assert.equal(typeof implementation, "string");
    assert.notEqual(implementation, "0x");
    assert.equal(
      await rpc({
        method: "eth_getBalance",
        params: [batch.account, "pending"],
      }),
      "0x0",
    );
    assert.equal(
      await rpc({ method: "eth_getCode", params: [batch.account, "pending"] }),
      "0x",
    );
    await rpc({
      method: "anvil_setStorageAt",
      params: [batch.account, `0x${"0".repeat(64)}`, `0x${"0".repeat(63)}3`],
    });
    assert.equal(
      await rpc({
        method: "eth_getStorageAt",
        params: [batch.account, "0x0", "pending"],
      }),
      `0x${"0".repeat(63)}3`,
    );
    requests.length = 0;
    assert.equal(
      value(await discoverSponsoredNonce(liveConfig, batch.account, rpc)),
      3n,
    );
    assert.deepEqual(requests, [
      { method: "eth_chainId", params: [] },
      { method: "eth_getCode", params: [batch.account, "latest"] },
      { method: "eth_getCode", params: [paymaster, "latest"] },
      {
        method: "eth_call",
        params: [
          { to: batch.account, data: abiSelector("nonce()") },
          "latest",
          { [batch.account]: { code: implementation } },
        ],
      },
    ]);
    await rpc({
      method: "anvil_setCode",
      params: [batch.account, `0xef0100${paymaster.slice(2)}`],
    });
    assert.equal(
      await rpc({ method: "eth_getCode", params: [batch.account, "pending"] }),
      `0xef0100${paymaster.slice(2)}`,
    );
    requests.length = 0;
    const nonce = value(
      await discoverSponsoredNonce(liveConfig, batch.account, rpc),
    );
    assert.equal(nonce, 3n);
    assert.deepEqual(requests, [
      { method: "eth_chainId", params: [] },
      { method: "eth_getCode", params: [batch.account, "latest"] },
      {
        method: "eth_call",
        params: [
          { to: batch.account, data: abiSelector("nonce()") },
          "latest",
        ],
      },
    ]);
    const evmNonce = await rpc({
      method: "eth_getTransactionCount",
      params: [batch.account, "pending"],
    });
    assert.equal(evmNonce, "0x0");
    const first = { ...batch, nonce };
    const second = { ...batch, nonce: nonce + 1n };
    assert.deepEqual([first.nonce, second.nonce], [3n, 4n]);
    const firstSignature = sign(value(sponsoredBatchDigest(first)), accountKey);
    const secondSignature = sign(
      value(sponsoredBatchDigest(second)),
      accountKey,
    );
    assert.notEqual(firstSignature, secondSignature);
    assert.equal(
      value(
        sponsoredBatchCall(
          liveConfig,
          first,
          firstSignature,
          relayerSignature,
          999n,
        ),
      ).to,
      batch.account,
    );
    assert.equal(
      value(
        sponsoredBatchCall(
          liveConfig,
          second,
          secondSignature,
          relayerSignature,
          999n,
        ),
      ).to,
      batch.account,
    );
    refused(
      sponsoredBatchCall(
        liveConfig,
        second,
        firstSignature,
        relayerSignature,
        999n,
      ),
      "invalid_signature",
      "signature",
    );
    assert.equal(
      await rpc({
        method: "eth_getTransactionCount",
        params: [batch.account, "pending"],
      }),
      evmNonce,
    );
    const authNonce = BigInt(evmNonce as string);
    const digest = value(
      eip7702AuthorizationDigest({
        chainId: liveConfig.chainId,
        address: paymaster,
        nonce: authNonce,
      }),
    );
    assert.equal(
      value(
        assembleEip7702Authorization(
          liveConfig,
          batch.account,
          authNonce,
          sign(digest, accountKey),
        ),
      ).nonce,
      0n,
    );
    refused(
      await discoverSponsoredNonce(
        { ...liveConfig, chainId: config.chainId + 1n },
        batch.account,
        rpc,
      ),
      "chain_mismatch",
      "chainId",
    );
    refused(
      await discoverSponsoredNonce(liveConfig, "0x", rpc),
      "invalid_value",
      "account",
    );
    await rpc({
      method: "anvil_setCode",
      params: [batch.account, `0xef0100${owner.slice(2)}`],
    });
    assert.equal(
      await rpc({ method: "eth_getCode", params: [batch.account, "pending"] }),
      `0xef0100${owner.slice(2)}`,
    );
    refused(
      await discoverSponsoredNonce(liveConfig, batch.account, rpc),
      "incompatible_delegation",
      "delegation",
    );
    await rpc({ method: "anvil_setCode", params: [batch.account, "0x"] });
    assert.equal(
      await rpc({ method: "eth_getCode", params: [batch.account, "pending"] }),
      "0x",
    );
    refused(
      await discoverSponsoredNonce(
        { ...liveConfig, paymaster: owner },
        batch.account,
        rpc,
      ),
      "replay_state_unavailable",
      "paymasterCode",
    );
    const options = { restUrl: quoteUrl, request: rpc };
    assert.deepEqual(await readNativeFeePreference(batch.account, options), {
      kind: "native-fee-preference",
      state: "inactive",
      reason: "chain_mismatch",
    });
    assert.deepEqual(await readNativeFeePreference("0x", options), {
      kind: "native-fee-preference",
      state: "unavailable",
      reason: "account",
    });
  } finally {
    if (
      evm.pid !== undefined &&
      evm.exitCode === null &&
      evm.signalCode === null
    ) {
      const closed = once(evm, "close");
      evm.kill("SIGTERM");
      await closed;
    }
  }
}

await paymaster_nonce_stream();
console.log(
  "gas station: 1 suite passed, 0 failed; real paymaster nonce, quote DID, fee denomination and refusals",
);
