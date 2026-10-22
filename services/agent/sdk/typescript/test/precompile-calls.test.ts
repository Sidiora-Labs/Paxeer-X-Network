import assert from "node:assert/strict";
import { readFileSync } from "node:fs";

import {
  BRIDGE_EVENTS,
  EXCHANGE_EVENTS,
  LAUNCHPAD_EVENTS,
  LAUNCHPAD_PRECOMPILE,
  LAYERX_BRIDGE_PRECOMPILE,
  LAYERX_EXCHANGE_PRECOMPILE,
  PrecompileAbiError,
  abiEventTopic,
  abiSelector,
  bridgeInCall,
  bridgeOutCall,
  decodeBridgeEvent,
  decodeExchangeEvent,
  decodeLaunchpadEvent,
  exchangeCancelOrderCall,
  exchangeDepositMarginCall,
  exchangeDepositMarginTokenCall,
  exchangePlaceOrderCall,
  exchangeRequestSettlementCall,
  exchangeWithdrawMarginCall,
  LAUNCHPAD_WRITES,
  launchpadBuyCall,
  launchpadCall,
  launchpadClaimFeesCall,
  launchpadCreateMarketCall,
  launchpadNftCall,
  launchpadSellCall,
  launchpadSetFeeStrategyCall,
  precompileEventSignature,
  precompileTransactionRequest,
  sendExchangeDepositMargin,
  sendLaunchpadNftWrite,
  type Eip1193Requester,
  type PrecompileCall,
  type PrecompileEventSpec,
  type PrecompileLog,
} from "../src/index.js";

interface AbiEntry {
  readonly type: string;
  readonly name?: string;
  readonly stateMutability?: string;
  readonly inputs?: readonly AbiInput[];
}

interface AbiInput {
  readonly type: string;
  readonly indexed?: boolean;
  readonly name: string;
  readonly components?: readonly AbiInput[];
}

const canonical = (input: AbiInput): string =>
  input.type.startsWith("tuple")
    ? `(${(input.components ?? []).map(canonical).join(",")})${input.type.slice(5)}`
    : input.type;

function abi(directory: string): readonly AbiEntry[] {
  return JSON.parse(readFileSync(
    new URL(`../../../../../../precompiles/${directory}/abi.json`, import.meta.url),
    "utf8",
  )) as readonly AbiEntry[];
}

const sources: readonly (readonly [string, readonly PrecompileEventSpec[]])[] = [
  ["layerxexchange", EXCHANGE_EVENTS],
  ["layerxbridge", BRIDGE_EVENTS],
  ["launchpad", LAUNCHPAD_EVENTS],
];

