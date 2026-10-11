import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { test } from "node:test";

import { ed25519 } from "@noble/curves/ed25519.js";
import { secp256k1 } from "@noble/curves/secp256k1.js";
import { keccak_256 } from "@noble/hashes/sha3.js";

import {
  AccountDerivationError,
  LAYERX_EXCHANGE_PRECOMPILE,
  PERPS_ACTIVITY_TYPES,
  SPOT_ACTIVITY_TYPES,
  TradingPayloadError,
  TradingWriter,
  abiSelector,
  buildPerpsActivity,
  buildSpotActivity,
  decodePerpsActivity,
  decodeSpotActivity,
  encodePerpsActivity,
  encodeSpotActivity,
  deriveFromSeed,
  perpsActivityType,
  spotActivityType,
  type BindFees,
  type PerpsCallActivity,
  type PrecompileCall,
  type SpotCallActivity,
  type PerpsActivity,
  type SpotActivity,
} from "../src/index.js";

interface TradingVector {
  readonly name: string;
  readonly activity_type: number;
  readonly fields: Record<string, string | boolean | readonly string[]>;
  readonly bytes: string;
}

const fixture = JSON.parse(
  readFileSync(
    new URL(
      "../../../../../tests/fixtures/trading-payloads/vectors.json",
      import.meta.url,
    ),
    "utf8",
  ),
) as { readonly source: string; readonly vectors: readonly TradingVector[] };
assert.equal(fixture.source, "tests/modules/dump_trading_vectors.c");

const SMALL = new Set(["side", "kind", "time_in_force"]);

function camel(name: string): string {
  return name.replace(/_([a-z])/gu, (_, letter: string) =>
    letter.toUpperCase(),
  );
}

function activity(vector: TradingVector): Record<string, unknown> {
  const prefix = vector.name.startsWith("perps_") ? "perps_" : "spot_";
  let name = vector.name.slice(prefix.length);
  if (name.startsWith("order_place")) {
    name = "order_place";
  }
  const out: Record<string, unknown> = { activity: name };
  for (const [key, value] of Object.entries(vector.fields)) {
    if (typeof value === "boolean" || Array.isArray(value)) {
      out[camel(key)] = value;
    } else if (/^[0-9a-f]{64}$/u.test(value as string)) {
      out[camel(key)] = value;
    } else if (SMALL.has(key)) {
      out[camel(key)] = Number(value);
    } else {
      out[camel(key)] = BigInt(value as string);
    }
  }
  return out;
}

function bytes(value: string): Uint8Array {
  return Uint8Array.from(Buffer.from(value, "hex"));
}

function hex(value: Uint8Array): string {
  return Buffer.from(value).toString("hex");
}

function refuses(run: () => unknown, code: TradingPayloadError["code"]): void {
  assert.throws(
    run,
    (error: unknown) =>
      error instanceof TradingPayloadError && error.code === code,
  );
}

const perps = fixture.vectors.filter((vector) =>
  vector.name.startsWith("perps_"),
);
const spot = fixture.vectors.filter((vector) =>
  vector.name.startsWith("spot_"),
);
assert.equal(perps.length, Object.keys(PERPS_ACTIVITY_TYPES).length);
assert.equal(spot.length, 6);
assert.deepEqual(
  new Set(spot.map((vector) => vector.activity_type)),
  new Set(Object.values(SPOT_ACTIVITY_TYPES)),
);

const vector = (name: string): TradingVector => {
  const found = fixture.vectors.find((candidate) => candidate.name === name);
  assert.ok(found, name);
  return found;
};

