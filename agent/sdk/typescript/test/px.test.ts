import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { once } from "node:events";
import { readFileSync } from "node:fs";
import * as http from "node:http";
import { test } from "node:test";
import { fileURLToPath } from "node:url";

import {
  HISTORY_DEFAULT_LIMIT,
  JsonRpcClient,
  JsonRpcError,
  PxClient,
  decodeHistoryAssetMetadata,
  decodeLayerxHistoryPage,
  decodePaxeerHistoryPage,
  decodeUnifiedHistoryPage,
  historyParams,
  PX_MAXIMUM_JOINED_ASSETS,
  PX_METHODS,
  decodePxAccountBalances,
  decodePxAccountJoin,
  decodePxAssetTable,
  decodePxNetworkHead,
  decodePxReceiptKeys,
  decodePxResolvedIdentities,
  pxAccountKey,
  pxOptionalQuantity,
  pxQuantity,
} from "../src/index.js";

const ACCOUNT = "bb".repeat(32);
const DID = `did:layerx:${"aa".repeat(32)}`;
const EVM = `0x${"11".repeat(20)}`;
const NATIVE = "cc".repeat(32);
const UNJOINED = "dd".repeat(32);

assert.equal(pxAccountKey(EVM.toUpperCase()), EVM);
assert.equal(pxAccountKey(` ${DID.toUpperCase()} `), DID);
assert.equal(pxAccountKey(ACCOUNT.toUpperCase()), ACCOUNT);
for (const refused of [
  "",
  "0x",
  `0x${"11".repeat(19)}`,
  "did:layerx:zz",
  `did:paxeer:${"aa".repeat(32)}`,
  "bb".repeat(31),
  "agent:did:layerx:alice:main",
]) {
  assert.throws(() => pxAccountKey(refused));
}

assert.equal(pxQuantity("500000"), 500000n);
assert.equal(pxQuantity("0x1e"), 30n);
assert.equal(pxQuantity("0X1E"), 30n);
assert.equal(pxQuantity(0), 0n);
assert.equal(pxQuantity(16), 16n);
assert.equal(
  pxQuantity("340282366920938463463374607431768211455"),
  340282366920938463463374607431768211455n,
);
for (const refused of [
  "",
  "0x",
  "-1",
  "1.5",
  "007",
  "0xgg",
  `0x${"f".repeat(33)}`,
  "340282366920938463463374607431768211456",
  1.5,
  -1,
  null,
  true,
  {},
]) {
  assert.throws(() => pxQuantity(refused));
}
assert.equal(pxOptionalQuantity(null), null);
assert.equal(pxOptionalQuantity("0x00"), 0n);

const identities = {
  evm_address: EVM,
  pax_address: "pax1qq7p8m4nfz0k7h8s2v9d3l6c5x4b3n2m1q0w9e",
  layerx_did: DID,
  layerx_account: ACCOUNT,
  bound: true,
};
const custody = {
  asset_id: NATIVE,
  denom: "ulxp",
  pointer: `0x${"22".repeat(20)}`,
  enabled: true,
  paused: false,
  minimum_deposit: "1000",
  custody_cap: "100000000000",
  custodied: "500000",
  released: "0",
  pending: "0",
};
const balances = {
  account: identities,
  balances: [
    {
      asset_id: NATIVE,
      denom: "ulxp",
      custody,
      paxeer: { denom: "ulxp", amount: "0x1e" },
      layerx: { balance: "12" },
    },
    {
      asset_id: UNJOINED,
      denom: null,
      custody: null,
      paxeer: null,
      layerx: null,
    },
  ],
  joined_limit: 16,
};

const decoded = decodePxAccountBalances(balances);
assert.equal(decoded.account.evmAddress, EVM);
assert.equal(decoded.account.layerxDid, DID);
assert.equal(decoded.account.layerxAccount, ACCOUNT);
assert.equal(decoded.account.bound, true);
assert.equal(decoded.joinedLimit, 16n);
assert.equal(decoded.balances.length, 2);

