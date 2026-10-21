import { describe, it } from "node:test";
import { strict as assert } from "node:assert";
import { readFileSync, readdirSync, statSync } from "node:fs";
import * as React from "react";
import { renderToStaticMarkup } from "react-dom/server";

type Kind = "query" | "subscription";

interface Operation {
  kind: Kind;
  name: string | null;
  roots: string[];
}

interface GoldenOperation {
  name: string;
  roots: readonly string[];
}

const root = new URL("../", import.meta.url);
const read = (file: string) => readFileSync(new URL(file, root), "utf8");

// contracts/data-services.md section 14: the 51 operations of the
// application and the gateway query roots each one selects.
export const GOLDEN_OPERATIONS: readonly GoldenOperation[] = [
  { name: "GetPositions", roots: ["positions"] },
  { name: "GetClosedPositions", roots: ["positions"] },
  { name: "GetTrades", roots: ["trades"] },
  { name: "GetOrders", roots: ["orders"] },
  { name: "GetMarketInfo", roots: ["market", "marketStats"] },
  { name: "GetMarkets", roots: ["markets"] },
  { name: "GetGlobalStats", roots: ["globalStats"] },
  { name: "GetUserStats", roots: ["userStats"] },
  { name: "GetUserVault", roots: ["userVault"] },
  { name: "GetFilledOrders", roots: ["orders"] },
  { name: "GetAllOrders", roots: ["orders"] },
  { name: "GetLiquidations", roots: ["liquidations"] },
  { name: "GetLatestPrices", roots: ["latestPrices"] },
  { name: "GetVaultEvents", roots: ["vaultEvents"] },
  { name: "GetAccountLedger", roots: ["accountLedger"] },
  { name: "GetPositionLedger", roots: ["accountLedger"] },
  { name: "GetDelegates", roots: ["delegates"] },
  { name: "GetTradingAccountEvents", roots: ["tradingAccountEvents"] },
  { name: "GetKeeperCycles", roots: ["keeperCycles"] },
  { name: "GetTPSLOrders", roots: ["orders"] },
  { name: "DashboardV2", roots: ["userVault", "positions", "orders", "delegates", "userStats"] },
  { name: "GetAllOrdersV2", roots: ["orders"] },
  { name: "GetLeaderboard", roots: ["leaderboard"] },
  { name: "GetFees", roots: ["fees"] },
  { name: "GetTradeSettlements", roots: ["tradeSettlements"] },
  { name: "GetOiSnapshots", roots: ["oiSnapshots"] },
  { name: "GetMarkPriceHistory", roots: ["markPriceHistory"] },
  { name: "GetVaultBalanceHistory", roots: ["vaultBalanceHistory"] },
  { name: "GetFundingPayments", roots: ["fundingPayments"] },
  { name: "GetMarketSnapshots", roots: ["marketSnapshots"] },
  { name: "GetProtocolSnapshots", roots: ["protocolSnapshots"] },
  { name: "GetEnrichedGlobalStats", roots: ["enrichedGlobalStats"] },
  { name: "GetEnrichedMarketStats", roots: ["enrichedMarketStats"] },
  { name: "GetOrderlyTickers", roots: ["orderlyTickers"] },
  { name: "GetOrderlyFundingRates", roots: ["orderlyFundingRates"] },
  { name: "GetOrderlyPriceChanges", roots: ["orderlyPriceChanges"] },
  { name: "GetOrderlyOpenInterests", roots: ["orderlyOpenInterests"] },
  { name: "GetSpotMarkets", roots: ["spotMarkets"] },
  { name: "GetSpotMarket", roots: ["spotMarket"] },
  { name: "GetSpotOrders", roots: ["spotOrders"] },
  { name: "GetSpotOrder", roots: ["spotOrder"] },
  { name: "GetSpotOrderFills", roots: ["spotOrderFills"] },
  { name: "GetSpotBatchClearings", roots: ["spotBatchClearings"] },
  { name: "GetSpotEpochSettlements", roots: ["spotEpochSettlements"] },
  { name: "GetSpotFastSettlements", roots: ["spotFastSettlements"] },
  { name: "GetSpotCollateralMovements", roots: ["spotCollateralMovements"] },
  { name: "GetSpotMarketModeChanges", roots: ["spotMarketModeChanges"] },
  { name: "GetSpotPofqUpdates", roots: ["spotPofqUpdates"] },
  { name: "GetSpotPLVs", roots: ["spotPLVs"] },
  { name: "GetSpotKeeperCycles", roots: ["spotKeeperCycles"] },
  { name: "GetSpotBatchClearFailures", roots: ["spotBatchClearFailures"] },
];