test("kernel trading codecs retain every golden and refusal", () => {
  for (const vector of perps) {
    const typed = activity(vector) as PerpsActivity;
    assert.equal(perpsActivityType(typed), vector.activity_type, vector.name);
    assert.equal(hex(encodePerpsActivity(typed)), vector.bytes, vector.name);
    assert.deepEqual(
      decodePerpsActivity(vector.activity_type, bytes(vector.bytes)),
      typed,
      vector.name,
    );
  }

  for (const vector of spot) {
    const typed = activity(vector) as SpotActivity;
    assert.equal(spotActivityType(typed), vector.activity_type, vector.name);
    assert.equal(hex(encodeSpotActivity(typed)), vector.bytes, vector.name);
    assert.deepEqual(
      decodeSpotActivity(vector.activity_type, bytes(vector.bytes)),
      typed,
      vector.name,
    );
  }

  const order = vector("perps_order_place");
  const orderBytes = bytes(order.bytes);
  refuses(
    () => decodePerpsActivity(order.activity_type, orderBytes.subarray(1)),
    "length",
  );
  refuses(
    () => decodePerpsActivity(0x0006000c, orderBytes),
    "unknown_activity",
  );
  refuses(
    () => decodePerpsActivity(0x000a0002, orderBytes),
    "unknown_activity",
  );
  const badSide = orderBytes.slice();
  badSide[96] = 3;
  refuses(
    () => decodePerpsActivity(order.activity_type, badSide),
    "non_canonical",
  );
  const zeroQuantity = orderBytes.slice();
  zeroQuantity.fill(0, 113);
  refuses(
    () => decodePerpsActivity(order.activity_type, zeroQuantity),
    "non_canonical",
  );

  const market = vector("perps_market_create");
  const marketBytes = bytes(market.bytes);
  const padding = marketBytes.slice();
  padding[32 * 7 + 16 * 4 + 4 * 6 + 8 * 2 + 16 * 2 + 1 + 32 * 7] = 1;
  refuses(
    () => decodePerpsActivity(market.activity_type, padding),
    "non_canonical",
  );
  const typedMarket = decodePerpsActivity(market.activity_type, marketBytes);
  assert.equal(typedMarket.activity, "market_create");
  refuses(
    () =>
      encodePerpsActivity({
        ...typedMarket,
        initialMarginRatioBps: typedMarket.maintenanceMarginRatioBps,
      }),
    "parameter_bounds",
  );
  const halted = marketBytes.slice();
  halted[621] = 2;
  refuses(
    () => decodePerpsActivity(market.activity_type, halted),
    "non_canonical",
  );

  const adl = activity(vector("perps_adl")) as Extract<
    PerpsActivity,
    { activity: "adl" }
  >;
  assert.ok(adl.positionIds.length > 1);
  refuses(
    () =>
      encodePerpsActivity({
        ...adl,
        positionIds: [...adl.positionIds].reverse(),
      }),
    "unsorted_sequence",
  );

  const open = activity(vector("perps_position_open")) as Extract<
    PerpsActivity,
    { activity: "position_open" }
  >;
  refuses(
    () => encodePerpsActivity({ ...open, entryNotional: 1n }),
    "non_canonical",
  );

  const limit = activity(vector("spot_order_place_limit")) as Extract<
    SpotActivity,
    { activity: "order_place" }
  >;
  refuses(() => encodeSpotActivity({ ...limit, price: 0n }), "non_canonical");
  refuses(
    () => encodeSpotActivity({ ...limit, quoteAccountId: limit.baseAccountId }),
    "non_canonical",
  );
  refuses(() => encodeSpotActivity({ ...limit, kind: 2 }), "non_canonical");
  const spotBytes = bytes(vector("spot_order_place_limit").bytes);
  const badKind = spotBytes.slice();
  badKind[129] = 3;
  refuses(
    () => decodeSpotActivity(SPOT_ACTIVITY_TYPES.order_place, badKind),
    "non_canonical",
  );
  refuses(() => decodeSpotActivity(0x000a0006, spotBytes), "unknown_activity");
  refuses(
    () =>
      decodeSpotActivity(SPOT_ACTIVITY_TYPES.market_halt, new Uint8Array(32)),
    "non_canonical",
  );
});

function word(value: string | bigint | number | boolean): string {
  if (typeof value === "string") return value;
  return BigInt(value).toString(16).padStart(64, "0");
}

function calldata(
  selector: string,
  ...values: readonly (string | bigint | number | boolean)[]
): string {
  return selector + values.map(word).join("");
}

