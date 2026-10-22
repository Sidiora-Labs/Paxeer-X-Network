import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { test } from "node:test";

import { secp256k1 } from "@noble/curves/secp256k1.js";
import { keccak_256 } from "@noble/hashes/sha3.js";

import {
  AccountDerivationError,
  PERPS_PRECOMPILE,
  PERPS_WRITES,
  PrecompileAbiError,
  SPOT_PRECOMPILES,
  SPOT_WRITES,
  PERPS_ACTIVITY_TYPES,
  SPOT_ACTIVITY_TYPES,
  TradingPayloadError,
  TradingWriter,
  abiSelector,
  decodePerpsActivity,
  decodeSpotActivity,
  encodePerpsActivity,
  encodeSpotActivity,
  deriveFromSeed,
  perpsActivityType,
  perpsCall,
  spotActivityType,
  spotCall,
  type BindFees,
  type PrecompileCall,
  type SpotContract,
  type SpotWrite,
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
      "../../../../../../tests/fixtures/trading-payloads/vectors.json",
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
  assert.deepEqual(call, { to: PERPS_PRECOMPILE, data, value: 0n });
  assert.deepEqual(Object.keys(call).sort(), ["data", "to", "value"]);
}

const ROOT = new URL("../../../../../../", import.meta.url);

interface AbiParameter {
  readonly type: string;
  readonly components?: readonly AbiParameter[];
}

function canonical(input: AbiParameter): string {
  return input.type.startsWith("tuple")
    ? `(${(input.components ?? []).map(canonical).join(",")})${input.type.slice(5)}`
    : input.type;
}

function abiWrites(path: string): Map<string, boolean> {
  const entries = JSON.parse(readFileSync(new URL(path, ROOT), "utf8")) as readonly {
    readonly type: string;
    readonly name?: string;
    readonly stateMutability?: string;
    readonly inputs?: readonly AbiParameter[];
  }[];
  const out = new Map<string, boolean>();
  for (const entry of entries) {
    if (entry.type !== "function" || entry.stateMutability === "view" || entry.stateMutability === "pure") continue;
    out.set(`${entry.name ?? ""}(${(entry.inputs ?? []).map(canonical).join(",")})`, entry.stateMutability === "payable");
  }
  return out;
}

function goAddress(path: string, constant: string): string {
  const match = new RegExp(`${constant}\\s*=\\s*"(0x[0-9a-fA-F]{40})"`, "u").exec(readFileSync(new URL(path, ROOT), "utf8"));
  assert.ok(match?.[1], constant);
  return match[1];
}

const SPOT_SOURCES = {
  pecor: ["PECORAddress", "pecor.json"],
  orders: ["PECOROrdersAddress", "orders.json"],
  stopOrders: ["PECORStopOrdersAddress", "stoporders.json"],
  quoter: ["PECORQuoterAddress", "quoter.json"],
  vault: ["PECORVaultAddress", "vault.json"],
} as const;

test("perps and spot write tables are exactly the precompile ABIs at the served addresses", () => {
  assert.equal(PERPS_PRECOMPILE, goAddress("modules/layerxexchange/types/ids.go", "ExchangeAddress"));
  const perpsAbi = abiWrites("precompiles/perps/abi/abi.json");
  assert.deepEqual(
    new Map(Object.entries(PERPS_WRITES).map(([signature, write]) => [signature, write.payable])),
    perpsAbi,
  );
  for (const signature of perpsAbi.keys()) {
    const write = PERPS_WRITES[signature as keyof typeof PERPS_WRITES];
    assert.equal(`${signature.slice(0, signature.indexOf("("))}(${write.types.join(",")})`, signature);
  }
  assert.deepEqual(Object.keys(SPOT_PRECOMPILES).sort(), Object.keys(SPOT_SOURCES).sort());
  for (const [contract, [constant, file]] of Object.entries(SPOT_SOURCES)) {
    const key = contract as SpotContract;
    assert.equal(SPOT_PRECOMPILES[key], goAddress("modules/spot/types/keys.go", constant));
    assert.deepEqual(
      new Map(Object.entries(SPOT_WRITES[key]).map(([signature, write]) => [signature, write.payable])),
      abiWrites(`precompiles/spot/abi/${file}`),
      contract,
    );
  }
});