const TOPICS: Readonly<Record<string, string>> = {
  MarginDeposited: "456ba29aa60d5cac1a6dc1c0f3df30b1f18963fd90dafe8c5f4a6de80440118a",
  MarginWithdrawalRequested: "9cf8600ce0e07d0d2b0b82ed99fc940eb6121143201d8d682c25fc74972c88f0",
  OrderCancelRequested: "39148489da3c16ee8c589a95e2f0c869816ee4f816b4ba1b85b32bc6d94c0241",
  OrderPlaced: "88b93538701d726739ade066c2a9f09e5088f608d9b2ebba0cf305f5ee0752ce",
  SettlementRequested: "70d5b9c37994017669de2a991c65a88ebeb34999f37c6dc6d0c44462d57eb655",
  BridgeIn: "4352fb2e09bdaa35c4d407ce85dfa93eaec318876eddd6f5a490cce830c3f274",
  BridgeOut: "3e990eb54009dcdca53d8fa87307210f07097f37dcf6185dee71a42f8e7d524e",
  ConfigUpdated: "2903b3642d2105b7fbd127326de888ab50e8d975bf819398337fb58407c1b10e",
  FeeDistributed: "f7ab826cedf9d005102eddf95e65f43597aef36a55ff09ee4f452204c83f4afc",
  FeeFlow: "626e3fd5d9323ca4b7ffa3ca51c21c02766b638d15eb8896a36a4e0e97ae6de4",
  FeeStrategyChanged: "337fd9f9304e9da4c04886a5b8bf56e74032b459758212a860bc61d3544353dd",
  MarketCreated: "b0a9c7605389f1127c2299d4a3ed85f0b2fc19894ab5411d76b453bb29f9f47b",
  NftMint: "d2917d37031c0dfe5016665c7a83360cbf2ff32063096b2caf61ba66c06832e5",
  NftTransfer: "021f03e8a1315704c74c03e8c022c12bc80c4a2bd4fec8bd36b217322c340ad3",
  PoolRegistered: "da5568642b67660133b1de538c867c58f3037a5722b0e231a959ee8ca11b2ed7",
  PoolStateUpdated: "8155a4b38a877d212b271811786986f8f7d78074c82fce4526c58defcc6d7ebd",
  RouterTrade: "17c1899a3fd431f2b2200b90e738d270bb856a4222040c5650bfa208acb3b650",
  Swap: "a234ab85728295548823d8a26077ecc77aa5815f9ed8f4c5e6fd390ac851d4cc",
  TokenDeployed: "ad187874b793634c55d977f35d4828526eb48170a9531c71d88f8fd10cf9c237",
};

let declared = 0;
for (const [directory, specs] of sources) {
  const events = abi(directory).filter((entry) => entry.type === "event");
  assert.equal(specs.length, events.length, directory);
  for (const entry of events) {
    const spec = specs.find((candidate) => candidate.name === entry.name);
    assert.ok(spec, `${directory} ${entry.name ?? ""}`);
    assert.deepEqual(
      spec.inputs.map((input) => [input.name, input.type, input.indexed]),
      (entry.inputs ?? []).map((input) => [input.name, input.type, input.indexed === true]),
    );
    assert.equal(abiEventTopic(precompileEventSignature(spec)), `0x${TOPICS[spec.name] ?? ""}`, spec.name);
    declared += 1;
  }
}
assert.equal(declared, 19);

const word = (value: bigint | number): string => BigInt(value).toString(16).padStart(64, "0");
const id = (byte: string): string => `0x${byte.repeat(32)}`;
const address = (byte: string): string => `0x${byte.repeat(20)}`;
const addressWord = (byte: string): string => "0".repeat(24) + byte.repeat(20);
const text = (value: string): string => {
  const body = Buffer.from(value, "utf8").toString("hex");
  return word(body.length / 2) + body.padEnd(Math.ceil(body.length / 64) * 64, "0");
};