function exactCall(call: PrecompileCall, data: string): void {
  assert.deepEqual(call, { to: LAYERX_EXCHANGE_PRECOMPILE, data, value: 0n });
  assert.deepEqual(Object.keys(call).sort(), ["data", "to", "value"]);
}

function perpsInput<T extends PerpsActivity["activity"]>(
  name: string,
  kind: T,
): Extract<PerpsActivity, { activity: T }> {
  const decoded = decodePerpsActivity(
    vector(name).activity_type,
    bytes(vector(name).bytes),
  );
  assert.equal(decoded.activity, kind);
  return decoded as Extract<PerpsActivity, { activity: T }>;
}

function spotInput<T extends SpotActivity["activity"]>(
  name: string,
  kind: T,
): Extract<SpotActivity, { activity: T }> {
  const decoded = decodeSpotActivity(
    vector(name).activity_type,
    bytes(vector(name).bytes),
  );
  assert.equal(decoded.activity, kind);
  return decoded as Extract<SpotActivity, { activity: T }>;
}

test("perps_builder_is_abi", () => {
  const order = perpsInput("perps_order_place", "order_place");
  const { orderId, ...placed } = order;
  for (const timeInForce of [0, 1, 2, 3] as const) {
    exactCall(
      buildPerpsActivity({ ...placed, timeInForce }),
      calldata(
        "0xf9835781",
        order.marketId,
        order.ownerAccountId,
        order.side,
        order.price,
        order.quantity,
        timeInForce,
      ),
    );
  }
  assert.equal(
    buildPerpsActivity(placed).data,
    buildPerpsActivity({ ...placed, timeInForce: 0 }).data,
  );
  assert.equal(
    buildPerpsActivity(order).data,
    buildPerpsActivity({ ...placed, orderId: "not-an-id" }).data,
  );
  assert.ok(!buildPerpsActivity(order).data.includes(orderId));
  const open = perpsInput("perps_position_open", "position_open");
  const { positionId, entryNotional, ...opening } = open;
  exactCall(
    buildPerpsActivity(opening),
    calldata(
      "0xb14bfbe6",
      open.marketId,
      open.marginAccountId,
      open.side,
      open.size,
      open.marginAmount,
    ),
  );
  exactCall(
    buildPerpsActivity({
      ...opening,
      positionId: open.marginAccountId,
      entryNotional,
    }),
    buildPerpsActivity(opening).data,
  );
  assert.ok(!buildPerpsActivity(opening).data.includes(positionId));
  const increase = perpsInput("perps_position_increase", "position_increase");
  const { notionalDelta, ...increasing } = increase;
  exactCall(
    buildPerpsActivity(increasing),
    calldata(
      "0xf9f467f3",
      increase.marketId,
      increase.positionId,
      increase.sizeDelta,
      increase.marginAmount,
    ),
  );
  assert.equal(notionalDelta, 0n);
  const cancel = perpsInput("perps_order_cancel", "order_cancel");
  exactCall(
    buildPerpsActivity(cancel),
    calldata("0xa47d9d33", cancel.marketId, cancel.orderId),
  );
  const close = perpsInput("perps_position_close", "position_close");
  exactCall(
    buildPerpsActivity(close),
    calldata("0x9c49d053", close.marketId, close.positionId),
  );
  const funding = perpsInput("perps_funding_tick", "funding_tick");
  exactCall(
    buildPerpsActivity(funding),
    calldata("0x2b6edb1e", funding.marketId),
  );
  const liquidation = perpsInput("perps_liquidate", "liquidate");
  exactCall(
    buildPerpsActivity(liquidation),
    calldata(
      "0xd5b3e4d9",
      liquidation.marketId,
      liquidation.positionId,
      liquidation.liquidatorAccountId,
    ),
  );
  const halt = perpsInput("perps_market_halt", "market_halt");
  exactCall(
    buildPerpsActivity(halt),
    calldata("0x141053a4", halt.marketId, true),
  );
  exactCall(
    buildPerpsActivity({ ...halt, halted: false }),
    calldata("0x141053a4", halt.marketId, false),
  );
});

