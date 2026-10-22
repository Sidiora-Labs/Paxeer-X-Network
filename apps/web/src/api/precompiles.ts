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
import { GatewayRpcError, ethBlockNumber, ethCall, ethGetLogs, type GatewayLog } from "./gateway.ts";
import {
  BRIDGE_EVENTS,
  LAUNCHPAD_PRECOMPILE,
  LAYERX_BRIDGE_PRECOMPILE,
  PERPS_PRECOMPILE,
  PrecompileAbiError,
  abiEventTopic,
  abiSelector,
  decodeBridgeEvent,
  encodeAbiCall,
  precompileEventSignature,
  type DecodedPrecompileEvent,
} from "./sdk.ts";

/** How many recent Paxeer blocks the precompile event scans cover. */
export const PRECOMPILE_LOG_WINDOW = 50_000n;

const EVM_ADDRESS = /^0x[0-9a-fA-F]{40}$/u;

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

/* Perps (0x1015) */

/** The USDL collateral the perps module lists by default (modules/perps/types/params.go DefaultCollateralToken). */
export const PERPS_COLLATERAL_TOKEN = "0x85fcd13735f4309833a503ee804ea32395851479";
/** USDL carries 6 decimals: the perps keeper prices one USDL unit at 1e12 of its 18-decimal USD. */
export const PERPS_COLLATERAL_DECIMALS = 6;
/** USD sizes and prices of the perps precompile are 18-decimal fixed point. */
export const PERPS_USD_DECIMALS = 18;
/** The bound on totalMarkets() a market listing accepts. */
export const PERPS_MAX_MARKETS = 1024n;

/** The parameterless custom errors of precompiles/perps/abi/abi.json, the revert data of every keeper refusal. */
export const PERPS_ERRORS = [
  "PerpsCloseSizeExceedsPosition", "PerpsCrossMarginOnly", "PerpsDeadlineExpired", "PerpsDelegatecall",
  "PerpsDuplicateCloid", "PerpsFokNotFilled", "PerpsInsufficientCollateral", "PerpsInsufficientMargin",
  "PerpsInvalidGenesis", "PerpsInvalidOrderType", "PerpsInvalidParams", "PerpsInvalidTimeInForce",
  "PerpsInvalidTriggerPrice", "PerpsLeverageTooHigh", "PerpsLiquidationCooldown", "PerpsLotSize",
  "PerpsMarketDisabled", "PerpsMaxOrderValue", "PerpsMinOrderValue", "PerpsNoAccount", "PerpsNoLiquidity",
  "PerpsNotLiquidatable", "PerpsNotOrderHolder", "PerpsNotPositionHolder", "PerpsOpenInterestCap", "PerpsOverflow",
  "PerpsPaused", "PerpsPostOnlyWouldCross", "PerpsPriceStale", "PerpsReduceOnlyViolation", "PerpsSelfTransfer",
  "PerpsSlippageExceeded", "PerpsStaticCall", "PerpsTickSize", "PerpsTokenTransferFailed", "PerpsTooManyOpenOrders",
  "PerpsTriggerNotReached", "PerpsUnauthorized", "PerpsUnknownMarket", "PerpsUnknownOrder", "PerpsUnknownPosition",
  "PerpsUnsupportedCollateral", "PerpsZeroAddress", "PerpsZeroAmount",
] as const;

const PERPS_ERROR_BY_SELECTOR: ReadonlyMap<string, string> = new Map(
  PERPS_ERRORS.map((name) => [abiSelector(`${name}()`), name]),
);
const ERROR_STRING = "0x08c379a0";

/** The reason a perps precompile revert carries: its custom error name or its Error(string) text. */
export function perpsRevertReason(data: string | undefined): string | undefined {
  if (data === undefined) {
    return undefined;
  }
  const lowered = data.toLowerCase();
  const named = PERPS_ERROR_BY_SELECTOR.get(lowered);
  if (named !== undefined) {
    return named;
  }
  if (lowered.startsWith(ERROR_STRING)) {
    try {
      return readText(decodeAbiResult(`0x${lowered.slice(ERROR_STRING.length)}`, [{ name: "reason", type: "string" }]).reason, "reason");
    } catch (error) {
      if (error instanceof PrecompileAbiError) {
        return undefined;
      }
      throw error;
    }
  }
  return undefined;
}

