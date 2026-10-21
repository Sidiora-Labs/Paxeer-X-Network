import { describe, it } from "node:test";
import assert from "node:assert/strict";
import { existsSync, readFileSync, readdirSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import ts from "typescript";
import type { Address } from "viem";

import { PERPS_SPENDER, planApproval } from "../src/core/network/approvals";
import { SPOT_TOKENS } from "../src/core/spot/addresses";

const ROOT = fileURLToPath(new URL("..", import.meta.url));
const SCAN_ROOTS = ["src/core", "src/hooks", "src/widgets", "src/app", "src/pages", "src/page-modules"] as const;
const SOURCE_EXT = /\.(ts|tsx|js|jsx|mjs|cjs)$/;

const ABI_DIR = "src/core/network/abi";
const SPOT_ABI_DIR = "src/core/network/spot_abi";
const PERPS_ABI = "src/core/network/abi/PerpsPrecompile.ts";
const PRECOMPILE_MODULE = "src/core/network/precompiles.ts";
const APPROVAL_PLANNER = "src/core/network/approvals.ts";
const EXACT_APPROVAL_WRITER = "src/hooks/spot/useSpotApprovals.ts";
const SPOT_TOKEN_MODULE = "src/core/spot/addresses.ts";
const GRAPHQL_TRANSPORT_WS = "src/core/graphql/client.ts";
const EMBEDDED_WALLET_CLIENT = "src/core/wallet/embedded/client.ts";
const ENDPOINT_MODULES = new Set([
  "src/core/feeds.ts",
  "src/core/network/gateway.ts",
  "src/core/rpc.ts",
  "src/core/chain.ts",
  "src/pages/api/candle/[...path].ts",
]);

const DIAMOND_ADDRESSES = ["0xFEf3751eFA8d861C54A58A8041a2F8814BbBA623", "0xeA65FE02665852c615774A3041DFE6f00fb77537"];
const PRECOMPILE_ADDRESSES = new Set([
  "0x0000000000000000000000000000000000001015",
  "0x0000000000000000000000000000000000001017",
  "0x0000000000000000000000000000000000001021",
  "0x0000000000000000000000000000000000001022",
  "0x0000000000000000000000000000000000001023",
  "0x0000000000000000000000000000000000001024",
  "0x0000000000000000000000000000000000001025",
]);
const POINTER_VIEW_ADDRESS = "0x000000000000000000000000000000000000100a";
const USDL_ADDRESS = "0x85fcd13735f4309833a503ee804ea32395851479";
const NATIVE_SENTINEL = "0x0000000000000000000000000000000000000000";
const ADDRESS_RE = /(?<![0-9a-fA-F])0x[0-9a-fA-F]{40}(?![0-9a-fA-F])/g;

const FACET_ABI_NAMES = new Set([
  "PositionFacetABI",
  "OrderBookFacetABI",
  "QuoterFacetABI",
  "TradingAccountABI",
  "VaultFactoryFacetABI",
  "OracleFacetABI",
]);
const BARREL_SOURCES = new Set(["./PerpsPrecompile", "./IERC20"]);
const ABI_DIR_ENTRIES = /^(PerpsPrecompile\.ts|IERC20\.(ts|js|json)|index\.(ts|js))$/;

const MARGIN_MODE_NAMES = /\b(setMarginMode|MarginModeToggle|TransferMarginModal|useTransferMargin)\b/g;
const LEGACY_TOKENS = new Set(["WPAX9", "USDC", "USDT", "WETH", "WBNB"]);
const SPOT_ERROR_NAMES = new Set(["UseWethDeposit", "UseWethWithdraw", "WethOnly"]);
const CHAINFLOW_SYMBOL_SUFFIX = /_USDC(?![A-Za-z0-9])/g;
const UNBOUNDED_MARKET_WRITES = new Set(["openPosition", "addSize", "closePosition", "partialClose"]);
const UNBOUNDED_APPROVALS = new Set(["maxUint256", "MaxUint256", "approveMax", "MAX_UINT256"]);
const LEVERAGE_OR_PRICE = /^lev$|leverage|price/i;
const GAP_CLOSE_CODE = 4000;

const GATEWAY_QUERY_OPERATIONS = new Set([
  "GetPositions",
  "GetClosedPositions",
  "GetTrades",
  "GetOrders",
  "GetMarketInfo",
  "GetMarkets",
  "GetGlobalStats",
  "GetUserStats",
  "GetUserVault",
  "GetFilledOrders",
  "GetAllOrders",
  "GetLiquidations",
  "GetLatestPrices",
  "GetVaultEvents",
  "GetAccountLedger",
  "GetPositionLedger",
  "GetDelegates",
  "GetTradingAccountEvents",
  "GetKeeperCycles",
  "GetTPSLOrders",
  "DashboardV2",
  "GetAllOrdersV2",
  "GetLeaderboard",
  "GetFees",
  "GetTradeSettlements",
  "GetOiSnapshots",
  "GetMarkPriceHistory",
  "GetVaultBalanceHistory",
  "GetFundingPayments",
  "GetMarketSnapshots",
  "GetProtocolSnapshots",
  "GetEnrichedGlobalStats",
  "GetEnrichedMarketStats",
  "GetOrderlyTickers",
  "GetOrderlyFundingRates",
  "GetOrderlyPriceChanges",
  "GetOrderlyOpenInterests",
  "GetSpotMarkets",
  "GetSpotMarket",
  "GetSpotOrders",
  "GetSpotOrder",
  "GetSpotOrderFills",
  "GetSpotBatchClearings",
  "GetSpotEpochSettlements",
  "GetSpotFastSettlements",
  "GetSpotCollateralMovements",
  "GetSpotMarketModeChanges",
  "GetSpotPofqUpdates",
  "GetSpotPLVs",
  "GetSpotKeeperCycles",
  "GetSpotBatchClearFailures",
  "User",
  "UserBalances",
  "UserDailyPnl",
  "UserStats",
  "UserAlerts",
  "UserReferrals",
  "ReferralTiers",
  "Competitions",
  "Competition",
]);
const GATEWAY_SUBSCRIPTION_FIELDS = new Set([
  "indexerStatusUpdated",
  "latestPricesUpdated",
  "priceUpdated",
  "positionChanged",
  "orderChanged",
  "tradeCreated",
  "liquidationCreated",
  "tradeExecuted",
]);
const GRAPHQL_OPERATION = /^\s*(query|mutation|subscription)\b\s*([A-Za-z_]\w*)?\s*(?:\([^)]*\))?\s*\{\s*([A-Za-z_]\w*)/;

const OUTBOUND_PAGE_HOSTS = new Set([
  "paxeer.network",
  "hyperpaxeer.com",
  "hyperpaxstatus.statuspage.io",
  "t.me",
  "support.google.com",
  "support.mozilla.org",
  "support.apple.com",
  "support.microsoft.com",
  "raw.githubusercontent.com",
]);
const EMBEDDED_WALLET_HOSTS = new Set(["connect.paxportwallet.com", "zezsqawedbikldiedlse.supabase.co"]);
const SCHEME_LITERAL = /^(https?|wss?):\/\/([^/?#\s]*)/i;

const GATEWAY_ORIGIN = "https://gateway.invalid";
const E18 = 10n ** 18n;
const BPS = 10_000n;

type TokenKind = "identifier" | "string" | "regex";

interface Token {
  kind: TokenKind;
  text: string;
  line: number;
  node: ts.Node;
}

interface Parsed {
  source: ts.SourceFile;
  tokens: readonly Token[];
}

const SCRIPT_KINDS: Record<string, ts.ScriptKind> = {
  ".ts": ts.ScriptKind.TS,
  ".tsx": ts.ScriptKind.TSX,
  ".js": ts.ScriptKind.JS,
  ".jsx": ts.ScriptKind.JSX,
  ".mjs": ts.ScriptKind.JS,
  ".cjs": ts.ScriptKind.JS,
};

const read = (file: string): string => readFileSync(path.join(ROOT, file), "utf8");

function walk(dir: string): string[] {
  if (!existsSync(path.join(ROOT, dir))) return [];
  return readdirSync(path.join(ROOT, dir), { withFileTypes: true })
    .flatMap((entry) => (entry.isDirectory() ? walk(`${dir}/${entry.name}`) : [`${dir}/${entry.name}`]))
    .sort();
}

function sources(roots: readonly string[]): string[] {
  return roots.flatMap(walk).filter((file) => SOURCE_EXT.test(file));
}

function lineOf(source: ts.SourceFile, node: ts.Node): number {
  return source.getLineAndCharacterOfPosition(node.getStart(source)).line + 1;
}

function tokenKind(node: ts.Node): TokenKind | null {
  if (ts.isIdentifier(node) || ts.isPrivateIdentifier(node)) return "identifier";
  if (
    ts.isStringLiteral(node) ||
    ts.isNoSubstitutionTemplateLiteral(node) ||
    ts.isTemplateHead(node) ||
    ts.isTemplateMiddle(node) ||
    ts.isTemplateTail(node)
  ) {
    return "string";
  }
  if (ts.isRegularExpressionLiteral(node)) return "regex";
  return null;
}

function tokenText(node: ts.Node): string {
  if (ts.isIdentifier(node) || ts.isPrivateIdentifier(node)) return node.text;
  return (node as ts.LiteralLikeNode).text;
}

const parsed = new Map<string, Parsed>();

function parse(file: string): Parsed {
  const cached = parsed.get(file);
  if (cached) return cached;
  const source = ts.createSourceFile(file, read(file), ts.ScriptTarget.Latest, true, SCRIPT_KINDS[path.extname(file)]);
  const tokens: Token[] = [];
  const visit = (node: ts.Node): void => {
    const kind = tokenKind(node);
    if (kind) tokens.push({ kind, text: tokenText(node), line: lineOf(source, node), node });
    ts.forEachChild(node, visit);
  };
  visit(source);
  const result = { source, tokens };
  parsed.set(file, result);
  return result;
}

function collect<T extends ts.Node>(root: ts.Node, guard: (node: ts.Node) => node is T): T[] {
  const found: T[] = [];
  const visit = (node: ts.Node): void => {
    if (guard(node)) found.push(node);
    ts.forEachChild(node, visit);
  };
  visit(root);
  return found;
}

function nodes<T extends ts.Node>(file: string, guard: (node: ts.Node) => node is T): T[] {
  return collect(parse(file).source, guard);
}

function at(file: string, node: ts.Node): string {
  return `${file}:${lineOf(parse(file).source, node)}`;
}

function propertyName(name: ts.PropertyName): string | null {
  if (ts.isIdentifier(name) || ts.isStringLiteral(name) || ts.isNoSubstitutionTemplateLiteral(name)) return name.text;
  return null;
}

function stringProperty(node: ts.PropertyAssignment): string | null {
  const value = node.initializer;
  return ts.isStringLiteral(value) || ts.isNoSubstitutionTemplateLiteral(value) ? value.text : null;
}

function identifierSegments(name: string): string[] {
  return name
    .split(/_|(?<=[a-z0-9])(?=[A-Z])|(?<=[A-Z])(?=[A-Z][a-z])/)
    .filter((segment) => segment !== "")
    .map((segment) => segment.toUpperCase());
}

function isWethView(file: string, node: ts.Node): boolean {
  const parent = node.parent;
  if (ts.isLiteralTypeNode(parent)) return true;
  if (!ts.isPropertyAssignment(parent) || parent.initializer !== node) return false;
  const key = propertyName(parent.name);
  return key === "functionName" || (key === "name" && file.startsWith(`${SPOT_ABI_DIR}/`));
}

function pointerEntryAddresses(): Set<string> {
  const found = new Set<string>();
  for (const entry of nodes(SPOT_TOKEN_MODULE, ts.isPropertyAssignment)) {
    const symbol = propertyName(entry.name);
    if ((symbol !== "PAX" && symbol !== "SID") || !ts.isObjectLiteralExpression(entry.initializer)) continue;
    for (const field of entry.initializer.properties) {
      if (!ts.isPropertyAssignment(field) || propertyName(field.name) !== "address") continue;
      for (const literal of field.initializer.getText(parse(SPOT_TOKEN_MODULE).source).match(ADDRESS_RE) ?? []) {
        found.add(literal.toLowerCase());
      }
    }
  }
  return found;
}

async function loadPerps() {
  process.env.NEXT_PUBLIC_PERPS_GATEWAY_URL ??= GATEWAY_ORIGIN;
  const [calls, config, slippage] = await Promise.all([
    import("../src/core/perps/calls"),
    import("../src/core/perps/config"),
    import("../src/core/spot/slippage"),
  ]);
  return { calls, config, slippage };
}

describe("v6120Invariants", () => {
  it("noDiamondAddressRemains", () => {
    const files = [...walk("src").filter((file) => /\.(ts|tsx)$/.test(file)), ...walk("src/env")];
    const hits: string[] = [];
    for (const file of new Set(files)) {
      read(file)
        .split("\n")
        .forEach((line, index) => {
          for (const diamond of DIAMOND_ADDRESSES) {
            if (line.toLowerCase().includes(diamond.toLowerCase())) hits.push(`${file}:${index + 1} ${diamond}`);
          }
        });
    }
    assert.deepEqual(hits, []);
  });

  it("onlyAddressTableAddresses", () => {
    const allowed = new Set([
      ...PRECOMPILE_ADDRESSES,
      POINTER_VIEW_ADDRESS,
      USDL_ADDRESS,
      NATIVE_SENTINEL,
      ...pointerEntryAddresses(),
    ]);
    const hits: string[] = [];
    for (const file of sources(SCAN_ROOTS)) {
      for (const token of parse(file).tokens) {
        if (token.kind !== "string") continue;
        for (const literal of token.text.match(ADDRESS_RE) ?? []) {
          const address = literal.toLowerCase();
          if (PRECOMPILE_ADDRESSES.has(address) && file !== PRECOMPILE_MODULE) {
            hits.push(`${file}:${token.line} ${literal} outside ${PRECOMPILE_MODULE}`);
          } else if (!allowed.has(address)) {
            hits.push(`${file}:${token.line} ${literal}`);
          }
        }
      }
    }
    assert.deepEqual(hits, []);
  });

  it("parseEtherOnlyForLeverageAndPrices", () => {
    const hits: string[] = [];
    for (const file of sources(["src/widgets", "src/hooks/perps"])) {
      const aliases = new Set(["parseEther"]);
      for (const specifier of nodes(file, ts.isImportSpecifier)) {
        if ((specifier.propertyName ?? specifier.name).text === "parseEther") aliases.add(specifier.name.text);
      }
      for (const call of nodes(file, ts.isCallExpression)) {
        const callee = call.expression;
        const name = ts.isIdentifier(callee)
          ? callee.text
          : ts.isPropertyAccessExpression(callee)
            ? callee.name.text
            : null;
        if (name === null || !aliases.has(name)) continue;
        const names = call.arguments.flatMap((argument) =>
          collect(argument, ts.isIdentifier).map((identifier) => identifier.text),
        );
        if (!names.some((identifier) => LEVERAGE_OR_PRICE.test(identifier))) {
          hits.push(`${at(file, call)} ${call.getText(parse(file).source)}`);
        }
      }
    }
    assert.deepEqual(hits, []);
  });

  it("approvalsAreExactAndNeverNative", async () => {
    const hits: string[] = [];
    for (const file of sources(SCAN_ROOTS)) {
      for (const token of parse(file).tokens) {
        if (token.kind !== "identifier" || !UNBOUNDED_APPROVALS.has(token.text)) continue;
        if (file === APPROVAL_PLANNER && token.text === "MAX_UINT256") continue;
        hits.push(`${file}:${token.line} ${token.text}`);
      }
      if (file === APPROVAL_PLANNER || file === EXACT_APPROVAL_WRITER) continue;
      for (const property of nodes(file, ts.isPropertyAssignment)) {
        if (propertyName(property.name) === "functionName" && stringProperty(property) === "approve") {
          hits.push(`${at(file, property)} approve outside ${APPROVAL_PLANNER}`);
        }
      }
    }
    assert.deepEqual(hits, []);

    const usdl = SPOT_TOKENS.USDL.address;
    const pax = SPOT_TOKENS.PAX.address;
    const native = new Set<Address>([pax]);
    let reads = 0;
    const allowance = (value: bigint) => async () => {
      reads += 1;
      return value;
    };
    assert.equal(usdl.toLowerCase(), USDL_ADDRESS);
    assert.deepEqual(await planApproval(null, PERPS_SPENDER, allowance(0n), 5_000_000n, native), []);
    assert.deepEqual(await planApproval(pax, PERPS_SPENDER, allowance(0n), 5_000_000n, native), []);
    assert.equal(reads, 0);
    assert.deepEqual(await planApproval(usdl, PERPS_SPENDER, allowance(4_999_999n), 5_000_000n, native), [
      { address: usdl, functionName: "approve", args: [PERPS_SPENDER, 5_000_000n] },
    ]);
    assert.deepEqual(await planApproval(usdl, PERPS_SPENDER, allowance(5_000_000n), 5_000_000n, native), []);
    assert.equal(reads, 2);
    await assert.rejects(planApproval(usdl, PERPS_SPENDER, allowance(0n), (1n << 256n) - 1n, native), RangeError);
  });

  it("marketWritesCarryBoundAndDeadline", async () => {
    const hits: string[] = [];
    for (const file of sources(SCAN_ROOTS)) {
      if (file.startsWith(`${ABI_DIR}/`) || file.startsWith(`${SPOT_ABI_DIR}/`)) continue;
      for (const property of nodes(file, ts.isPropertyAssignment)) {
        const method = stringProperty(property);
        if (propertyName(property.name) === "functionName" && method !== null && UNBOUNDED_MARKET_WRITES.has(method)) {
          hits.push(`${at(file, property)} ${method}`);
        }
      }
    }
    assert.deepEqual(hits, []);

    const { calls, config, slippage } = await loadPerps();
    const bps = BigInt(config.DEFAULT_SLIPPAGE_BPS);
    const window = BigInt(slippage.DEFAULT_MARKET_DEADLINE_SECONDS);
    const entryPrice = 50_000n * E18;
    const exitPrice = 49_000n * E18;
    const before = BigInt(Math.floor(Date.now() / 1000));
    const open = calls.buildOpenPosition({
      marketId: 0n,
      isLong: true,
      collateralAmount: 100_000_000n,
      leverage: 20n * E18,
      quote: { entryPrice, sizeUsd: 2_000n * E18 },
    });
    const addSize = calls.buildAddSize({
      position: { marketId: 1n, isLong: false },
      additionalCollateral: 10_000_000n,
      leverage: 5n * E18,
      quote: { entryPrice, sizeUsd: 50n * E18 },
    });
    const close = calls.buildClosePosition({ positionId: 7n, isLong: true, sizeUsd: 2_000n * E18, exitPrice });
    const partial = calls.buildPartialClose({ positionId: 8n, isLong: false, closeSizeUsd: 1_000n * E18, exitPrice });
    const after = BigInt(Math.floor(Date.now() / 1000));
    const inWindow = (deadline: bigint) => deadline >= before + window && deadline <= after + window;

    assert.equal(open.functionName, "placeOrder");
    assert.equal(open.args[0].acceptablePrice, (entryPrice * (BPS + bps)) / BPS);
    assert.ok(inWindow(open.args[0].deadline));
    assert.equal(addSize.functionName, "placeOrder");
    assert.equal(addSize.args[0].acceptablePrice, (entryPrice * (BPS - bps)) / BPS);
    assert.ok(inWindow(addSize.args[0].deadline));
    assert.equal(close.functionName, "closePositionWithParams");
    assert.equal(close.args[2], (exitPrice * (BPS - bps)) / BPS);
    assert.ok(inWindow(close.args[3]));
    assert.equal(partial.functionName, "closePositionWithParams");
    assert.equal(partial.args[2], (exitPrice * (BPS + bps)) / BPS);
    assert.ok(inWindow(partial.args[3]));
  });

  it("socketsGuardSequence", () => {
    const hits: string[] = [];
    for (const file of sources(SCAN_ROOTS)) {
      if (file === GRAPHQL_TRANSPORT_WS) continue;
      const sockets = nodes(file, ts.isNewExpression).filter(
        (expression) => ts.isIdentifier(expression.expression) && expression.expression.text === "WebSocket",
      );
      if (sockets.length === 0) continue;
      const calls = nodes(file, ts.isCallExpression);
      const gapCodes = new Set(
        nodes(file, ts.isVariableDeclaration).flatMap(({ name, initializer }) =>
          ts.isIdentifier(name) &&
          initializer &&
          ts.isNumericLiteral(initializer) &&
          Number(initializer.text) === GAP_CLOSE_CODE
            ? [name.text]
            : [],
        ),
      );
      const guarded = calls.some(
        (call) => ts.isIdentifier(call.expression) && call.expression.text === "sequenceGuard",
      );
      const closesOnGap = calls.some((call) => {
        const code = call.arguments[0];
        if (!ts.isPropertyAccessExpression(call.expression) || call.expression.name.text !== "close" || !code) {
          return false;
        }
        return (
          (ts.isNumericLiteral(code) && Number(code.text) === GAP_CLOSE_CODE) ||
          (ts.isIdentifier(code) && gapCodes.has(code.text))
        );
      });
      if (!guarded) hits.push(`${at(file, sockets[0])} WebSocket without sequenceGuard`);
      if (!closesOnGap) hits.push(`${at(file, sockets[0])} WebSocket without a ${GAP_CLOSE_CODE} close on a gap`);
      const strings = parse(file).tokens.filter((token) => token.kind === "string").map((token) => token.text);
      const venueBook = strings.some((text) => text.includes("perp_orderbook"));
      if (venueBook && !strings.some((text) => text.includes("/orderbook/"))) {
        hits.push(`${at(file, sockets[0])} venue book without the REST /orderbook/ resync`);
      }
    }
    assert.deepEqual(hits, []);
  });

  it("graphqlOperationsAreGatewayOperations", () => {
    const hits: string[] = [];
    const queries = new Set<string>();
    for (const file of sources(SCAN_ROOTS)) {
      for (const token of parse(file).tokens) {
        if (token.kind !== "string") continue;
        const operation = GRAPHQL_OPERATION.exec(token.text);
        if (!operation) continue;
        const [, kind, name, root] = operation;
        if (kind === "mutation") {
          hits.push(`${file}:${token.line} mutation ${name ?? root}`);
        } else if (kind === "query") {
          if (name === undefined || !GATEWAY_QUERY_OPERATIONS.has(name)) {
            hits.push(`${file}:${token.line} query ${name ?? root}`);
          } else {
            queries.add(name);
          }
        } else if (!GATEWAY_SUBSCRIPTION_FIELDS.has(root)) {
          hits.push(`${file}:${token.line} subscription ${root}`);
        }
      }
    }
    assert.deepEqual(hits, []);
    assert.ok(queries.has("GetFundingPayments"));
  });

  it("noHostLiterals", () => {
    const hits: string[] = [];
    for (const file of sources(SCAN_ROOTS)) {
      if (ENDPOINT_MODULES.has(file)) continue;
      for (const token of parse(file).tokens) {
        if (token.kind !== "string") continue;
        const literal = SCHEME_LITERAL.exec(token.text);
        if (!literal) continue;
        const host = literal[2].toLowerCase();
        if (OUTBOUND_PAGE_HOSTS.has(host)) continue;
        if (file === EMBEDDED_WALLET_CLIENT && EMBEDDED_WALLET_HOSTS.has(host)) continue;
        hits.push(`${file}:${token.line} ${literal[0]}`);
      }
    }
    assert.deepEqual(hits, []);
  });

  it("noFacetAbiImports", () => {
    const hits: string[] = [];
    for (const file of walk(ABI_DIR)) {
      if (!ABI_DIR_ENTRIES.test(path.basename(file))) hits.push(`${file} facet file`);
    }
    for (const file of sources(["src"])) {
      for (const token of parse(file).tokens) {
        if (token.kind === "identifier" && FACET_ABI_NAMES.has(token.text)) {
          hits.push(`${file}:${token.line} ${token.text}`);
        }
      }
    }
    for (const barrel of [`${ABI_DIR}/index.ts`, `${ABI_DIR}/index.js`]) {
      if (!existsSync(path.join(ROOT, barrel))) continue;
      for (const call of nodes(barrel, ts.isCallExpression)) {
        const target = call.arguments[0];
        const required = ts.isIdentifier(call.expression) && call.expression.text === "require";
        if (required && target && ts.isStringLiteral(target) && !BARREL_SOURCES.has(target.text)) {
          hits.push(`${at(barrel, call)} ${target.text}`);
        }
      }
      for (const statement of parse(barrel).source.statements) {
        if (!ts.isExportDeclaration(statement) && !ts.isImportDeclaration(statement)) continue;
        const target = statement.moduleSpecifier;
        if (target && ts.isStringLiteral(target) && !BARREL_SOURCES.has(target.text)) {
          hits.push(`${at(barrel, statement)} ${target.text}`);
        }
      }
    }
    assert.deepEqual(hits, []);
  });

  it("marginModeIsCross", async () => {
    const hits: string[] = [];
    for (const file of sources(["src"])) {
      if (/^(MarginModeToggle|TransferMarginModal)\./.test(path.basename(file))) hits.push(`${file} file`);
      if (file === PERPS_ABI) continue;
      read(file)
        .split("\n")
        .forEach((line, index) => {
          for (const match of line.matchAll(MARGIN_MODE_NAMES)) hits.push(`${file}:${index + 1} ${match[1]}`);
        });
      for (const token of parse(file).tokens) {
        if (token.kind === "identifier" && (token.text === "useMarginMode" || token.text === "useSetMarginMode")) {
          hits.push(`${file}:${token.line} ${token.text}`);
        }
      }
    }
    assert.deepEqual(hits, []);

    const { config } = await loadPerps();
    assert.deepEqual({ ...config.MARGIN_MODE_LABELS }, { 0: "Cross" });
  });

  it("noTransferMarginCaller", () => {
    const hits: string[] = [];
    for (const file of sources(SCAN_ROOTS)) {
      if (/transfermargin/i.test(path.basename(file))) hits.push(`${file} file`);
      if (file === PERPS_ABI) continue;
      for (const token of parse(file).tokens) {
        if (token.kind !== "regex" && /transfermargin/i.test(token.text)) {
          hits.push(`${file}:${token.line} ${token.text}`);
        }
      }
    }
    assert.deepEqual(hits, []);
  });

  it("noFundingThroughFloat", () => {
    const field = /\b(funding_8h|perp_funding_rate|last_funding_rate|last_funding_rate_hourly|funding_rate|funding8h|fundingHourly|venueHourly|estFunding|fundingRate24h)\b/;
    const hits: string[] = [];
    for (const file of walk("src").filter((f) => /\.(ts|tsx)$/.test(f))) {
      read(file).split("\n").forEach((line, index) => {
        if (field.test(line) && /\bNumber\(|parseFloat|\bnum\(/.test(line)) hits.push(`${file}:${index + 1} ${line.trim()}`);
      });
    }
    assert.deepEqual(hits, []);
    const chart = read("src/widgets/analytics/FundingHistoryChart.tsx");
    const boundary = /\nfunction plotFundingRate\(rate: bigint\): number \{\n[^}]*\n\}\n/.exec(chart);
    assert.ok(boundary, "FundingHistoryChart converts to a plot number in plotFundingRate");
    assert.doesNotMatch(chart.replace(boundary[0], "\n"), /\bNumber\(|parseFloat|\bnum\(|\/ ?1e18/);
    assert.match(chart, /const rate = parseFundingRateWad\(s\.fundingRate24h\);/);
    assert.match(chart, /formatSignedFundingDecimalPct\(fundingWadDecimal\(item\.payload\?\.rate \?\? null\)\)/);
    assert.doesNotMatch(chart, /formatFundingRatePct|formatFundingDecimalPct\(|\.percent\(/);
    const table = read("src/widgets/analytics/MarketSnapshotTable.tsx");
    for (const line of table.split("\n").filter((l) => /fundingRate|fundingSign/.test(l))) {
      assert.doesNotMatch(line, /\bNumber\(|parseFloat|\bnum\(|parse18|\/ ?1e18|\.percent\(|formatFundingRatePct|formatFundingDecimalPct\(/, `MarketSnapshotTable.tsx: ${line.trim()}`);
    }
    assert.match(table, /fundingWadDecimal\(parseFundingRateWad\(s\.fundingRate24h\)\)/);
    assert.match(table, /formatSignedFundingDecimalPct\(fundingRate\)/);
  });

  it("noEstFundingRate", () => {
    const hits = walk("src").filter((file) => /est_funding_rate/.test(read(file)));
    assert.deepEqual(hits, []);
    assert.ok(walk("src").some((file) => file.endsWith(".graphql")));
  });

  it("noLegacyTokenEntries", () => {
    const hits: string[] = [];
    for (const file of sources(SCAN_ROOTS)) {
      for (const token of parse(file).tokens) {
        if (token.kind === "identifier") {
          if (SPOT_ERROR_NAMES.has(token.text)) continue;
          if (identifierSegments(token.text).some((segment) => LEGACY_TOKENS.has(segment))) {
            hits.push(`${file}:${token.line} ${token.text}`);
          }
          continue;
        }
        if (/\s/.test(token.text)) continue;
        if (token.text === "WETH" && isWethView(file, token.node)) continue;
        const legacy = token.text
          .replace(CHAINFLOW_SYMBOL_SUFFIX, "")
          .split(/[^A-Za-z0-9]+/)
          .filter((segment) => LEGACY_TOKENS.has(segment));
        if (legacy.length > 0) hits.push(`${file}:${token.line} ${token.text}`);
      }
    }
    assert.deepEqual(hits, []);
  });
});