test("spot_builder_is_abi", () => {
  const market = spotInput("spot_market_create", "market_create");
  exactCall(
    buildSpotActivity(market),
    calldata(
      "0xdba301bc",
      market.marketId,
      market.baseAsset,
      market.quoteAsset,
      market.administrator,
      market.tickSize,
      market.lotSize,
    ),
  );
  for (const name of ["spot_order_place_limit", "spot_order_place_market"]) {
    const order = spotInput(name, "order_place");
    const { orderId, ...placed } = order;
    exactCall(
      buildSpotActivity(placed),
      calldata(
        "0x91a861ce",
        order.marketId,
        order.baseAccountId,
        order.quoteAccountId,
        order.side,
        order.kind,
        order.timeInForce,
        order.price,
        order.quantity,
      ),
    );
    assert.equal(
      buildSpotActivity(order).data,
      buildSpotActivity({ ...placed, orderId: "not-an-id" }).data,
    );
    assert.ok(!buildSpotActivity(placed).data.includes(orderId));
  }
  const cancel = spotInput("spot_order_cancel", "order_cancel");
  exactCall(
    buildSpotActivity(cancel),
    calldata("0x7f94da73", cancel.marketId, cancel.orderId),
  );
  const halt = spotInput("spot_market_halt", "market_halt");
  exactCall(
    buildSpotActivity(halt),
    calldata("0x9b89e0a4", halt.marketId, true),
  );
  exactCall(
    buildSpotActivity(spotInput("spot_market_resume", "market_resume")),
    calldata("0x9b89e0a4", halt.marketId, false),
  );
});

test("perps market creation encodes the dynamic tuple in contract order", () => {
  const market = perpsInput("perps_market_create", "market_create");
  const signature =
    "createPerpsMarket((bytes32,bytes32,bytes32,bytes32,bytes32,bytes32,bytes32,uint256,uint256,uint256,uint256,uint32,uint32,uint32,uint32,uint32,uint32,uint64,uint64,uint256,uint256,bytes32[],uint32))";
  const expected = calldata(
    abiSelector(signature),
    32n,
    market.marketId,
    market.quoteAsset,
    market.administrator,
    market.liquidityAccountId,
    market.longFundingAccountId,
    market.shortFundingAccountId,
    market.insuranceAccountId,
    market.contractSize,
    market.tickSize,
    market.lotSize,
    market.priceScale,
    market.initialMarginRatioBps,
    market.maintenanceMarginRatioBps,
    market.liquidationFeeBps,
    market.liquidatorShareBps,
    market.maximumFundingRateBps,
    market.maximumDeviationBasisPoints,
    market.fundingIntervalMs,
    market.maximumOracleStalenessMs,
    market.minimumPrice,
    market.maximumPrice,
    736n,
    market.parameterVersion,
    BigInt(market.permittedOracleKeys.length),
    ...market.permittedOracleKeys,
  );
  exactCall(buildPerpsActivity(market), expected);
  const { halted, ...creating } = market;
  assert.equal(halted, false);
  exactCall(buildPerpsActivity(creating), expected);
  assert.equal((expected.length - 2) / 2, 868);
  const keys = Array.from({ length: 9 }, (_, index) => word(BigInt(index + 1)));
  for (const count of [1, 8]) {
    const permittedOracleKeys = keys.slice(0, count);
    exactCall(
      buildPerpsActivity({ ...market, permittedOracleKeys }),
      expected.slice(0, 10 + 24 * 64) +
        word(count) +
        permittedOracleKeys.join(""),
    );
  }
  refuses(
    () => buildPerpsActivity({ ...market, halted: true }),
    "non_canonical",
  );
  refuses(
    () => buildPerpsActivity({ ...market, parameterVersion: 1n << 32n }),
    "non_canonical",
  );
  refuses(
    () =>
      buildPerpsActivity({ ...market, maximumOracleStalenessMs: 1n << 64n }),
    "non_canonical",
  );
  refuses(
    () => buildPerpsActivity({ ...market, maximumPrice: 1n << 128n }),
    "non_canonical",
  );
  refuses(
    () =>
      buildPerpsActivity({
        ...market,
        initialMarginRatioBps: market.maintenanceMarginRatioBps,
      }),
    "parameter_bounds",
  );
  refuses(
    () => buildPerpsActivity({ ...market, permittedOracleKeys: [] }),
    "parameter_bounds",
  );
  refuses(
    () => buildPerpsActivity({ ...market, permittedOracleKeys: keys }),
    "parameter_bounds",
  );
  refuses(
    () =>
      buildPerpsActivity({
        ...market,
        permittedOracleKeys: [market.marketId, market.marketId],
      }),
    "parameter_bounds",
  );
  refuses(
    () =>
      buildPerpsActivity({
        ...market,
        permittedOracleKeys: [...market.permittedOracleKeys].reverse(),
      }),
    "parameter_bounds",
  );
});