const joined = decoded.balances[0]!;
assert.equal(joined.assetId, NATIVE);
assert.equal(joined.denom, "ulxp");
assert.equal(joined.paxeer?.amount, 30n);
assert.equal(joined.paxeer?.denom, "ulxp");
assert.equal(joined.custody?.custodied, 500000n);
assert.equal(joined.custody?.pending, 0n);
assert.equal(joined.custody?.enabled, true);
assert.equal(joined.custody?.paused, false);
assert.deepEqual(joined.layerx, { balance: "12" });

const unknownRow = decoded.balances[1]!;
assert.equal(unknownRow.denom, null);
assert.notEqual(unknownRow.denom, "");
assert.equal(unknownRow.custody, null);
assert.equal(unknownRow.paxeer, null);
assert.notEqual(unknownRow.paxeer, 0n);
assert.equal(unknownRow.layerx, null);
assert.notEqual(unknownRow.layerx, 0n);

assert.throws(() =>
  decodePxAccountBalances({
    ...balances,
    balances: [{ asset_id: NATIVE, denom: null, custody: null, paxeer: null }],
  }),
);
assert.throws(() =>
  decodePxAccountBalances({ ...balances, joined_limit: null }),
);
assert.throws(() =>
  decodePxAccountBalances({ account: identities, joined_limit: 16 }),
);
assert.throws(() =>
  decodePxResolvedIdentities({ ...identities, bound: "true" }),
);
assert.throws(() =>
  decodePxResolvedIdentities({
    ...identities,
    evm_address: `0x${"11".repeat(32)}`,
  }),
);
assert.equal(
  decodePxResolvedIdentities({
    ...identities,
    evm_address: null,
    layerx_account: null,
    bound: false,
  }).evmAddress,
  null,
);
assert.equal(PX_MAXIMUM_JOINED_ASSETS, 1024);
assert.throws(() =>
  decodePxAccountBalances({
    ...balances,
    balances: new Array<unknown>(PX_MAXIMUM_JOINED_ASSETS + 1).fill(
      balances.balances[1],
    ),
  }),
);

const join = decodePxAccountJoin({
  account: identities,
  paxeer: { address: EVM, balance: "0x0de0b6b3a7640000", nonce: "0x07" },
  layerx: { sequence: "3" },
});
assert.equal(join.paxeer?.balance, 1000000000000000000n);
assert.equal(join.paxeer?.nonce, 7n);
assert.deepEqual(join.layerx, { sequence: "3" });
const halfJoin = decodePxAccountJoin({
  account: identities,
  paxeer: null,
  layerx: null,
});
assert.equal(halfJoin.paxeer, null);
assert.equal(halfJoin.layerx, null);

const assets = decodePxAssetTable({
  assets: [
    { asset_id: NATIVE, layerx: { symbol: "LXP" }, paxeer: custody },
    { asset_id: UNJOINED, layerx: null, paxeer: null },
  ],
  joined_limit: 16,
});
assert.equal(assets.joinedLimit, 16n);
assert.equal(assets.assets[0]!.paxeer?.denom, "ulxp");
assert.equal(assets.assets[1]!.paxeer, null);
assert.equal(assets.assets[1]!.layerx, null);

const networkDocument = {
  network_id: "eip155:125",
  paxeer: { chain_id: "0x7d", latest_block: "0x2b67" },
  layerx: { node_info: { network: "eip155:125" } },
  latest_committed_height: "11110",
};
const maximumHeight = "18446744073709551615";
const networkCases = [
  networkDocument,
  {
    ...networkDocument,
    latest_committed_height: "0",
    layerx: { node_info: null },
  },
  { ...networkDocument, latest_committed_height: "0x2b67", layerx: null },
  {
    ...networkDocument,
    paxeer: { chain_id: 125, latest_block: maximumHeight },
    latest_committed_height: maximumHeight,
  },
];
const invalidNetworks: readonly {
  readonly value: unknown;
  readonly ts: string;
  readonly py: string;
}[] = [
  {
    value: {
      network_id: networkDocument.network_id,
      paxeer: networkDocument.paxeer,
      layerx: null,
      anchor: null,
    },
    ts: "Incomplete gateway document",
    py: "incomplete-px-document",
  },
  {
    value: { ...networkDocument, anchor: null },
    ts: "Invalid gateway finality",
    py: "invalid-px-finality",
  },
  {
    value: { ...networkDocument, latest_committed_height: "11112" },
    ts: "Invalid gateway finality",
    py: "invalid-px-finality",
  },
  {
    value: { ...networkDocument, paxeer: { chain_id: "0x7d" } },
    ts: "Incomplete gateway document",
    py: "incomplete-px-document",
  },
  {
    value: { ...networkDocument, network_id: "" },
    ts: "Invalid gateway text",
    py: "invalid-px-text",
  },
  ...[null, true, -1, 1.5, "-1", "01", "18446744073709551616"].map(
    (height) => ({
      value: { ...networkDocument, latest_committed_height: height },
      ts:
        height === "18446744073709551616"
          ? "Invalid gateway count"
          : "Invalid gateway quantity",
      py:
        height === "18446744073709551616"
          ? "invalid-px-count"
          : "invalid-px-quantity",
    }),
  ),
];