/** A perps revert rethrown with the precompile's reason as its message. */
function perpsRevert(error: unknown): unknown {
  if (!(error instanceof GatewayRpcError)) {
    return error;
  }
  const reason = perpsRevertReason(error.data);
  return reason === undefined ? error : new GatewayRpcError(error.code, `The perps precompile refused: ${reason}`, error.data);
}

async function perpsView(data: string, outputs: readonly AbiReadField[]): Promise<AbiReadRecord> {
  try {
    return decodeAbiResult(await ethCall({ to: PERPS_PRECOMPILE, data }), outputs);
  } catch (error) {
    throw perpsRevert(error);
  }
}

/** Runs `data` against 0x1015 as `from` at the latest height; the precompile's revert reason, or undefined when it would succeed. */
export async function perpsPreflight(from: string, data: string): Promise<string | undefined> {
  try {
    await ethCall({ from, to: PERPS_PRECOMPILE, data });
    return undefined;
  } catch (error) {
    const refused = perpsRevert(error);
    if (refused instanceof GatewayRpcError) {
      return refused.message;
    }
    throw refused;
  }
}

export interface PerpsMarket {
  readonly marketId: bigint;
  readonly name: string;
  readonly symbol: string;
  readonly maxLeverage: bigint;
  readonly maintenanceMarginBps: bigint;
  readonly maxOpenInterest: bigint;
  readonly enabled: boolean;
}

const MARKET_FIELDS: readonly AbiReadField[] = [
  { name: "name", type: "string" },
  { name: "symbol", type: "string" },
  { name: "maxLeverage", type: "uint256" },
  { name: "maintenanceMarginBps", type: "uint256" },
  { name: "maxOpenInterest", type: "uint256" },
  { name: "enabled", type: "bool" },
];

/** Every listed market: ids 0 to totalMarkets() - 1, each read with getMarket(id). */
export async function perpsMarkets(): Promise<readonly PerpsMarket[]> {
  const count = readUint((await perpsView(encodeAbiCall("totalMarkets", [], []), [{ name: "count", type: "uint256" }])).count, "count");
  if (count > PERPS_MAX_MARKETS) {
    throw new PrecompileAbiError("invalid_value", `totalMarkets ${count.toString()} above ${PERPS_MAX_MARKETS.toString()}`);
  }
  return Promise.all(
    Array.from({ length: Number(count) }, async (_, index): Promise<PerpsMarket> => {
      const marketId = BigInt(index);
      const market = await perpsView(encodeAbiCall("getMarket", ["uint256"], [marketId]), MARKET_FIELDS);
      return {
        marketId,
        name: readText(market.name, "name"),
        symbol: readText(market.symbol, "symbol"),
        maxLeverage: readUint(market.maxLeverage, "maxLeverage"),
        maintenanceMarginBps: readUint(market.maintenanceMarginBps, "maintenanceMarginBps"),
        maxOpenInterest: readUint(market.maxOpenInterest, "maxOpenInterest"),
        enabled: readFlag(market.enabled, "enabled"),
      };
    }),
  );
}

export interface PerpsPosition {
  readonly positionId: bigint;
  readonly marketId: bigint;
  readonly isLong: boolean;
  readonly sizeUsd: bigint;
  readonly collateralUsd: bigint;
  readonly collateralAmount: bigint;
  readonly entryPrice: bigint;
}

export interface PerpsOrder {
  readonly orderId: bigint;
  readonly marketId: bigint;
  readonly isLong: boolean;
  readonly orderType: bigint;
  readonly triggerPrice: bigint;
  readonly limitPrice: bigint;
  readonly sizeUsd: bigint;
  readonly collateralAmount: bigint;
}

export interface PerpsAccount {
  readonly idle: bigint;
  readonly locked: bigint;
  readonly positions: readonly PerpsPosition[];
  readonly orders: readonly PerpsOrder[];
}