test("oracle calldata retains signed canonical observation bytes", () => {
  const oracle = perpsInput("perps_oracle_push", "oracle_push");
  const observation = bytes(vector("perps_oracle_push").bytes);
  const domain = new TextEncoder().encode("LXP:ORACLE:OBSERVATION:v1");
  const message = new Uint8Array(domain.length + observation.length);
  message.set(domain);
  message.set(observation, domain.length);
  const seed = bytes("01".repeat(32));
  const signature = ed25519.sign(message, seed);
  assert.equal(
    ed25519.verify(signature, message, ed25519.getPublicKey(seed)),
    true,
  );
  const expected =
    calldata("0x0e4d9382", 64n, 192n, 72n) +
    hex(observation).padEnd(192, "0") +
    word(64n) +
    hex(signature);
  exactCall(buildPerpsActivity({ ...oracle, signature }), expected);
  assert.equal((expected.length - 2) / 2, 292);
  for (const length of [0, 63, 65]) {
    const malformed = new Uint8Array(length);
    malformed.set(signature.subarray(0, Math.min(length, signature.length)));
    refuses(
      () => buildPerpsActivity({ ...oracle, signature: malformed }),
      "length",
    );
  }
  refuses(() => buildPerpsActivity(oracle as PerpsCallActivity), "length");
  refuses(
    () =>
      buildPerpsActivity({
        ...oracle,
        signature,
        observationSequence: 1n << 64n,
      }),
    "non_canonical",
  );
  refuses(
    () => buildPerpsActivity({ ...oracle, signature, price: 1n << 128n }),
    "non_canonical",
  );
  refuses(
    () => buildPerpsActivity({ ...oracle, signature, sourceIdentifier: 0n }),
    "non_canonical",
  );
});

test("perps ABI builders preserve bounds and refuse incompatible legacy fields", () => {
  const order = perpsInput("perps_order_place", "order_place");
  refuses(
    () =>
      buildPerpsActivity({ ...order, side: 3 } as unknown as PerpsCallActivity),
    "non_canonical",
  );
  refuses(
    () =>
      buildPerpsActivity({
        ...order,
        timeInForce: 4,
      } as unknown as PerpsCallActivity),
    "non_canonical",
  );
  refuses(
    () =>
      buildPerpsActivity({
        ...order,
        timeInForce: 1.5,
      } as unknown as PerpsCallActivity),
    "non_canonical",
  );
  refuses(() => buildPerpsActivity({ ...order, price: -1n }), "non_canonical");
  refuses(() => buildPerpsActivity({ ...order, price: 0n }), "non_canonical");
  refuses(
    () => buildPerpsActivity({ ...order, quantity: 0n }),
    "non_canonical",
  );
  refuses(
    () => buildPerpsActivity({ ...order, quantity: 1n << 128n }),
    "non_canonical",
  );
  refuses(
    () => buildPerpsActivity({ ...order, ownerAccountId: "0".repeat(64) }),
    "non_canonical",
  );
  const open = perpsInput("perps_position_open", "position_open");
  refuses(() => buildPerpsActivity(open), "non_canonical");
  refuses(
    () =>
      buildPerpsActivity({
        ...open,
        positionId: open.marginAccountId,
        entryNotional: 1n,
      }),
    "non_canonical",
  );
  const increase = perpsInput("perps_position_increase", "position_increase");
  refuses(
    () => buildPerpsActivity({ ...increase, notionalDelta: 1n }),
    "non_canonical",
  );
  refuses(
    () => buildPerpsActivity({ ...increase, sizeDelta: 1n << 128n }),
    "non_canonical",
  );
  const halt = perpsInput("perps_market_halt", "market_halt");
  refuses(
    () =>
      buildPerpsActivity({
        ...halt,
        activity: "unsupported",
      } as unknown as PerpsCallActivity),
    "unknown_activity",
  );
});