await test("network_finality_from_commit", () => {
  const network = decodePxNetworkHead(networkDocument);
  assert.deepEqual(network, {
    networkId: "eip155:125",
    paxeer: { chainId: 125n, latestBlock: 11111n },
    layerx: { node_info: { network: "eip155:125" } },
    latestCommittedHeight: 11110n,
  });
  assert.equal("anchor" in network, false);
  assert.equal(Object.isFrozen(network), true);
  assert.equal(Object.isFrozen(network.paxeer), true);
  assert.equal(decodePxNetworkHead(networkCases[1]).latestCommittedHeight, 0n);
  assert.deepEqual(decodePxNetworkHead(networkCases[1]).layerx, {
    node_info: null,
  });
  assert.equal(decodePxNetworkHead(networkCases[2]).layerx, null);
  assert.equal(
    decodePxNetworkHead(networkCases[2]).latestCommittedHeight,
    11111n,
  );
  assert.equal(
    decodePxNetworkHead(networkCases[3]).latestCommittedHeight,
    18446744073709551615n,
  );
  for (const { value, ts } of invalidNetworks)
    assert.throws(() => decodePxNetworkHead(value), { message: ts });
  assert.throws(
    () =>
      decodePxNetworkHead({
        ...networkDocument,
        latest_committed_height: Number.MAX_SAFE_INTEGER + 1,
      }),
    { message: "Invalid gateway quantity" },
  );
});

const receiptFixture = JSON.parse(
  readFileSync(
    new URL(
      "../../../../../platform/sdk/conformance/fixtures/receipt-positive-v2.json",
      import.meta.url,
    ),
    "utf8",
  ),
) as {
  authorized_batch: { sequencer_public_key_hex: string };
};
const publicKey = receiptFixture.authorized_batch.sequencer_public_key_hex;
const receiptKey = {
  keyId: Buffer.alloc(32, 11).toString("base64"),
  publicKey: Buffer.from(publicKey, "hex").toString("base64"),
  firstHeight: "100",
  lastHeight: "199",
};
const rotatedKey = {
  ...receiptKey,
  keyId: Buffer.alloc(32, 12).toString("base64"),
  firstHeight: "200",
  lastHeight: maximumHeight,
};
const receiptKeyCases = [
  [],
  [receiptKey, rotatedKey],
  [receiptKey, { ...rotatedKey, firstHeight: "100" }],
  [{ ...receiptKey, firstHeight: "0", lastHeight: "0" }],
];
const invalidReceiptKeys: readonly {
  readonly value: unknown;
  readonly ts: string;
  readonly py: string;
}[] = [
  ...[null, {}, { keys: [receiptKey] }, "keys"].map((value) => ({
    value,
    ts: "Invalid gateway receipt keys",
    py: "invalid-px-receipt-keys",
  })),
  { value: [null], ts: "Invalid gateway document", py: "invalid-px-document" },
  {
    value: [{}],
    ts: "Incomplete gateway document",
    py: "incomplete-px-document",
  },
  {
    value: [{ ...receiptKey, extra: 1 }],
    ts: "Invalid gateway receipt key",
    py: "invalid-px-receipt-key",
  },
  {
    value: [receiptKey, receiptKey],
    ts: "Invalid gateway receipt key",
    py: "invalid-px-receipt-key",
  },
  {
    value: [{ ...receiptKey, firstHeight: "200" }],
    ts: "Invalid gateway receipt key range",
    py: "invalid-px-receipt-key-range",
  },
  ...["keyId", "publicKey"].flatMap((field) =>
    [
      null,
      "00".repeat(32),
      "!".repeat(44),
      Buffer.alloc(31, 11).toString("base64"),
      Buffer.alloc(33, 11).toString("base64"),
      receiptKey.keyId.slice(0, -1),
    ].map((bytes) => ({
      value: [{ ...receiptKey, [field]: bytes }],
      ts: "Invalid gateway receipt key bytes",
      py: "invalid-px-receipt-key-bytes",
    })),
  ),
  ...["keyId", "publicKey"].map((field) => ({
    value: [{ ...receiptKey, [field]: Buffer.alloc(32).toString("base64") }],
    ts: "Invalid gateway receipt key",
    py: "invalid-px-receipt-key",
  })),
  ...["firstHeight", "lastHeight"].flatMap((field) =>
    [null, true, -1, 1.5, "-1", "01", "18446744073709551616"].map((height) => ({
      value: [{ ...receiptKey, [field]: height }],
      ts:
        height === "18446744073709551616"
          ? "Invalid gateway count"
          : "Invalid gateway quantity",
      py:
        height === "18446744073709551616"
          ? "invalid-px-count"
          : "invalid-px-quantity",
    })),
  ),
];