test("perpsCall encodes perps writes to the perps precompile", () => {
  exactCall(
    perpsCall("openPosition(uint256,address,uint256,uint256,bool)", [3n, `0x${"ab".repeat(20)}`, 5000n, 10n, true]),
    calldata(abiSelector("openPosition(uint256,address,uint256,uint256,bool)"), 3n, `${"0".repeat(24)}${"ab".repeat(20)}`, 5000n, 10n, true),
  );
  exactCall(perpsCall("cancelOrder(uint256)", [9n]), calldata(abiSelector("cancelOrder(uint256)"), 9n));
  exactCall(
    perpsCall("cancelByCloid(uint256,bytes16)", [3n, `0x${"cd".repeat(16)}`]),
    calldata(abiSelector("cancelByCloid(uint256,bytes16)"), 3n, "cd".repeat(16).padEnd(64, "0")),
  );
  const order = "placeOrder((uint256,bool,uint8,uint8,uint256,uint256,uint256,uint256,uint256,uint256,bool,uint256,uint256,uint64,bytes16))";
  const fields = [3n, true, 1n, 0n, 100n, 200n, 5000n, 10n, 0n, 0n, false, 0n, 0n, 1700000000n, `0x${"ee".repeat(16)}`] as const;
  exactCall(
    perpsCall(order, [fields]),
    calldata(abiSelector(order), ...fields.slice(0, 14), "ee".repeat(16).padEnd(64, "0")),
  );
  assert.throws(() => perpsCall("cancelOrder(uint256)", []), PrecompileAbiError);
  assert.throws(() => perpsCall("cancelOrder(uint256)", [-1n]), PrecompileAbiError);
  assert.throws(() => perpsCall("closePositionWithParams(uint256,uint256,uint256,uint64)", [1n, 1n, 1n, 1n << 64n]), PrecompileAbiError);
  assert.throws(() => perpsCall("cancelByCloid(uint256,bytes16)", [1n, `0x${"cd".repeat(15)}`]), PrecompileAbiError);
  assert.throws(() => perpsCall(order, [fields.slice(1)]), PrecompileAbiError);
  assert.throws(() => perpsCall("deposit(address,uint256)", ["0x12", 1n]), PrecompileAbiError);
});

test("spotCall encodes PECOR writes to their own precompiles", () => {
  const usdl = `0x${"11".repeat(20)}`;
  const token = `0x${"22".repeat(20)}`;
  const word20 = (value: string): string => value.slice(2).padStart(64, "0");
  assert.deepEqual(
    spotCall("pecor", "marketBuy(address,address,uint256,uint256,uint256)", [usdl, token, 1000n, 900n, 1700000000n]),
    {
      to: SPOT_PRECOMPILES.pecor,
      data: calldata(abiSelector("marketBuy(address,address,uint256,uint256,uint256)"), word20(usdl), word20(token), 1000n, 900n, 1700000000n),
      value: 0n,
    },
  );
  assert.deepEqual(spotCall("pecor", "swapExactInNative(address,uint256,uint256)", [token, 1n, 2n], 7n), {
    to: SPOT_PRECOMPILES.pecor,
    data: calldata(abiSelector("swapExactInNative(address,uint256,uint256)"), word20(token), 1n, 2n),
    value: 7n,
  });
  assert.deepEqual(spotCall("orders", "cancelLimitOrder(uint256)", [4n]).to, SPOT_PRECOMPILES.orders);
  assert.deepEqual(spotCall("stopOrders", "cancelStopLossOrder(uint256)", [4n]).to, SPOT_PRECOMPILES.stopOrders);
  assert.deepEqual(spotCall("vault", "depositBatch(address[],uint256[])", [[usdl, token], [5n, 6n]]), {
    to: SPOT_PRECOMPILES.vault,
    data: calldata(
      abiSelector("depositBatch(address[],uint256[])"),
      64n, 160n, 2n, word20(usdl), word20(token), 2n, 5n, 6n,
    ),
    value: 0n,
  });
  assert.throws(() => spotCall("pecor", "marketBuy(address,address,uint256,uint256,uint256)", [usdl, token, 1n, 1n, 1n], 1n), PrecompileAbiError);
  assert.throws(() => spotCall("pecor", "swapExactInNative(address,uint256,uint256)", [token, 1n, 2n], -1n), PrecompileAbiError);
  assert.throws(() => spotCall("orders", "marketBuy(address,address,uint256,uint256,uint256)" as unknown as SpotWrite<"orders">, []), PrecompileAbiError);
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
  const call = perpsCall("openPosition(uint256,address,uint256,uint256,bool)", [
    3n,
    `0x${"ab".repeat(20)}`,
    5000n,
    10n,
    true,
  ]);
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
      PERPS_PRECOMPILE.slice(2),
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
  const call = perpsCall("cancelOrder(uint256)", [9n]);
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