const writes: readonly (readonly [string, string, PrecompileCall, string, string])[] = [
  ["layerxexchange", "placeOrder(bytes32,uint8,uint256,uint256,uint8)",
    exchangePlaceOrderCall({ marketId: id("11"), side: 2, price: 18446744073709551618n, quantity: 5000n, timeInForce: 0 }),
    LAYERX_EXCHANGE_PRECOMPILE, "11".repeat(32) + word(2) + word(18446744073709551618n) + word(5000) + word(0)],
  ["layerxexchange", "cancelOrder(bytes32)", exchangeCancelOrderCall(id("31")), LAYERX_EXCHANGE_PRECOMPILE, "31".repeat(32)],
  ["layerxexchange", "requestSettlement(bytes32)", exchangeRequestSettlementCall(id("41")), LAYERX_EXCHANGE_PRECOMPILE, "41".repeat(32)],
  ["layerxexchange", "depositMargin(bytes32)", exchangeDepositMarginCall(id("51"), 7n), LAYERX_EXCHANGE_PRECOMPILE, "51".repeat(32)],
  ["layerxexchange", "depositMarginToken(address,uint256,bytes32)",
    exchangeDepositMarginTokenCall(address("ab"), 9n, id("51")), LAYERX_EXCHANGE_PRECOMPILE,
    addressWord("ab") + word(9) + "51".repeat(32)],
  ["layerxexchange", "withdrawMargin(bytes32,bytes32,uint256)",
    exchangeWithdrawMarginCall(id("51"), id("61"), 10n), LAYERX_EXCHANGE_PRECOMPILE, "51".repeat(32) + "61".repeat(32) + word(10)],
  ["layerxbridge", "bridgeIn(uint64,address,bytes32,uint64,bytes32,address,uint256,bytes[])",
    bridgeInCall({ chain: 1n, vault: address("aa"), txHash: id("bb"), logIndex: 3n, recipient: id("cc"), asset: address("dd"), amount: 5n, signatures: ["0x0102", "0x" + "ee".repeat(33)] }),
    LAYERX_BRIDGE_PRECOMPILE,
    word(1) + addressWord("aa") + "bb".repeat(32) + word(3) + "cc".repeat(32) + addressWord("dd") + word(5) + word(256)
      + word(2) + word(64) + word(128) + word(2) + "0102".padEnd(64, "0") + word(33) + ("ee".repeat(33)).padEnd(128, "0")],
  ["layerxbridge", "bridgeOut(uint64,address,uint256,address)",
    bridgeOutCall(8n, address("dd"), 5n, address("ee")), LAYERX_BRIDGE_PRECOMPILE, word(8) + addressWord("dd") + word(5) + addressWord("ee")],
  ["launchpad", "buy(address,uint256,uint256,uint256)",
    launchpadBuyCall({ pool: address("aa"), amountIn: 100n, minOut: 90n, deadline: 1700000000n }), LAUNCHPAD_PRECOMPILE,
    addressWord("aa") + word(100) + word(90) + word(1700000000)],
  ["launchpad", "sell(address,uint256,uint256,uint256)",
    launchpadSellCall({ pool: address("aa"), amountIn: 100n, minOut: 90n, deadline: 1700000000n }), LAUNCHPAD_PRECOMPILE,
    addressWord("aa") + word(100) + word(90) + word(1700000000)],
  ["launchpad", "createMarket(string,string,uint8,address)", launchpadCreateMarketCall("Paxeer Dog", "PDOG", 2), LAUNCHPAD_PRECOMPILE,
    word(128) + word(192) + word(2) + word(0) + text("Paxeer Dog") + text("PDOG")],
  ["launchpad", "createMarket(string,string,uint8,address)", launchpadCreateMarketCall("Paxeer Dog", "PDOG", 2, address("cc")), LAUNCHPAD_PRECOMPILE,
    word(128) + word(192) + word(2) + addressWord("cc") + text("Paxeer Dog") + text("PDOG")],
  ["launchpad", "setFeeStrategy(uint256,uint8)", launchpadSetFeeStrategyCall(7n, 1), LAUNCHPAD_PRECOMPILE, word(7) + word(1)],
  ["launchpad", "claimFees(uint256)", launchpadClaimFeesCall(7n), LAUNCHPAD_PRECOMPILE, word(7)],
  ...(["claimAirdrop", "executeAirdrop", "executeBurn", "executeLpRewards"] as const).map(
    (write) => ["launchpad", `${write}(uint256)`, launchpadNftCall(write, 7n), LAUNCHPAD_PRECOMPILE, word(7)] as const,
  ),
  ["launchpad", "multicall(bytes[])", launchpadCall("multicall(bytes[])", [["0x0102"]]), LAUNCHPAD_PRECOMPILE,
    word(32) + word(1) + word(32) + word(2) + "0102".padEnd(64, "0")],
  ["launchpad", "buyWithPermit(address,uint256,uint256,uint256,(uint256,uint256,uint8,bytes32,bytes32))",
    launchpadCall("buyWithPermit(address,uint256,uint256,uint256,(uint256,uint256,uint8,bytes32,bytes32))",
      [address("aa"), 100n, 90n, 1700000000n, [5n, 1700000000n, 27n, id("31"), id("41")]]), LAUNCHPAD_PRECOMPILE,
    addressWord("aa") + word(100) + word(90) + word(1700000000) + word(5) + word(1700000000) + word(27) + "31".repeat(32) + "41".repeat(32)],
];