await test("receipt_keys_shape", () => {
  assert.deepEqual(PX_METHODS, [
    "px_resolveAccount",
    "px_getAccount",
    "px_getBalances",
    "px_listAssets",
    "px_getNetwork",
    "px_getReceiptKeys",
    "px_getHistory",
    "px_getUnifiedHistory",
  ]);
  const keys = decodePxReceiptKeys(receiptKeyCases[1]);
  assert.deepEqual(keys, [
    { keyId: "0b".repeat(32), publicKey, firstHeight: 100n, lastHeight: 199n },
    {
      keyId: "0c".repeat(32),
      publicKey,
      firstHeight: 200n,
      lastHeight: 18446744073709551615n,
    },
  ]);
  assert.equal(Object.isFrozen(keys), true);
  assert.equal(Object.isFrozen(keys[0]), true);
  assert.deepEqual(decodePxReceiptKeys([]), []);
  assert.equal(decodePxReceiptKeys(receiptKeyCases[2])[1]?.firstHeight, 100n);
  assert.equal(decodePxReceiptKeys(receiptKeyCases[3])[0]?.lastHeight, 0n);
  for (const { value, ts } of invalidReceiptKeys)
    assert.throws(() => decodePxReceiptKeys(value), { message: ts });
  assert.throws(
    () =>
      decodePxReceiptKeys([
        { ...receiptKey, lastHeight: Number.MAX_SAFE_INTEGER + 1 },
      ]),
    { message: "Invalid gateway quantity" },
  );
  const nonCanonical = receiptKey.keyId.slice(0, -2) + "t=";
  assert.throws(
    () => decodePxReceiptKeys([{ ...receiptKey, keyId: nonCanonical }]),
    { message: "Invalid gateway receipt key bytes" },
  );
});

