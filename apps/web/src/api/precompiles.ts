import "server-only";

import {
  decodeAbiResult,
  readFlag,
  readList,
  readRecord,
  readText,
  readUint,
  type AbiReadField,
  type AbiReadRecord,
} from "./abi-read.ts";
import { ethBlockNumber, ethCall, ethGetLogs, type GatewayLog } from "./gateway.ts";
import {
  BRIDGE_EVENTS,
  EXCHANGE_EVENTS,
  LAUNCHPAD_PRECOMPILE,
  LAYERX_BRIDGE_PRECOMPILE,
  LAYERX_EXCHANGE_PRECOMPILE,
  abiEventTopic,
  decodeBridgeEvent,
  decodeExchangeEvent,
  encodeAbiCall,
  precompileEventSignature,
  type DecodedPrecompileEvent,
} from "./sdk.ts";

/** How many recent Paxeer blocks the precompile event scans cover. */
export const PRECOMPILE_LOG_WINDOW = 50_000n;

const EVM_ADDRESS = /^0x[0-9a-fA-F]{40}$/u;
const ZERO32 = `0x${"00".repeat(32)}`;

async function view(to: string, data: string, outputs: readonly AbiReadField[]): Promise<AbiReadRecord> {
  return decodeAbiResult(await ethCall({ to, data }), outputs);
}

function addressTopic(address: string): string {
  if (!EVM_ADDRESS.test(address)) {
    throw new RangeError("an EVM address is required");
  }
  return `0x${"00".repeat(12)}${address.slice(2).toLowerCase()}`;
}

async function recentLogs(address: string, topics: readonly (string | null)[]): Promise<readonly GatewayLog[]> {
  const head = await ethBlockNumber();
  const fromBlock = head > PRECOMPILE_LOG_WINDOW ? head - PRECOMPILE_LOG_WINDOW : 0n;
  return ethGetLogs({ address, topics, fromBlock });
}

export interface ObservedEvent {
  readonly event: DecodedPrecompileEvent;
  readonly blockNumber: bigint;
  readonly transactionHash: string;
  readonly logIndex: bigint;
}

function observed(log: GatewayLog, decode: (log: GatewayLog) => DecodedPrecompileEvent): ObservedEvent | undefined {
  try {
    return {
      event: decode(log),
      blockNumber: log.blockNumber,
      transactionHash: log.transactionHash,
      logIndex: log.logIndex,
    };
  } catch {
    return undefined;
  }
}

function newestFirst(events: readonly (ObservedEvent | undefined)[]): readonly ObservedEvent[] {
  return events
    .filter((entry): entry is ObservedEvent => entry !== undefined)
    .sort((left, right) =>
      left.blockNumber === right.blockNumber
        ? Number(right.logIndex - left.logIndex)
        : right.blockNumber > left.blockNumber
          ? 1
          : -1,
    );
}

/* Launchpad */

const ZERO_ADDRESS = `0x${"00".repeat(20)}`;

const LAUNCHPAD_POOL_STATS: readonly AbiReadField[] = [
  { name: "virtualUsdl", type: "uint256" },
  { name: "realUsdl", type: "uint256" },
  { name: "tokenReserve", type: "uint256" },
  { name: "cumulativeVolume", type: "uint256" },
  { name: "currentFeeBps", type: "uint256" },
  { name: "poolAge", type: "uint256" },
  { name: "marketCap", type: "uint256" },
  { name: "price", type: "uint256" },
];

const LAUNCHPAD_POOL_METADATA: readonly AbiReadField[] = [
  { name: "creator", type: "address" },
  { name: "token", type: "address" },
  { name: "optical", type: "address" },
  { name: "nftId", type: "uint256" },
  { name: "createdAt", type: "uint256" },
  { name: "createdBlock", type: "uint256" },
];

export interface LaunchpadMarket {
  readonly pool: string;
  readonly token: string;
  readonly name: string;
  readonly symbol: string;
  readonly creator: string;
  readonly feeStrategy: bigint;
  readonly paused: boolean;
  readonly totalSupply: bigint;
  readonly tokenReserve: bigint;
  readonly realQuoteBalance: bigint;
  readonly cumulativeVolume: bigint;
  readonly price: bigint;
}