test("ADL calldata uses a sorted array bounded to sixteen positions", () => {
  const adl = perpsInput("perps_adl", "adl");
  exactCall(
    buildPerpsActivity(adl),
    calldata(
      "0xc293c109",
      adl.marketId,
      64n,
      BigInt(adl.positionIds.length),
      ...adl.positionIds,
    ),
  );
  const ids = Array.from({ length: 17 }, (_, index) => word(BigInt(index + 1)));
  for (const count of [1, 16]) {
    exactCall(
      buildPerpsActivity({ ...adl, positionIds: ids.slice(0, count) }),
      calldata("0xc293c109", adl.marketId, 64n, count, ...ids.slice(0, count)),
    );
  }
  refuses(() => buildPerpsActivity({ ...adl, positionIds: ids }), "length");
  refuses(
    () => buildPerpsActivity({ ...adl, positionIds: [] }),
    "non_canonical",
  );
  refuses(
    () =>
      buildPerpsActivity({
        ...adl,
        positionIds: [...adl.positionIds].reverse(),
      }),
    "unsorted_sequence",
  );
  refuses(
    () =>
      buildPerpsActivity({ ...adl, positionIds: [adl.marketId, adl.marketId] }),
    "unsorted_sequence",
  );
  assert.equal(encodePerpsActivity({ ...adl, positionIds: ids }).length, 577);
});

test("spot ABI builders preserve exact refusal codes", () => {
  const order = spotInput("spot_order_place_limit", "order_place");
  refuses(() => buildSpotActivity({ ...order, price: 0n }), "non_canonical");
  refuses(() => buildSpotActivity({ ...order, quantity: 0n }), "non_canonical");
  refuses(
    () => buildSpotActivity({ ...order, quantity: -1n }),
    "non_canonical",
  );
  refuses(
    () => buildSpotActivity({ ...order, quantity: 1n << 128n }),
    "non_canonical",
  );
  refuses(
    () => buildSpotActivity({ ...order, quoteAccountId: order.baseAccountId }),
    "non_canonical",
  );
  refuses(
    () => buildSpotActivity({ ...order, marketId: "0".repeat(64) }),
    "non_canonical",
  );
  refuses(
    () => buildSpotActivity({ ...order, baseAccountId: "ff" }),
    "non_canonical",
  );
  for (const field of ["side", "kind", "timeInForce"]) {
    refuses(
      () =>
        buildSpotActivity({
          ...order,
          [field]: 3,
        } as unknown as SpotCallActivity),
      "non_canonical",
    );
  }
  const marketOrder = spotInput("spot_order_place_market", "order_place");
  refuses(
    () => buildSpotActivity({ ...marketOrder, timeInForce: 1 }),
    "non_canonical",
  );
  refuses(
    () => buildSpotActivity({ ...marketOrder, price: 1n }),
    "non_canonical",
  );
  const market = spotInput("spot_market_create", "market_create");
  refuses(
    () => buildSpotActivity({ ...market, baseAsset: market.quoteAsset }),
    "non_canonical",
  );
  refuses(
    () => buildSpotActivity({ ...market, administrator: "0".repeat(64) }),
    "non_canonical",
  );
  refuses(
    () => buildSpotActivity({ ...market, tickSize: 0n }),
    "non_canonical",
  );
  refuses(
    () => buildSpotActivity({ ...market, lotSize: 1n << 128n }),
    "non_canonical",
  );
  refuses(
    () =>
      buildSpotActivity({
        ...market,
        activity: "unsupported",
      } as unknown as SpotCallActivity),
    "unknown_activity",
  );
});