await test("python_px_matches_ts", () => {
  const script = `
import json
import sys
from dataclasses import FrozenInstanceError
from layerx_sdk.px import PX_METHODS, PxClient, decode_px_network_head, decode_px_receipt_keys

document = json.load(sys.stdin)
assert list(PX_METHODS) == document["methods"]
assert callable(PxClient.get_network)
assert callable(PxClient.get_receipt_keys)
networks = []
for value in document["networks"]:
    network = decode_px_network_head(value)
    assert not hasattr(network, "anchor")
    networks.append({"networkId": network.network_id, "paxeer": {"chainId": str(network.paxeer.chain_id), "latestBlock": str(network.paxeer.latest_block)}, "layerx": None if network.layerx is None else dict(network.layerx), "latestCommittedHeight": str(network.latest_committed_height)})
    try:
        network.latest_committed_height = 0
    except FrozenInstanceError:
        pass
    else:
        raise AssertionError("mutable network")
keys = []
for value in document["keys"]:
    entries = decode_px_receipt_keys(value)
    assert isinstance(entries, tuple)
    keys.append([{"keyId": key.key_id, "publicKey": key.public_key, "firstHeight": str(key.first_height), "lastHeight": str(key.last_height)} for key in entries])
    for key in entries:
        try:
            key.first_height = 0
        except FrozenInstanceError:
            pass
        else:
            raise AssertionError("mutable key")
for group, decoder in [("invalidNetworks", decode_px_network_head), ("invalidKeys", decode_px_receipt_keys)]:
    for case in document[group]:
        try:
            decoder(case["value"])
        except ValueError as error:
            assert str(error) == case["py"], (str(error), case["py"])
        else:
            raise AssertionError("invalid document accepted")
print(json.dumps({"networks": networks, "keys": keys}))
`;
  const result = spawnSync("python3", ["-B", "-c", script], {
    env: {
      ...process.env,
      PYTHONPATH: fileURLToPath(new URL("../../../python/", import.meta.url)),
      PYTHONDONTWRITEBYTECODE: "1",
    },
    input: JSON.stringify({
      methods: PX_METHODS,
      networks: networkCases,
      keys: receiptKeyCases,
      invalidNetworks,
      invalidKeys: invalidReceiptKeys,
    }),
    encoding: "utf8",
    timeout: 30_000,
    maxBuffer: 1024 * 1024,
  });
  assert.equal(result.error, undefined);
  assert.equal(result.signal, null);
  assert.equal(result.status, 0, result.stderr);
  const expected = {
    networks: networkCases.map(decodePxNetworkHead),
    keys: receiptKeyCases.map(decodePxReceiptKeys),
  };
  assert.deepEqual(
    JSON.parse(result.stdout),
    JSON.parse(
      JSON.stringify(expected, (_key, value: unknown) =>
        typeof value === "bigint" ? value.toString() : value,
      ),
    ),
  );
});

const requests: { readonly method: unknown; readonly params: unknown }[] = [];
const gateway = http.createServer((request, response) => {
  const chunks: Buffer[] = [];
  request.on("data", (chunk: Buffer) => chunks.push(Buffer.from(chunk)));
  request.on("end", () => {
    const body = JSON.parse(Buffer.concat(chunks).toString("utf8")) as {
      jsonrpc: string;
      id: string;
      method: string;
      params: unknown;
    };
    assert.equal(request.method, "POST");
    assert.equal(request.headers["content-type"], "application/json");
    assert.equal(request.url, "/rpc");
    assert.equal(body.jsonrpc, "2.0");
    requests.push({ method: body.method, params: body.params });
    const answer =
      body.method === "px_getBalances"
        ? { jsonrpc: "2.0", id: body.id, result: balances }
        : {
            jsonrpc: "2.0",
            id: body.id,
            error: {
              code: -32001,
              message: "Paxeer read unavailable",
              data: { code: "paxeer_unreachable" },
            },
          };
    const encoded = Buffer.from(JSON.stringify(answer), "utf8");
    response.writeHead(200, {
      "Content-Type": "application/json",
      "Content-Length": encoded.length,
    });
    response.end(encoded);
  });
});
gateway.listen(0, "127.0.0.1");
await once(gateway, "listening");
const address = gateway.address();
assert(
  address !== null && typeof address === "object",
  "gateway listener missing",
);
try {
  const client = new PxClient({
    endpoint: `http://127.0.0.1:${address.port}/rpc`,
  });
  const table = await client.getBalances(ACCOUNT.toUpperCase());
  assert.equal(table.balances.length, 2);
  assert.equal(table.balances[0]!.paxeer?.amount, 30n);
  assert.equal(table.balances[1]!.paxeer, null);
  assert.equal(table.balances[1]!.denom, null);
  assert.deepEqual(requests[0], {
    method: "px_getBalances",
    params: [ACCOUNT],
  });

  await assert.rejects(
    client.getNetwork(),
    (error: unknown) =>
      error instanceof JsonRpcError &&
      error.code === -32001 &&
      error.message === "Paxeer read unavailable" &&
      (error.data as { code: string }).code === "paxeer_unreachable",
  );
  await assert.rejects(
    client.getReceiptKeys(),
    (error: unknown) =>
      error instanceof JsonRpcError &&
      error.code === -32001 &&
      error.message === "Paxeer read unavailable" &&
      (error.data as { code: string }).code === "paxeer_unreachable",
  );
  await assert.rejects(
    client.listAssets(),
    (error: unknown) => error instanceof JsonRpcError && error.code === -32001,
  );
  await assert.rejects(
    client.resolveAccount(DID),
    (error: unknown) => error instanceof JsonRpcError && error.code === -32001,
  );
  await assert.rejects(
    client.getAccount(EVM),
    (error: unknown) => error instanceof JsonRpcError && error.code === -32001,
  );
  assert.deepEqual(
    requests.map((entry) => entry.method),
    [
      "px_getBalances",
      "px_getNetwork",
      "px_getReceiptKeys",
      "px_listAssets",
      "px_resolveAccount",
      "px_getAccount",
    ],
  );
  assert.deepEqual(requests[1]!.params, []);
  assert.deepEqual(requests[2]!.params, []);
  assert.deepEqual(requests[4]!.params, [DID]);
  assert.deepEqual(requests[5]!.params, [EVM]);
} finally {
  gateway.close();
  await once(gateway, "close");
}