for (const [directory, signature, call, to, body] of writes) {
  assert.equal(call.to, to, signature);
  assert.equal(call.data, abiSelector(signature) + body, signature);
  assert.equal(call.value, signature.startsWith("depositMargin(") ? 7n : 0n, signature);
  const entry = abi(directory).find((candidate) =>
    candidate.type === "function" && `${candidate.name ?? ""}(${(candidate.inputs ?? []).map(canonical).join(",")})` === signature);
  assert.ok(entry, signature);
}
const launchpadWrites = new Set(abi("launchpad")
  .filter((entry) => entry.type === "function" && entry.stateMutability !== "view" && entry.stateMutability !== "pure")
  .map((entry) => `${entry.name ?? ""}(${(entry.inputs ?? []).map(canonical).join(",")})`));
assert.deepEqual(new Set(Object.keys(LAUNCHPAD_WRITES)), launchpadWrites);
for (const [directory] of sources) {
  const nonView = abi(directory).filter(
    (entry) => entry.type === "function" && entry.stateMutability !== "view" && entry.stateMutability !== "pure",
  );
  for (const entry of nonView) {
    const signature = `${entry.name ?? ""}(${(entry.inputs ?? []).map(canonical).join(",")})`;
    assert.ok(writes.some(([source, built]) => source === directory && built === signature) || (directory === "launchpad" && launchpadWrites.has(signature)), signature);
  }
}
assert.equal(abiSelector("transfer(address,uint256)"), "0xa9059cbb");

const from = address("12");
const sent: unknown[] = [];
const wallet: Eip1193Requester = {
  request: async ({ method, params }) => {
    sent.push({ method, params });
    return id("fe");
  },
};
assert.equal(await sendExchangeDepositMargin(wallet, from, id("51"), 255n), id("fe"));
assert.equal(await sendLaunchpadNftWrite(wallet, from, "executeBurn", 9n), id("fe"));
assert.deepEqual(sent, [
  { method: "eth_sendTransaction", params: [{ from, to: LAYERX_EXCHANGE_PRECOMPILE, data: abiSelector("depositMargin(bytes32)") + "51".repeat(32), value: "0xff" }] },
  { method: "eth_sendTransaction", params: [{ from, to: LAUNCHPAD_PRECOMPILE, data: abiSelector("executeBurn(uint256)") + word(9), value: "0x0" }] },
]);
assert.deepEqual(precompileTransactionRequest(from, exchangeCancelOrderCall(id("31"))).params, [
  { from, to: LAYERX_EXCHANGE_PRECOMPILE, data: abiSelector("cancelOrder(bytes32)") + "31".repeat(32), value: "0x0" },
]);
await assert.rejects(
  sendExchangeDepositMargin({ request: async () => null }, from, id("51"), 1n),
  (error: unknown) => error instanceof PrecompileAbiError && error.code === "malformed_wallet_answer",
);
assert.throws(() => exchangeDepositMarginCall(id("51"), 0n), PrecompileAbiError);
assert.throws(() => exchangePlaceOrderCall({ marketId: id("11"), side: 256, price: 1n, quantity: 1n, timeInForce: 0 }), PrecompileAbiError);