test("targets_from_address_table", () => {
  const table = readFileSync(
    new URL(
      "../../../../../spec/layerx-merge/contracts/address-table.md",
      import.meta.url,
    ),
    "utf8",
  );
  const row = table
    .split("\n")
    .find((line) => line.includes("| layerxExchange |"));
  assert.ok(row);
  const address = row.split("|")[1]?.trim();
  assert.equal(address, LAYERX_EXCHANGE_PRECOMPILE);
  for (const item of perps) {
    const input = perpsInput(
      item.name,
      activity(item).activity as PerpsActivity["activity"],
    );
    let call: PrecompileCall;
    if (input.activity === "oracle_push") {
      const observation = encodePerpsActivity(input);
      const domain = new TextEncoder().encode("LXP:ORACLE:OBSERVATION:v1");
      const message = new Uint8Array(domain.length + observation.length);
      message.set(domain);
      message.set(observation, domain.length);
      call = buildPerpsActivity({
        ...input,
        signature: ed25519.sign(message, bytes("01".repeat(32))),
      });
    } else {
      call =
        input.activity === "position_open"
          ? buildPerpsActivity({ ...input, positionId: input.marginAccountId })
          : buildPerpsActivity(input);
    }
    assert.equal(call.to, address);
  }
  for (const item of spot)
    assert.equal(buildSpotActivity(activity(item) as SpotActivity).to, address);
});

function transactionFields(
  raw: string,
): readonly { readonly payload: Uint8Array; readonly encoded: Uint8Array }[] {
  const source = bytes(raw.slice(2));
  assert.equal(source[0], 2);
  const list = source[1];
  assert.ok(list !== undefined && list > 0xf7);
  const width = list - 0xf7;
  const length = Number(BigInt(`0x${hex(source.subarray(2, 2 + width))}`));
  let offset = 2 + width;
  assert.equal(offset + length, source.length);
  const fields = [];
  while (offset < source.length) {
    const start = offset;
    const prefix = source[offset];
    assert.ok(prefix !== undefined);
    let size: number;
    if (prefix < 0x80) {
      size = 1;
    } else if (prefix <= 0xb7) {
      size = prefix - 0x80;
      offset += 1;
    } else if (prefix <= 0xbf) {
      const width = prefix - 0xb7;
      size = Number(
        BigInt(`0x${hex(source.subarray(offset + 1, offset + 1 + width))}`),
      );
      offset += 1 + width;
    } else {
      assert.equal(prefix, 0xc0);
      size = 0;
      offset += 1;
    }
    fields.push({
      payload: source.subarray(offset, offset + size),
      encoded: source.subarray(start, offset + size),
    });
    offset += size;
  }
  assert.equal(offset, source.length);
  assert.equal(fields.length, 12);
  return fields;
}

const fees: BindFees = {
  evmNonce: 7n,
  maxPriorityFeePerGas: 2n,
  maxFeePerGas: 9n,
  gasLimit: 80000n,
};