assert.throws(() => new PxClient({ endpoint: "http://example.com/rpc" }));
assert.throws(
  () => new PxClient({ endpoint: "https://user:secret@example.com/rpc" }),
);
assert.throws(() => new PxClient({ endpoint: "wss://example.com/rpc" }));
assert.throws(() => new PxClient({ endpoint: "https://example.com/rpc#head" }));
await assert.rejects(
  new PxClient({ endpoint: "http://127.0.0.1:1/rpc" }).getNetwork(),
);
await assert.rejects(
  new PxClient({ endpoint: "https://example.com/rpc" }).getBalances(
    "not-an-account",
  ),
);

// Recorded gateway answers for the three history reads, as the gateway joins
// the indexer's rows with its /v1/assets records and the LayerX registry.
const LXP = "11".repeat(32);
const ALICE = "aa".repeat(32);
const EVE = `0x${"ee".repeat(20)}`;
const FRANK = `0x${"ff".repeat(20)}`;
const POINTER = `0x${"cc".repeat(20)}`;
const lxpMetadata = {
  asset: LXP,
  chain: "layerx",
  kind: "layerx",
  address: null,
  denom: null,
  symbol: "LXP",
  decimals: 6,
  native_id: LXP,
  pointer: null,
  metadata: { supply: "100" },
};
const pointerMetadata = {
  asset: `evm:${POINTER}`,
  chain: "paxeer",
  kind: "pointer",
  address: POINTER,
  denom: "ulxp",
  symbol: "LXP",
  decimals: 6,
  native_id: LXP,
  pointer: POINTER,
  metadata: {},
};
const nativeMetadata = {
  asset: "evm:native",
  chain: "paxeer",
  kind: "native",
  address: null,
  denom: null,
  symbol: null,
  decimals: 18,
  native_id: null,
  pointer: null,
  metadata: {},
};
const custodyRow = {
  id: "8",
  height_or_seq: "11",
  chain: "paxeer",
  kind: "custody_deposit",
  direction: "in",
  account: ALICE,
  counterparty: EVE,
  asset: LXP,
  amount: "9",
  tx_id: "custody_deposit-11",
  ordinal: "0",
  final: false,
  decoded: { position: "11" },
  asset_metadata: lxpMetadata,
};
const pointerRow = {
  id: "6",
  height_or_seq: "11",
  chain: "paxeer",
  kind: "pointer_transfer",
  direction: "in",
  account: EVE,
  counterparty: FRANK,
  asset: `evm:${POINTER}`,
  amount: "3",
  tx_id: "pointer_transfer-11",
  ordinal: "0",
  final: false,
  decoded: { position: "11" },
  asset_metadata: pointerMetadata,
};
const creditRow = {
  id: "5",
  height_or_seq: "2",
  chain: "layerx",
  kind: "lxp_credit",
  direction: "in",
  account: ALICE,
  counterparty: null,
  asset: LXP,
  amount: "7",
  tx_id: "lxp_credit-2",
  ordinal: "0",
  final: true,
  decoded: { position: "2" },
  asset_metadata: lxpMetadata,
};
const nativeRow = {
  id: "3",
  height_or_seq: "10",
  chain: "paxeer",
  kind: "native_transfer",
  direction: "out",
  account: EVE,
  counterparty: FRANK,
  asset: "evm:native",
  amount: "1000000000000000000",
  tx_id: "native_transfer-10",
  ordinal: "0",
  final: false,
  decoded: { position: "10" },
  asset_metadata: nativeMetadata,
};
const unknownAssetRow = {
  id: "11",
  height_or_seq: "12",
  chain: "paxeer",
  kind: "erc20_transfer",
  direction: "in",
  account: EVE,
  counterparty: FRANK,
  asset: `evm:0x${"00".repeat(18)}0abc`,
  amount:
    "115792089237316195423570985008687907853269984665640564039457584007913129639935",
  tx_id: "erc20_transfer-12",
  ordinal: "0",
  final: false,
  decoded: {},
  asset_metadata: null,
};
const recordedLayerx = {
  items: [custodyRow, creditRow],
  next_cursor: "5",
  account: ALICE,
};
const recordedPaxeer = {
  items: [unknownAssetRow, pointerRow, nativeRow],
  next_cursor: null,
  account: EVE,
};
const recordedUnified = {
  items: [
    { ...custodyRow, side: "layerx" },
    { ...pointerRow, side: "paxeer" },
    { ...creditRow, side: "layerx" },
  ],
  next_cursor: "5",
  account: identities,
  accounts: [
    { side: "paxeer", account: EVE },
    { side: "layerx", account: ALICE },
  ],
};