const topic = (name: string): string => `0x${TOPICS[name] ?? ""}`;
const order: PrecompileLog = {
  address: LAYERX_EXCHANGE_PRECOMPILE,
  topics: [topic("OrderPlaced"), id("a1"), id("11"), `0x${addressWord("12")}`],
  data: `0x${word(2)}${word(18446744073709551618n)}${word(5000)}${word(0)}${word(4)}`,
};
assert.deepEqual(decodeExchangeEvent(order), {
  event: "OrderPlaced",
  precompile: LAYERX_EXCHANGE_PRECOMPILE,
  topic0: topic("OrderPlaced"),
  fields: { intentId: id("a1"), marketId: id("11"), owner: from, side: 2n, price: 18446744073709551618n, quantity: 5000n, timeInForce: 0n, nonce: 4n },
});
const refused = (log: PrecompileLog, code: PrecompileAbiError["code"], decode = decodeExchangeEvent): void => {
  assert.throws(() => decode(log), (error: unknown) => error instanceof PrecompileAbiError && error.code === code);
};
refused({ ...order, topics: order.topics.slice(0, 3) }, "topic_count");
refused({ ...order, data: order.data.slice(0, -2) }, "data_length");
refused({ ...order, data: `0x${word(256)}${order.data.slice(66)}` }, "non_canonical_word");
refused({ ...order, address: LAUNCHPAD_PRECOMPILE }, "unknown_event");
refused({ ...order, topics: [id("00"), ...order.topics.slice(1)] }, "unknown_event");

const bridgeIn = decodeBridgeEvent({
  address: LAYERX_BRIDGE_PRECOMPILE,
  topics: [topic("BridgeIn"), `0x${word(1)}`, id("bb"), `0x${addressWord("12")}`],
  data: `0x${word(3)}${addressWord("dd")}${word(5)}${word(128)}${text("factory/paxeer/usdc")}`,
});
assert.equal(bridgeIn.event, "BridgeIn");
assert.deepEqual(bridgeIn.fields, { chain: 1n, txHash: id("bb"), recipient: from, logIndex: 3n, asset: address("dd"), amount: 5n, denom: "factory/paxeer/usdc" });
const bridgeOut = decodeBridgeEvent({
  address: LAYERX_BRIDGE_PRECOMPILE,
  topics: [topic("BridgeOut"), `0x${word(8)}`, `0x${addressWord("dd")}`, `0x${word(9)}`],
  data: `0x${word(5)}${addressWord("ee")}`,
});
assert.deepEqual(bridgeOut.fields, { chain: 8n, asset: address("dd"), amount: 5n, recipient: address("ee"), nonce: 9n });

const created = decodeLaunchpadEvent({
  address: LAUNCHPAD_PRECOMPILE,
  topics: [topic("MarketCreated"), id("a1"), `0x${addressWord("aa")}`, `0x${addressWord("12")}`],
  data: `0x${addressWord("bb")}${addressWord("cc")}${word(1700000000)}${word(42)}`,
});
assert.deepEqual(created.fields, { poolId: id("a1"), token: address("aa"), creator: from, pool: address("bb"), optical: address("cc"), timestamp: 1700000000n, blockNumber: 42n });
const swapLog: PrecompileLog = {
  address: LAUNCHPAD_PRECOMPILE,
  topics: [topic("Swap"), id("a1"), `0x${addressWord("12")}`],
  data: `0x${word(1)}${word(100)}${word(95)}${word(1)}${word(7)}${word(1700000000)}${word(42)}`,
};
assert.deepEqual(decodeLaunchpadEvent(swapLog).fields, {
  poolId: id("a1"), sender: from, isBuy: true, amountIn: 100n, amountOut: 95n, fee: 1n, price: 7n, timestamp: 1700000000n, blockNumber: 42n,
});
refused({ ...swapLog, data: `0x${word(2)}${swapLog.data.slice(66)}` }, "non_canonical_word", decodeLaunchpadEvent);
assert.throws(() => launchpadBuyCall({ pool: "0x12", amountIn: 1n, minOut: 1n, deadline: 1n }), PrecompileAbiError);
assert.throws(() => launchpadCall("claimFees(uint256)", [1n], 1n), PrecompileAbiError);
assert.throws(() => launchpadCall("claimFees(uint256)", []), PrecompileAbiError);
assert.throws(() => launchpadSetFeeStrategyCall(1n, 256), PrecompileAbiError);