export interface LaunchpadConfig {
  readonly quoteDenom: string;
  readonly creationFee: bigint;
  readonly minFeeBps: bigint;
  readonly maxFeeBps: bigint;
  readonly baseFeeBps: bigint;
}

async function viewUint(to: string, name: string, types: readonly ("address" | "uint256")[] = [], values: readonly (string | bigint)[] = []): Promise<bigint> {
  const result = await view(to, encodeAbiCall(name, types, values), [{ name: "value", type: "uint256" }]);
  return readUint(result.value, name);
}

async function viewAddress(to: string, name: string, types: readonly "address"[] = [], values: readonly string[] = []): Promise<string> {
  const result = await view(to, encodeAbiCall(name, types, values), [{ name: "value", type: "address" }]);
  return readText(result.value, name);
}

async function viewString(to: string, name: string): Promise<string> {
  const result = await view(to, encodeAbiCall(name, [], []), [{ name: "value", type: "string" }]);
  return readText(result.value, name);
}

async function viewFlag(to: string, name: string): Promise<boolean> {
  const result = await view(to, encodeAbiCall(name, [], []), [{ name: "value", type: "bool" }]);
  return readFlag(result.value, name);
}

/** PoolRegistry `getPoolCount()` on the launchpad precompile. */
export async function launchpadMarketCount(): Promise<bigint> {
  return viewUint(LAUNCHPAD_PRECOMPILE, "getPoolCount");
}

async function launchpadMarket(pool: string, feeRights: string): Promise<LaunchpadMarket> {
  const [statsResult, metadataResult, paused] = await Promise.all([
    view(LAUNCHPAD_PRECOMPILE, encodeAbiCall("getPoolStats", ["address"], [pool]), [
      { name: "stats", type: { tuple: LAUNCHPAD_POOL_STATS } },
    ]),
    view(LAUNCHPAD_PRECOMPILE, encodeAbiCall("getPoolMetadata", ["address"], [pool]), LAUNCHPAD_POOL_METADATA),
    viewFlag(pool, "paused"),
  ]);
  const stats = readRecord(statsResult.stats, "stats");
  const token = readText(metadataResult.token, "token");
  const [name, symbol, totalSupply, feeStrategy] = await Promise.all([
    viewString(token, "name"),
    viewString(token, "symbol"),
    viewUint(token, "totalSupply"),
    view(feeRights, encodeAbiCall("getFeeStrategy", ["uint256"], [readUint(metadataResult.nftId, "nftId")]), [
      { name: "strategy", type: "uint8" },
    ]),
  ]);
  return {
    pool,
    token,
    name,
    symbol,
    creator: readText(metadataResult.creator, "creator"),
    feeStrategy: readUint(feeStrategy.strategy, "strategy"),
    paused,
    totalSupply,
    tokenReserve: readUint(stats.tokenReserve, "tokenReserve"),
    realQuoteBalance: readUint(stats.realUsdl, "realUsdl"),
    cumulativeVolume: readUint(stats.cumulativeVolume, "cumulativeVolume"),
    price: readUint(stats.price, "price"),
  };
}

/** PoolRegistry `getAllPools(offset, limit)`, each pool read through the Quoter, its pool surface, its token and its fee right. */
export async function launchpadMarkets(offset: bigint, limit: bigint): Promise<readonly LaunchpadMarket[]> {
  const [result, feeRights] = await Promise.all([
    view(LAUNCHPAD_PRECOMPILE, encodeAbiCall("getAllPools", ["uint256", "uint256"], [offset, limit]), [
      { name: "pools", type: { array: "address" } },
    ]),
    viewAddress(LAUNCHPAD_PRECOMPILE, "nftContract"),
  ]);
  const pools = readList(result.pools, "pools").map((pool) => readText(pool, "pool"));
  return Promise.all(pools.map((pool) => launchpadMarket(pool, feeRights)));
}