// contracts/data-services.md section 27: the PerfDB operations that join
// the operation set on the gateway endpoint.
const GOLDEN_PERFDB_OPERATIONS: readonly GoldenOperation[] = [
  { name: "User", roots: ["user"] },
  { name: "UserBalances", roots: ["userBalances"] },
  { name: "UserDailyPnl", roots: ["userDailyPnl"] },
  { name: "UserStats", roots: ["userStats"] },
  { name: "UserAlerts", roots: ["userAlerts"] },
  { name: "UserReferrals", roots: ["userReferrals"] },
  { name: "ReferralTiers", roots: ["referralTiers"] },
  { name: "Competitions", roots: ["competitions"] },
  { name: "Competition", roots: ["competition"] },
];

// contracts/data-services.md sections 13 and 27: the query roots the
// gateway serves.
const GATEWAY_QUERY_ROOTS: ReadonlySet<string> = new Set([
  "position", "positions", "trades", "order", "orders", "liquidations",
  "market", "markets", "latestPrices", "priceHistory", "fundingRates",
  "userVault", "vaultEvents", "collateralTokens", "poolStates", "poolState",
  "feeConfig", "protocolEvents", "userStats", "marketStats", "globalStats",
  "indexerStatus", "keeperCycles", "accountLedger", "delegates",
  "tradingAccountEvents", "fees", "tradeSettlements", "oiSnapshots",
  "markPriceHistory", "vaultBalanceHistory", "fundingPayments",
  "marketSnapshots", "protocolSnapshots", "enrichedGlobalStats",
  "enrichedMarketStats", "orderlyTickers", "orderlyFundingRates",
  "orderlyPriceChanges", "orderlyOpenInterests", "spotMarket", "spotMarkets",
  "spotOrder", "spotOrders", "spotOrderFills", "spotBatchClearings",
  "spotEpochSettlements", "spotFastSettlements", "spotCollateralMovements",
  "spotMarketModeChanges", "spotPofqUpdates", "spotPLVs", "spotKeeperCycles",
  "spotBatchClearFailures", "leaderboard",
  "user", "userBalances", "userDailyPnl", "userAlerts", "userReferrals",
  "referralTiers", "competitions", "competition",
]);

// contracts/data-services.md section 15: the subscription fields.
const GATEWAY_SUBSCRIPTION_ROOTS: ReadonlySet<string> = new Set([
  "indexerStatusUpdated", "latestPricesUpdated", "priceUpdated",
  "positionChanged", "orderChanged", "tradeCreated", "liquidationCreated",
  "tradeExecuted",
]);