test("TradingWriter signs real type-2 transactions with the existing account key", () => {
  const derived = deriveFromSeed(bytes("000102030405060708090a0b0c0d0e0f"));
  const open = perpsInput("perps_position_open", "position_open");
  const call = buildPerpsActivity({
    ...open,
    positionId: open.marginAccountId,
  });
  const request = TradingWriter.transactionRequest(derived, 125n, call, fees);
  assert.equal(request.method, "eth_sendRawTransaction");
  assert.equal(request.params.length, 1);
  const raw = request.params[0];
  assert.equal(typeof raw, "string");
  assert.ok(typeof raw === "string");
  const fields = transactionFields(raw);
  const payload = (index: number): string => {
    const field = fields[index];
    assert.ok(field);
    return hex(field.payload);
  };
  assert.deepEqual(
    fields.slice(0, 9).map((field) => hex(field.payload)),
    [
      "7d",
      "07",
      "02",
      "09",
      "013880",
      LAYERX_EXCHANGE_PRECOMPILE.slice(2),
      "",
      call.data.slice(2),
      "",
    ],
  );
  assert.equal(hex(fields[8]?.encoded ?? new Uint8Array()), "c0");
  const unsignedBody = fields
    .slice(0, 9)
    .map((field) => hex(field.encoded))
    .join("");
  assert.equal(unsignedBody.length / 2, 197);
  const digest = keccak_256(bytes(`02f8c5${unsignedBody}`));
  const parity = payload(9) === "" ? 0 : Number.parseInt(payload(9), 16);
  assert.ok(parity === 0 || parity === 1);
  const compact = bytes(
    payload(10).padStart(64, "0") + payload(11).padStart(64, "0"),
  );
  assert.ok(derived.evmPrivateKey);
  const publicKey = secp256k1.getPublicKey(derived.evmPrivateKey, false);
  assert.equal(
    secp256k1.verify(compact, digest, publicKey, {
      prehash: false,
      lowS: true,
    }),
    true,
  );
  assert.equal(
    hex(
      secp256k1.Signature.fromBytes(compact, "compact")
        .addRecoveryBit(parity)
        .recoverPublicKey(digest)
        .toBytes(false),
    ),
    hex(publicKey),
  );
  assert.deepEqual(
    TradingWriter.transactionRequest(derived, 125n, call, fees),
    request,
  );
});

test("TradingWriter refuses invalid transaction bounds and unavailable signer keys", () => {
  const derived = deriveFromSeed(bytes("000102030405060708090a0b0c0d0e0f"));
  const call = buildPerpsActivity(
    perpsInput("perps_funding_tick", "funding_tick"),
  );
  for (const changes of [
    { evmNonce: -1n },
    { evmNonce: 1n << 64n },
    { gasLimit: 0n },
    { gasLimit: 1n << 64n },
    { maxPriorityFeePerGas: 10n },
    { maxPriorityFeePerGas: -1n },
    { maxFeePerGas: 1n << 256n },
  ]) {
    refuses(
      () =>
        TradingWriter.transactionRequest(derived, 125n, call, {
          ...fees,
          ...changes,
        }),
      "non_canonical",
    );
  }
  refuses(
    () =>
      TradingWriter.transactionRequest(
        derived,
        125n,
        { ...call, value: -1n },
        fees,
      ),
    "non_canonical",
  );
  refuses(
    () =>
      TradingWriter.transactionRequest(
        derived,
        125n,
        { ...call, data: "0x0" },
        fees,
      ),
    "non_canonical",
  );
  refuses(
    () =>
      TradingWriter.transactionRequest(
        derived,
        125n,
        { ...call, to: derived.evmAddress },
        fees,
      ),
    "non_canonical",
  );
  assert.throws(
    () =>
      TradingWriter.transactionRequest(
        { ...derived, evmPrivateKey: null },
        125n,
        call,
        fees,
      ),
    (error: unknown) =>
      error instanceof AccountDerivationError &&
      error.code === "evm_key_unavailable",
  );
  assert.throws(
    () =>
      TradingWriter.transactionRequest(
        { ...derived, evmAddress: `0x${"00".repeat(20)}` },
        125n,
        call,
        fees,
      ),
    (error: unknown) =>
      error instanceof AccountDerivationError &&
      error.code === "wallet_signer_mismatch",
  );
  for (const chain of [0n, -1n, 1n << 256n, 1.5, Number.MAX_SAFE_INTEGER + 1]) {
    assert.throws(
      () => TradingWriter.transactionRequest(derived, chain, call, fees),
      (error: unknown) =>
        error instanceof AccountDerivationError &&
        error.code === "invalid_chain_id",
    );
  }
});