/** The ProtocolConfig getters, with the quote asset named by the USDL token's symbol. */
export async function launchpadConfig(): Promise<LaunchpadConfig> {
  const [usdl, creationFee, minFeeBps, maxFeeBps, baseFeeBps] = await Promise.all([
    viewAddress(LAUNCHPAD_PRECOMPILE, "usdlAddress"),
    viewUint(LAUNCHPAD_PRECOMPILE, "creationFee"),
    viewUint(LAUNCHPAD_PRECOMPILE, "minFeeBps"),
    viewUint(LAUNCHPAD_PRECOMPILE, "maxFeeBps"),
    viewUint(LAUNCHPAD_PRECOMPILE, "baseFeeBps"),
  ]);
  return { quoteDenom: await viewString(usdl, "symbol"), creationFee, minFeeBps, maxFeeBps, baseFeeBps };
}

export interface LaunchpadQuote {
  readonly amountOut: bigint;
  readonly feeAmount: bigint;
  readonly priceImpactBps: bigint;
}

/** The Quoter's `quoteExactInput` answer for `amountIn` against the pool of `token`. */
export async function launchpadQuote(token: string, side: "buy" | "sell", amountIn: bigint): Promise<LaunchpadQuote> {
  const pool = await viewAddress(LAUNCHPAD_PRECOMPILE, "getPoolByToken", ["address"], [token]);
  if (pool === ZERO_ADDRESS) {
    throw new RangeError(`no launchpad pool trades ${token}`);
  }
  const result = await view(
    LAUNCHPAD_PRECOMPILE,
    encodeAbiCall("quoteExactInput", ["address", "uint256", "bool"], [pool, amountIn, side === "buy"]),
    [
      {
        name: "result",
        type: {
          tuple: [
            { name: "amountOut", type: "uint256" },
            { name: "feeAmount", type: "uint256" },
            { name: "priceImpactBps", type: "uint256" },
          ],
        },
      },
    ],
  );
  const quote = readRecord(result.result, "result");
  return {
    amountOut: readUint(quote.amountOut, "amountOut"),
    feeAmount: readUint(quote.feeAmount, "feeAmount"),
    priceImpactBps: readUint(quote.priceImpactBps, "priceImpactBps"),
  };
}

/* Bridge */

export interface BridgeChain {
  readonly registered: boolean;
  readonly vault: string;
  readonly finalityDepth: bigint;
  readonly enabled: boolean;
}

export interface BridgeCap {
  readonly denom: string;
  readonly maxInFlight: bigint;
  readonly maxPerTx: bigint;
  readonly inFlight: bigint;
}

export interface BridgeAttestors {
  readonly signers: readonly string[];
  readonly threshold: bigint;
}

export async function bridgePaused(): Promise<boolean> {
  const result = await view(LAYERX_BRIDGE_PRECOMPILE, encodeAbiCall("isPaused", [], []), [
    { name: "paused", type: "bool" },
  ]);
  return readFlag(result.paused, "paused");
}

export async function bridgeChain(chain: bigint): Promise<BridgeChain> {
  const result = await view(LAYERX_BRIDGE_PRECOMPILE, encodeAbiCall("getChain", ["uint64"], [chain]), [
    { name: "registered", type: "bool" },
    { name: "vault", type: "address" },
    { name: "finalityDepth", type: "uint64" },
    { name: "enabled", type: "bool" },
  ]);
  return {
    registered: readFlag(result.registered, "registered"),
    vault: readText(result.vault, "vault"),
    finalityDepth: readUint(result.finalityDepth, "finalityDepth"),
    enabled: readFlag(result.enabled, "enabled"),
  };
}

export async function bridgeCap(chain: bigint, asset: string): Promise<BridgeCap> {
  const result = await view(
    LAYERX_BRIDGE_PRECOMPILE,
    encodeAbiCall("getCap", ["uint64", "address"], [chain, asset]),
    [
      { name: "denom", type: "string" },
      { name: "maxInFlight", type: "uint256" },
      { name: "maxPerTx", type: "uint256" },
      { name: "inFlight", type: "uint256" },
    ],
  );
  return {
    denom: readText(result.denom, "denom"),
    maxInFlight: readUint(result.maxInFlight, "maxInFlight"),
    maxPerTx: readUint(result.maxPerTx, "maxPerTx"),
    inFlight: readUint(result.inFlight, "inFlight"),
  };
}