// req 6016 ac_6: the fields of the application schema the gateway has no
// source for, which the application reads null-safe.
const GATEWAY_ONLY_FIELDS: readonly (readonly [string, string])[] = [
  ["PoolState", "baseReserve"],
  ["PoolState", "quoteReserve"],
  ["MarkPriceRecord", "baseReserve"],
  ["MarkPriceRecord", "quoteReserve"],
  ["SpotMarket", "marketId"],
  ["SpotMarket", "baseToken"],
  ["SpotMarket", "quoteToken"],
  ["SpotMarket", "mode"],
  ["SpotMarket", "status"],
  ["SpotOrder", "side"],
  ["SpotOrder", "offsetBps"],
  ["SpotOrder", "size"],
  ["SpotOrder", "filledSize"],
  ["SpotOrder", "marketId"],
  ["SpotOrderFill", "fillScore"],
  ["LatestPrice", "onchainTimestamp"],
  ["Market", "maxLeverage"],
  ["Trade", "isLong"],
  ["FeeConfig", "takerFeeBps"],
  ["FeeConfig", "makerFeeBps"],
  ["FeeConfig", "liquidationFeeBps"],
  ["FeeConfig", "insuranceFeeBps"],
];

function closing(text: string, open: number): number {
  const pair: Record<string, string> = { "{": "}", "(": ")" };
  const opener = text[open]!;
  let depth = 0;
  for (let i = open; i < text.length; i++) {
    if (text[i] === opener) depth++;
    else if (text[i] === pair[opener]) {
      depth--;
      if (depth === 0) return i;
    }
  }
  throw new Error(`unbalanced ${opener} at ${open}`);
}