const POSITION_FIELDS: readonly AbiReadField[] = [
  { name: "user", type: "address" },
  { name: "marketId", type: "uint256" },
  { name: "isLong", type: "bool" },
  { name: "sizeUsd", type: "uint256" },
  { name: "collateralUsd", type: "uint256" },
  { name: "collateralToken", type: "address" },
  { name: "collateralAmount", type: "uint256" },
  { name: "entryPrice", type: "uint256" },
  { name: "timestamp", type: "uint256" },
  { name: "active", type: "bool" },
];

const ORDER_FIELDS: readonly AbiReadField[] = [
  { name: "user", type: "address" },
  { name: "marketId", type: "uint256" },
  { name: "isLong", type: "bool" },
  { name: "orderType", type: "uint8" },
  { name: "triggerPrice", type: "uint256" },
  { name: "limitPrice", type: "uint256" },
  { name: "sizeUsd", type: "uint256" },
  { name: "leverage", type: "uint256" },
  { name: "collateralToken", type: "address" },
  { name: "collateralAmount", type: "uint256" },
  { name: "active", type: "bool" },
];

const SUMMARY_FIELDS: readonly AbiReadField[] = [
  { name: "summary", type: { tuple: [
    { name: "idle", type: "uint256" },
    { name: "totalLocked", type: "uint256" },
    { name: "positionCount", type: "uint256" },
  ] } },
];

async function perpsIds(method: "getUserPositionIds" | "getUserOrderIds", owner: string): Promise<readonly bigint[]> {
  const result = await perpsView(encodeAbiCall(method, ["address"], [owner]), [{ name: "ids", type: { array: "uint256" } }]);
  return readList(result.ids, "ids").map((id) => readUint(id, "id"));
}

/** The USDL cross account, open positions and active orders `owner` holds on the perps precompile. */
export async function perpsAccount(owner: string): Promise<PerpsAccount> {
  if (!EVM_ADDRESS.test(owner)) {
    throw new RangeError("an EVM address is required");
  }
  const [summaryResult, positionIds, orderIds] = await Promise.all([
    perpsView(encodeAbiCall("getAccountSummary", ["address", "address"], [owner, PERPS_COLLATERAL_TOKEN]), SUMMARY_FIELDS),
    perpsIds("getUserPositionIds", owner),
    perpsIds("getUserOrderIds", owner),
  ]);
  const summary = readRecord(summaryResult.summary, "summary");
  const [positions, orders] = await Promise.all([
    Promise.all(
      positionIds.map(async (positionId) => {
        const position = await perpsView(encodeAbiCall("getPosition", ["uint256"], [positionId]), POSITION_FIELDS);
        return readFlag(position.active, "active")
          ? [{
              positionId,
              marketId: readUint(position.marketId, "marketId"),
              isLong: readFlag(position.isLong, "isLong"),
              sizeUsd: readUint(position.sizeUsd, "sizeUsd"),
              collateralUsd: readUint(position.collateralUsd, "collateralUsd"),
              collateralAmount: readUint(position.collateralAmount, "collateralAmount"),
              entryPrice: readUint(position.entryPrice, "entryPrice"),
            }]
          : [];
      }),
    ),
    Promise.all(
      orderIds.map(async (orderId) => {
        const order = await perpsView(encodeAbiCall("getOrder", ["uint256"], [orderId]), ORDER_FIELDS);
        return readFlag(order.active, "active")
          ? [{
              orderId,
              marketId: readUint(order.marketId, "marketId"),
              isLong: readFlag(order.isLong, "isLong"),
              orderType: readUint(order.orderType, "orderType"),
              triggerPrice: readUint(order.triggerPrice, "triggerPrice"),
              limitPrice: readUint(order.limitPrice, "limitPrice"),
              sizeUsd: readUint(order.sizeUsd, "sizeUsd"),
              collateralAmount: readUint(order.collateralAmount, "collateralAmount"),
            }]
          : [];
      }),
    ),
  ]);
  return {
    idle: readUint(summary.idle, "idle"),
    locked: readUint(summary.totalLocked, "totalLocked"),
    positions: positions.flat(),
    orders: orders.flat(),
  };
}