assert.deepEqual(historyParams(ALICE), [ALICE, null, null, null]);
assert.deepEqual(
  historyParams(EVE, { cursor: "42", limit: 7, kind: "erc20_transfer" }),
  [EVE, "42", 7, "erc20_transfer"],
);
for (const refused of [
  { cursor: "" },
  { cursor: "1&limit=500" },
  { cursor: "x".repeat(33) },
  { limit: 0 },
  { limit: 101 },
  { limit: 1.5 },
  { kind: "Transfer" },
  { kind: "" },
]) {
  assert.throws(() => historyParams(EVE, refused));
}
assert.equal(HISTORY_DEFAULT_LIMIT, 50);

const layerxPage = decodeLayerxHistoryPage(recordedLayerx);
assert.equal(layerxPage.account, ALICE);
assert.equal(layerxPage.nextCursor, "5");
assert.deepEqual(
  layerxPage.items.map((row) => row.id),
  [8n, 5n],
);
assert.equal(layerxPage.items[0]!.chain, "paxeer");
assert.equal(layerxPage.items[0]!.side, null);
assert.equal(layerxPage.items[1]!.counterparty, null);
assert.equal(layerxPage.items[1]!.amount, 7n);
assert.equal(layerxPage.items[1]!.final, true);
assert.equal(layerxPage.items[1]!.assetMetadata?.symbol, "LXP");
assert.equal(layerxPage.items[1]!.assetMetadata?.decimals, 6);
assert.equal(layerxPage.items[1]!.assetMetadata?.nativeId, LXP);
assert.deepEqual(layerxPage.items[1]!.assetMetadata?.metadata, {
  supply: "100",
});

const paxeerPage = decodePaxeerHistoryPage(recordedPaxeer);
assert.equal(paxeerPage.nextCursor, null);
assert.equal(paxeerPage.items[0]!.assetMetadata, null);
assert.equal(paxeerPage.items[0]!.amount, (1n << 256n) - 1n);
assert.equal(paxeerPage.items[1]!.assetMetadata?.pointer, POINTER);
assert.equal(paxeerPage.items[1]!.assetMetadata?.denom, "ulxp");
assert.equal(paxeerPage.items[2]!.assetMetadata?.decimals, 18);
assert.equal(paxeerPage.items[2]!.assetMetadata?.symbol, null);
assert.equal(paxeerPage.items[2]!.direction, "out");

const unifiedPage = decodeUnifiedHistoryPage(recordedUnified);
assert.deepEqual(
  unifiedPage.items.map((row) => [row.id, row.side, row.chain]),
  [
    [8n, "layerx", "paxeer"],
    [6n, "paxeer", "paxeer"],
    [5n, "layerx", "layerx"],
  ],
);
assert.deepEqual(unifiedPage.accounts, [
  { side: "paxeer", account: EVE },
  { side: "layerx", account: ALICE },
]);
assert.equal(unifiedPage.nextCursor, "5");
assert.equal(unifiedPage.account.evm_address, EVM);