function parseOperation(document: string): Operation {
  const text = document.replace(/#[^\n]*/g, "");
  const head = /^\s*(query|subscription)\b\s*([A-Za-z_]\w*)?/.exec(text);
  if (!head) throw new Error(`not an operation: ${document.trim().slice(0, 40)}`);
  let i = head[0].length;
  while (/\s/.test(text[i]!)) i++;
  if (text[i] === "(") i = closing(text, i) + 1;
  const open = text.indexOf("{", i);
  const end = closing(text, open);
  const roots: string[] = [];
  const field = /\s*([A-Za-z_]\w*)\s*(?::\s*([A-Za-z_]\w*))?\s*/y;
  let at = open + 1;
  while (at < end) {
    field.lastIndex = at;
    const m = field.exec(text);
    if (!m || m.index !== at) {
      if (/^\s*$/.test(text.slice(at, end))) break;
      throw new Error(`unexpected selection at ${text.slice(at, at + 20)}`);
    }
    const name = m[2] ?? m[1]!;
    if (!roots.includes(name)) roots.push(name);
    at = field.lastIndex;
    if (text[at] === "(") at = closing(text, at) + 1;
    while (/\s/.test(text[at]!)) at++;
    if (text[at] === "{") at = closing(text, at) + 1;
  }
  return { kind: head[1] as Kind, name: head[2] ?? null, roots };
}

function operationsIn(file: string, kind?: Kind): Operation[] {
  const templates = [...read(file).matchAll(/\/\* GraphQL \*\/\s*`([^`]*)`/g)];
  return templates
    .map((t) => parseOperation(t[1]!))
    .filter((op) => kind === undefined || op.kind === kind);
}

function sdlFields(sdl: string): Map<string, Map<string, string>> {
  const types = new Map<string, Map<string, string>>();
  const text = sdl.replace(/#[^\n]*/g, "");
  for (const block of text.matchAll(/\btype\s+([A-Za-z_]\w*)\s*\{/g)) {
    const open = block.index! + block[0].length - 1;
    const body = text.slice(open + 1, closing(text, open));
    const fields = new Map<string, string>();
    for (const f of body.matchAll(/([A-Za-z_]\w*)\s*(?:\([^)]*\))?\s*:\s*([[\]\w!]+)/g)) {
      fields.set(f[1]!, f[2]!);
    }
    types.set(block[1]!, fields);
  }
  return types;
}

function sourceFiles(dir: string): string[] {
  return readdirSync(new URL(dir, root)).flatMap((entry) => {
    const rel = `${dir}/${entry}`;
    if (statSync(new URL(rel, root)).isDirectory()) return sourceFiles(rel);
    return /\.tsx?$/.test(entry) ? [rel] : [];
  });
}

const golden = (ops: readonly GoldenOperation[]) => ops.map((op) => ({ name: op.name, roots: [...op.roots] }));
const named = (ops: Operation[]) => ops.map((op) => ({ name: op.name, roots: op.roots }));

const SUBGRAPH = "src/hooks/perps/useSubgraph.ts";
const SUBSCRIPTION_FILES = [
  "src/hooks/perps/useIndexerSubscriptions.ts",
  "src/hooks/perps/usePortfolioStats.ts",
  "src/widgets/trading/PerpsPositionsPanel.tsx",
];
const PERFDB_FILES = [
  "src/hooks/perps/usePerfdbUser.ts",
  "src/hooks/perps/usePerfdbBalances.ts",
  "src/hooks/perps/useDailyPnl.ts",
  "src/hooks/perps/usePerfdbTraderStats.ts",
  "src/hooks/perps/usePerfdbAlerts.ts",
  "src/hooks/perps/usePerfdbReferrals.ts",
  "src/hooks/perps/usePerfdbCompetitions.ts",
  "src/hooks/perps/usePerfdbSubscriptions.ts",
];

describe("graphql operations", () => {
  it("subgraphOperationsMatchGolden", () => {
    const ops = operationsIn(SUBGRAPH);
    assert.equal(ops.length, 51);
    assert.equal(GOLDEN_OPERATIONS.length, 51);
    assert.ok(ops.every((op) => op.kind === "query"));
    assert.deepEqual(named(ops), golden(GOLDEN_OPERATIONS));
  });

  it("subscriptionsMatchGolden", () => {
    const ops = SUBSCRIPTION_FILES.flatMap((file) => operationsIn(file, "subscription"));
    assert.equal(ops.length, 9);
    const distinct = [...new Map(ops.map((op) => [`${op.name}:${op.roots.join()}`, op])).values()];
    assert.deepEqual(named(distinct), [
      { name: null, roots: ["indexerStatusUpdated"] },
      { name: null, roots: ["latestPricesUpdated"] },
      { name: "PriceUpdated", roots: ["priceUpdated"] },
      { name: "PositionChanged", roots: ["positionChanged"] },
      { name: "OrderChanged", roots: ["orderChanged"] },
      { name: "TradeCreated", roots: ["tradeCreated"] },
      { name: "Liquidations", roots: ["liquidationCreated"] },
    ]);
    for (const op of ops) {
      for (const field of op.roots) assert.ok(GATEWAY_SUBSCRIPTION_ROOTS.has(field), field);
    }
  });

  it("perfdbOperationsMatchGolden", () => {
    const queries = PERFDB_FILES.flatMap((file) => operationsIn(file, "query"));
    assert.deepEqual(named(queries), [
      { name: "User", roots: ["user"] },
      { name: "UserBalances", roots: ["userBalances"] },
      { name: "UserDailyPnl", roots: ["userDailyPnl"] },
      { name: "UserStats", roots: ["userStats"] },
      { name: "UserAlerts", roots: ["userAlerts"] },
      { name: "UserReferrals", roots: ["userReferrals", "referralTiers"] },
      { name: "ReferralTiers", roots: ["referralTiers"] },
      { name: "Competitions", roots: ["competitions"] },
      { name: "Competition", roots: ["competition"] },
    ]);
    const goldenNames = GOLDEN_PERFDB_OPERATIONS.map((op) => op.name);
    assert.deepEqual(queries.map((op) => op.name), goldenNames);
    for (const op of queries) {
      for (const field of op.roots) assert.ok(GATEWAY_QUERY_ROOTS.has(field), field);
    }
    const subscriptions = PERFDB_FILES.flatMap((file) => operationsIn(file, "subscription"));
    assert.deepEqual(named(subscriptions), [
      { name: "PriceUpdated", roots: ["priceUpdated"] },
      { name: "TradeExecuted", roots: ["tradeExecuted"] },
    ]);
    for (const op of subscriptions) {
      for (const field of op.roots) assert.ok(GATEWAY_SUBSCRIPTION_ROOTS.has(field), field);
    }
  });

  it("applicationIsSubsetOfGateway", () => {
    const goldenByName = new Map(GOLDEN_OPERATIONS.map((op) => [op.name, op]));
    for (const op of operationsIn(SUBGRAPH)) {
      const entry = goldenByName.get(op.name ?? "");
      assert.ok(entry, `operation ${op.name} is not served by the gateway`);
      for (const field of op.roots) {
        assert.ok(entry.roots.includes(field), `${op.name} selects ${field}`);
        assert.ok(GATEWAY_QUERY_ROOTS.has(field), `${field} is not a gateway root`);
      }
    }
    const appQuery = sdlFields(read("src/core/perps/perpetual.graphql")).get("Query")!;
    assert.equal(appQuery.has("leaderboard"), false);
    assert.equal(GATEWAY_QUERY_ROOTS.has("leaderboard"), true);
    assert.deepEqual(goldenByName.get("GetLeaderboard")?.roots, ["leaderboard"]);
    for (const field of appQuery.keys()) assert.ok(GATEWAY_QUERY_ROOTS.has(field), field);
  });

  it("gatewayOnlyFieldsAreNullable", async () => {
    const types = sdlFields(read("src/core/perps/perpetual.graphql"));
    for (const [type, field] of GATEWAY_ONLY_FIELDS) {
      const declared = types.get(type)?.get(field);
      assert.ok(declared, `${type}.${field} is declared`);
      assert.equal(declared.endsWith("!"), false, `${type}.${field} is ${declared}`);
    }
    assert.equal(types.get("Trade")?.get("tradeType"), "String!");

    const distinctive = [
      "baseReserve", "quoteReserve", "fillScore", "offsetBps", "filledSize",
      "onchainTimestamp", "takerFeeBps", "makerFeeBps", "liquidationFeeBps",
      "insuranceFeeBps",
    ];
    const access = new RegExp(`\\.(${distinctive.join("|")})\\b`);
    const readers = sourceFiles("src")
      .filter((file) => !file.startsWith("src/core/network/abi/") && !file.startsWith("src/types/"))
      .filter((file) => access.test(read(file)));
    assert.deepEqual(readers, []);

    Object.assign(globalThis, { React });
    const { MobilePortfolioOverview } = await import("../src/widgets/portfolio/MobilePortfolioOverview");
    const noop = () => {};
    const trade = {
      id: 7,
      positionId: "3",
      userAddress: "0x0000000000000000000000000000000000000001",
      marketId: 2,
      tradeType: "liquidation",
      isLong: null,
      sizeUsd: "1500000000000000000000",
      price: "1000000000000000000",
      realizedPnl: null,
      feeUsd: null,
      blockNumber: 10,
      txHash: "0x01",
      blockTimestamp: "2026-01-01T00:00:00Z",
    };
    const html = renderToStaticMarkup(
      React.createElement(MobilePortfolioOverview, {
        address: null,
        totalVaultUsd: 0,
        totalPnl: 0,
        userStats: null,
        balances: [],
        trades: [trade],
        vaultEvents: [],
        isLoading: false,
        onRefresh: noop,
        action: "deposit",
        onActionChange: noop,
        selectedToken: "",
        onSelectedTokenChange: noop,
        tokenOptions: [],
        amount: "",
        onAmountChange: noop,
        maxAmount: "",
        isProcessing: false,
        txStep: "idle",
        error: null,
        success: null,
        onSubmit: noop,
        isConnected: true,
        hasVault: true,
        isCreatingVault: false,
        onConnect: noop,
        onCreateVault: noop,
      }),
    );
    assert.ok(html.includes(`<div class="text-[#828282]">liquidation</div>`));
    assert.ok(html.includes(`<div class="font-mono text-white">$1.50K</div>`));
    assert.equal(html.includes("No trades yet"), false);
  });
});