export async function bridgeAttestors(): Promise<BridgeAttestors> {
  const result = await view(LAYERX_BRIDGE_PRECOMPILE, encodeAbiCall("getAttestors", [], []), [
    { name: "signers", type: { array: "address" } },
    { name: "bonds", type: { array: "uint256" } },
    { name: "threshold", type: "uint32" },
  ]);
  return {
    signers: readList(result.signers, "signers").map((signer) => readText(signer, "signer")),
    threshold: readUint(result.threshold, "threshold"),
  };
}

/** True once the remote deposit event has been attested and credited on Paxeer. */
export async function bridgeNullified(chain: bigint, txHash: string, logIndex: bigint): Promise<boolean> {
  const result = await view(
    LAYERX_BRIDGE_PRECOMPILE,
    encodeAbiCall("isNullified", ["uint64", "bytes32", "uint64"], [chain, txHash, logIndex]),
    [{ name: "nullified", type: "bool" }],
  );
  return readFlag(result.nullified, "nullified");
}

function bridgeTopic(name: string): string {
  const spec = BRIDGE_EVENTS.find((candidate) => candidate.name === name);
  if (spec === undefined) {
    throw new RangeError(`unknown bridge event ${name}`);
  }
  return abiEventTopic(precompileEventSignature(spec));
}

/** Inbound attestations credited to `recipient` and outbound requests paying `recipient`, newest first. */
export async function bridgeActivity(recipient: string): Promise<readonly ObservedEvent[]> {
  const [inbound, outbound] = await Promise.all([
    recentLogs(LAYERX_BRIDGE_PRECOMPILE, [bridgeTopic("BridgeIn"), null, null, addressTopic(recipient)]),
    recentLogs(LAYERX_BRIDGE_PRECOMPILE, [bridgeTopic("BridgeOut")]),
  ]);
  const lowered = recipient.toLowerCase();
  return newestFirst([
    ...inbound.map((log) => observed(log, decodeBridgeEvent)),
    ...outbound
      .map((log) => observed(log, decodeBridgeEvent))
      .filter((entry) => entry !== undefined && entry.event.fields.recipient === lowered),
  ]);
}

/* Exchange */

const INTENT_FIELDS: readonly AbiReadField[] = [
  { name: "intentId", type: "bytes32" },
  { name: "kind", type: "uint8" },
  { name: "status", type: "uint8" },
  { name: "owner", type: "address" },
  { name: "nonce", type: "uint64" },
  { name: "height", type: "uint64" },
  { name: "account", type: "bytes32" },
  { name: "assetId", type: "bytes32" },
  { name: "denom", type: "string" },
  { name: "amount", type: "uint256" },
  { name: "depositId", type: "bytes32" },
  { name: "marketId", type: "bytes32" },
  { name: "side", type: "uint8" },
  { name: "price", type: "uint256" },
  { name: "quantity", type: "uint256" },
  { name: "timeInForce", type: "uint8" },
  { name: "orderId", type: "bytes32" },
  { name: "positionId", type: "bytes32" },
];

export type ExchangeIntentKind = "deposit" | "withdraw" | "place" | "cancel" | "settle" | "unknown";

export interface ExchangeIntent {
  readonly intentId: string;
  readonly kind: ExchangeIntentKind;
  readonly pending: boolean;
  readonly marketId: string;
  readonly side: bigint;
  readonly price: bigint;
  readonly quantity: bigint;
  readonly timeInForce: bigint;
  readonly orderId: string | null;
  readonly positionId: string | null;
  readonly amount: bigint;
  readonly assetId: string;
}

const INTENT_KINDS: readonly ExchangeIntentKind[] = ["unknown", "deposit", "withdraw", "place", "cancel", "settle"];