assert.throws(() =>
  decodeUnifiedHistoryPage({
    ...recordedUnified,
    items: [recordedUnified.items[2], recordedUnified.items[0]],
  }),
);
assert.throws(() =>
  decodeUnifiedHistoryPage({ ...recordedUnified, items: [custodyRow] }),
);
assert.throws(() =>
  decodeLayerxHistoryPage({ ...recordedLayerx, account: EVE }),
);
assert.throws(() =>
  decodePaxeerHistoryPage({ ...recordedPaxeer, next_cursor: "1&x" }),
);
assert.throws(() =>
  decodeLayerxHistoryPage({ items: [], next_cursor: "5", account: ALICE }),
);
assert.throws(() =>
  decodeLayerxHistoryPage({
    ...recordedLayerx,
    items: [{ ...creditRow, amount: "-7" }],
  }),
);
assert.throws(() =>
  decodeLayerxHistoryPage({
    ...recordedLayerx,
    items: [{ ...creditRow, direction: "sideways" }],
  }),
);
assert.throws(() =>
  decodeLayerxHistoryPage({
    ...recordedLayerx,
    items: [
      { ...creditRow, asset_metadata: { ...lxpMetadata, asset: "evm:native" } },
    ],
  }),
);
assert.throws(() => decodeLayerxHistoryPage(recordedLayerx, 1));
assert.throws(() =>
  decodeHistoryAssetMetadata({ ...nativeMetadata, decimals: 256 }),
);
assert.deepEqual(
  decodeLayerxHistoryPage({ items: [], next_cursor: null, account: ALICE })
    .items,
  [],
);

const historyRequests: {
  readonly method: unknown;
  readonly params: unknown;
}[] = [];
const historyGateway = http.createServer((request, response) => {
  const chunks: Buffer[] = [];
  request.on("data", (chunk: Buffer) => chunks.push(Buffer.from(chunk)));
  request.on("end", () => {
    const body = JSON.parse(Buffer.concat(chunks).toString("utf8")) as {
      id: string;
      method: string;
      params: unknown;
    };
    historyRequests.push({ method: body.method, params: body.params });
    const result =
      body.method === "lx_getHistory"
        ? recordedLayerx
        : body.method === "px_getHistory"
          ? recordedPaxeer
          : recordedUnified;
    const encoded = Buffer.from(
      JSON.stringify({ jsonrpc: "2.0", id: body.id, result }),
      "utf8",
    );
    response.writeHead(200, {
      "Content-Type": "application/json",
      "Content-Length": encoded.length,
    });
    response.end(encoded);
  });
});
historyGateway.listen(0, "127.0.0.1");
await once(historyGateway, "listening");
const historyAddress = historyGateway.address();
assert(
  historyAddress !== null && typeof historyAddress === "object",
  "history gateway listener missing",
);
try {
  const px = new PxClient({
    endpoint: `http://127.0.0.1:${historyAddress.port}/rpc`,
  });
  const lx = new JsonRpcClient(`http://127.0.0.1:${historyAddress.port}`);
  const unified = await px.getUnifiedHistory(DID.toUpperCase(), { limit: 3 });
  assert.deepEqual(
    unified.items.map((row) => row.id),
    [8n, 6n, 5n],
  );
  const paxeer = await px.getHistory(EVE.toUpperCase().replace("0X", "0x"), {
    kind: "erc20_transfer",
  });
  assert.equal(paxeer.items.length, 3);
  const layerx = await lx.getHistory(ALICE, { cursor: "9", limit: 2 });
  assert.equal(layerx.nextCursor, "5");
  await assert.rejects(px.getUnifiedHistory(EVE, { limit: 2 }));
  await assert.rejects(px.getHistory(ALICE));
  assert.throws(() => lx.getHistory(EVE));
  assert.deepEqual(historyRequests, [
    { method: "px_getUnifiedHistory", params: [DID, null, 3, null] },
    { method: "px_getHistory", params: [EVE, null, null, "erc20_transfer"] },
    { method: "lx_getHistory", params: [ALICE, "9", 2, null] },
    { method: "px_getUnifiedHistory", params: [EVE, null, 2, null] },
  ]);
} finally {
  historyGateway.close();
  await once(historyGateway, "close");
}