export async function exchangeIntent(intentId: string): Promise<ExchangeIntent> {
  const result = await view(LAYERX_EXCHANGE_PRECOMPILE, encodeAbiCall("getIntent", ["bytes32"], [intentId]), [
    { name: "intent", type: { tuple: INTENT_FIELDS } },
  ]);
  const intent = readRecord(result.intent, "intent");
  const orderId = readText(intent.orderId, "orderId");
  const positionId = readText(intent.positionId, "positionId");
  return {
    intentId,
    kind: INTENT_KINDS[Number(readUint(intent.kind, "kind"))] ?? "unknown",
    pending: readUint(intent.status, "status") === 1n,
    marketId: readText(intent.marketId, "marketId"),
    side: readUint(intent.side, "side"),
    price: readUint(intent.price, "price"),
    quantity: readUint(intent.quantity, "quantity"),
    timeInForce: readUint(intent.timeInForce, "timeInForce"),
    orderId: orderId === ZERO32 ? null : orderId,
    positionId: positionId === ZERO32 ? null : positionId,
    amount: readUint(intent.amount, "amount"),
    assetId: readText(intent.assetId, "assetId"),
  };
}

export interface ExchangeOrder {
  readonly intentId: string;
  readonly marketId: string;
  readonly side: bigint;
  readonly price: bigint;
  readonly quantity: bigint;
  readonly timeInForce: bigint;
  readonly orderId: string | null;
  readonly pending: boolean;
  readonly transactionHash: string;
}

export interface ExchangePosition {
  readonly positionId: string;
  readonly settlementPending: boolean;
  readonly transactionHash: string;
}

export interface ExchangeState {
  readonly events: readonly ObservedEvent[];
  readonly openOrders: readonly ExchangeOrder[];
  readonly positions: readonly ExchangePosition[];
}

function text(event: DecodedPrecompileEvent, field: string): string {
  const value = event.fields[field];
  return typeof value === "string" ? value : "";
}

function amount(event: DecodedPrecompileEvent, field: string): bigint {
  const value = event.fields[field];
  return typeof value === "bigint" ? value : 0n;
}

/** Every LayerXExchange event `owner` emitted in the scan window, with the orders and positions it names. */
export async function exchangeState(owner: string): Promise<ExchangeState> {
  const logs = await recentLogs(LAYERX_EXCHANGE_PRECOMPILE, [null, null, null, addressTopic(owner)]);
  const events = newestFirst(logs.map((log) => observed(log, decodeExchangeEvent))).filter((entry) =>
    EXCHANGE_EVENTS.some((spec) => spec.name === entry.event.event),
  );
  const cancelled = new Set(
    events.filter((entry) => entry.event.event === "OrderCancelRequested").map((entry) => text(entry.event, "orderId")),
  );
  const placed = events.filter((entry) => entry.event.event === "OrderPlaced");
  const settlements = events.filter((entry) => entry.event.event === "SettlementRequested");
  const intents = await Promise.all(placed.map((entry) => exchangeIntent(text(entry.event, "intentId"))));
  const openOrders = placed
    .map((entry, index): ExchangeOrder => {
      const intent = intents[index];
      return {
        intentId: text(entry.event, "intentId"),
        marketId: text(entry.event, "marketId"),
        side: amount(entry.event, "side"),
        price: amount(entry.event, "price"),
        quantity: amount(entry.event, "quantity"),
        timeInForce: amount(entry.event, "timeInForce"),
        orderId: intent?.orderId ?? null,
        pending: intent?.pending ?? false,
        transactionHash: entry.transactionHash,
      };
    })
    .filter((order) => order.orderId === null || !cancelled.has(order.orderId));
  const settlementIntents = await Promise.all(
    settlements.map((entry) => exchangeIntent(text(entry.event, "intentId"))),
  );
  const positions = settlements.map((entry, index): ExchangePosition => ({
    positionId: text(entry.event, "positionId"),
    settlementPending: settlementIntents[index]?.pending ?? false,
    transactionHash: entry.transactionHash,
  }));
  return { events, openOrders, positions };
}
