import { describe, it } from "node:test";
import { strict as assert } from "node:assert";

import {
  type AbiFunction,
  type Hex,
  decodeFunctionResult,
  encodeFunctionResult,
  toEventSignature,
  toFunctionSelector,
  toFunctionSignature,
} from "viem";

import { PerpsPrecompileABI, OrderType, TimeInForce } from "../src/core/network/abi/PerpsPrecompile";

const ORDER_PARAMS =
  "(uint256,bool,uint8,uint8,uint256,uint256,uint256,uint256,uint256,uint256,bool,uint256,uint256,uint64,bytes16)";

type CallSite = {
  method: string;
  hooks: readonly string[];
  removed: boolean;
  calls: readonly string[];
};

// The 32 distinct 0x1015 methods the six hook files call today, with their
// v6.12.0 replacement or removal (contracts/application.md#perpetual-call-sites
// and #account-and-collateral).
const CALL_SITES: readonly CallSite[] = [
  { method: "openPosition", hooks: ["usePosition.ts"], removed: false, calls: [`placeOrder(${ORDER_PARAMS})`] },
  { method: "addSize", hooks: ["usePosition.ts"], removed: false, calls: [`placeOrder(${ORDER_PARAMS})`] },
  { method: "closePosition", hooks: ["usePosition.ts"], removed: false, calls: ["closePositionWithParams(uint256,uint256,uint256,uint64)"] },
  { method: "partialClose", hooks: ["usePosition.ts"], removed: false, calls: ["quotePartialClose(uint256,uint256)", "closePositionWithParams(uint256,uint256,uint256,uint64)"] },
  { method: "addCollateral", hooks: ["usePosition.ts"], removed: false, calls: ["addCollateral(uint256,uint256)"] },
  { method: "removeCollateral", hooks: ["usePosition.ts"], removed: false, calls: ["removeCollateral(uint256,uint256)"] },
  { method: "getPosition", hooks: ["usePosition.ts"], removed: false, calls: ["getPosition(uint256)", "getPositionDetails(uint256)"] },
  { method: "getUserPositionIds", hooks: ["usePosition.ts"], removed: false, calls: ["getUserPositionIds(address)"] },
  { method: "getOpenInterest", hooks: ["usePosition.ts"], removed: false, calls: ["getOpenInterest(uint256)"] },
  { method: "placeLimitOrder", hooks: ["usePerpsOrders.ts"], removed: false, calls: [`placeOrder(${ORDER_PARAMS})`] },
  { method: "placeStopLimitOrder", hooks: ["usePerpsOrders.ts"], removed: false, calls: [`placeOrder(${ORDER_PARAMS})`] },
  { method: "placeTakeProfitOrder", hooks: ["usePerpsOrders.ts"], removed: false, calls: ["setPositionTpSl(uint256,uint256,uint256)"] },
  { method: "placeStopLossOrder", hooks: ["usePerpsOrders.ts"], removed: false, calls: ["setPositionTpSl(uint256,uint256,uint256)"] },
  { method: "cancelOrder", hooks: ["usePerpsOrders.ts"], removed: false, calls: ["cancelOrder(uint256)", "cancelByCloid(uint256,bytes16)"] },
  { method: "getOrder", hooks: ["usePerpsOrders.ts"], removed: false, calls: ["getOrder(uint256)"] },
  { method: "getUserOrderIds", hooks: ["usePerpsOrders.ts"], removed: false, calls: ["getUserOrderIds(address)"] },
  { method: "getMarginMode", hooks: ["useMarginMode.ts"], removed: true, calls: [] },
  { method: "setMarginMode", hooks: ["useMarginMode.ts"], removed: true, calls: [] },
  { method: "getPositionMargin", hooks: ["useMarginMode.ts"], removed: false, calls: ["getPositionMargin(uint256)"] },
  { method: "topUpPosition", hooks: ["useMarginMode.ts"], removed: false, calls: ["topUpPosition(uint256,uint256)"] },
  { method: "transferMargin", hooks: ["useMarginMode.ts"], removed: true, calls: [] },
  { method: "getAccountSummary", hooks: ["useMarginMode.ts"], removed: false, calls: ["getAccountSummary(address,address)"] },
  { method: "emergencyWithdraw", hooks: ["useMarginMode.ts"], removed: true, calls: ["getAccount(address)", "withdraw(address,uint256)"] },
  { method: "getVault", hooks: ["useVault.ts"], removed: true, calls: [] },
  { method: "createVault", hooks: ["useVault.ts"], removed: true, calls: [] },
  { method: "getBalance", hooks: ["useVault.ts", "usePortfolioStats.ts"], removed: false, calls: ["getAccount(address)", "getBalance(address,address)"] },
  { method: "getLockedBalance", hooks: ["useVault.ts", "usePortfolioStats.ts"], removed: false, calls: ["getAccount(address)", "getLockedBalance(address,address)"] },
  { method: "deposit", hooks: ["useVault.ts"], removed: false, calls: ["deposit(address,uint256)"] },
  { method: "withdraw", hooks: ["useVault.ts"], removed: false, calls: ["withdraw(address,uint256)"] },
  { method: "quoteMarket", hooks: ["useQuoter.ts"], removed: false, calls: ["quoteMarket(uint256)"] },
  { method: "quoteOpenPosition", hooks: ["useQuoter.ts"], removed: false, calls: ["quoteOpenPosition(uint256,address,uint256,uint256,bool)"] },
  { method: "quoteClosePosition", hooks: ["useQuoter.ts"], removed: false, calls: ["quoteClosePosition(uint256)"] },
];

// contracts/perps.md#19-errors: name and frozen selector, codes 2 to 45.
const ERRORS: readonly (readonly [string, Hex])[] = [
  ["PerpsInvalidParams", "0xc7088a37"],
  ["PerpsUnknownMarket", "0x65644af8"],
  ["PerpsMarketDisabled", "0x386d4503"],
  ["PerpsPriceStale", "0x737dedba"],
  ["PerpsZeroAmount", "0x811bd7a1"],
  ["PerpsUnsupportedCollateral", "0x0a8b8f76"],
  ["PerpsInsufficientCollateral", "0x61997bc4"],
  ["PerpsInsufficientMargin", "0x12b52d40"],
  ["PerpsLeverageTooHigh", "0x794b54cf"],
  ["PerpsTickSize", "0x73f775c1"],
  ["PerpsLotSize", "0x8e28a39a"],
  ["PerpsMinOrderValue", "0xd1d78b4f"],
  ["PerpsMaxOrderValue", "0xe9933ae1"],
  ["PerpsOpenInterestCap", "0x021901d6"],
  ["PerpsInvalidTimeInForce", "0x47306489"],
  ["PerpsInvalidOrderType", "0x7be0d47b"],
  ["PerpsPostOnlyWouldCross", "0x096e07cd"],
  ["PerpsFokNotFilled", "0xc3dbe522"],
  ["PerpsNoLiquidity", "0xef4eb50b"],
  ["PerpsSlippageExceeded", "0xb6c82773"],
  ["PerpsDeadlineExpired", "0xd28988d8"],
  ["PerpsReduceOnlyViolation", "0x8adbf151"],
  ["PerpsDuplicateCloid", "0x6298d438"],
  ["PerpsUnknownOrder", "0x1930eff6"],
  ["PerpsNotOrderHolder", "0xedef32ca"],
  ["PerpsUnknownPosition", "0x6dcd3d95"],
  ["PerpsNotPositionHolder", "0xe59be79c"],
  ["PerpsInvalidTriggerPrice", "0xd7580015"],
  ["PerpsTriggerNotReached", "0xc6298dab"],
  ["PerpsTooManyOpenOrders", "0xfc1f0263"],
  ["PerpsCrossMarginOnly", "0x6ff920cc"],
  ["PerpsNotLiquidatable", "0xce578943"],
  ["PerpsLiquidationCooldown", "0x6098ef55"],
  ["PerpsUnauthorized", "0xf73dc939"],
  ["PerpsStaticCall", "0x32a35088"],
  ["PerpsDelegatecall", "0x856fe7e1"],
  ["PerpsOverflow", "0x927150ed"],
  ["PerpsInvalidGenesis", "0xd06690f2"],
  ["PerpsCloseSizeExceedsPosition", "0x1be787db"],
  ["PerpsSelfTransfer", "0x98bc8e6a"],
  ["PerpsZeroAddress", "0xd6d30277"],
  ["PerpsNoAccount", "0xd7b16785"],
  ["PerpsTokenTransferFailed", "0x6fdbf101"],
  ["PerpsPaused", "0xe0753633"],
];

// contracts/perps.md section 18.
const EVENTS: readonly string[] = [
  "VaultCreated(address,address)",
  "CollateralDeposited(address,uint256,uint256)",
  "CollateralWithdrawn(address,uint256,uint256)",
  "CollateralTransferred(address,address,uint256)",
  "OrderPlaced(uint256,address,uint256,uint8,bool,uint256,uint256)",
  "OrderOptions(uint256,uint8,bool,uint256,uint256,uint64,bytes16)",
  "OrderCancelled(uint256,address)",
  "OrderExpired(uint256,address,uint256)",
  "OrderTriggered(uint256,uint256,uint256)",
  "OrderExecuted(uint256,uint256,uint256)",
  "Fill(uint256,uint256,uint256,address,address,bool,uint256,uint256,int256,uint256)",
  "PositionOpened(uint256,address,uint256,bool,uint256,uint256,uint256,address,uint256)",
  "PositionModified(uint256,uint256,uint256,uint256)",
  "PositionEntryPriceChanged(uint256,uint256,uint256)",
  "PositionClosed(uint256,address,uint256,uint256,uint256,int256,bool)",
  "PositionTpSlSet(uint256,uint256,uint256)",
  "TradeSettled(uint256,address,uint256,uint8,uint256,uint256,int256,uint256,int256,uint256)",
  "FeeCollected(uint256,address,uint256,uint8,uint256,uint256,address)",
  "MarkPriceUpdated(uint256,uint256,uint256,uint256)",
  "FundingRateUpdated(uint256,int256,int256)",
  "FundingSettled(uint256,int256,int256,int256)",
  "PositionFundingApplied(uint256,address,uint256,int256,uint256,uint256)",
  "OpenInterestChanged(uint256,uint256,uint256,uint256,bool)",
  "Liquidation(uint256,address,uint256,uint256,uint256,address)",
  "BackstopLiquidation(address,uint256,uint256,uint256,uint256)",
  "InsuranceContribution(address,uint256,uint8)",
  "InsuranceDraw(uint256,uint256,uint256)",
  "VaultDeficit(address,uint256)",
  "ADLExecuted(uint256,uint256)",
  "ADLMatched(uint256,uint256,address,uint256,uint256,uint256)",
];

const functions: readonly AbiFunction[] = PerpsPrecompileABI.filter(
  (item): item is Extract<(typeof PerpsPrecompileABI)[number], { type: "function" }> =>
    item.type === "function",
);
const signatures = new Set(functions.map((f) => toFunctionSignature(f)));

function fn(signature: string): AbiFunction {
  const found = functions.find((f) => toFunctionSignature(f) === signature);
  assert.ok(found, `PerpsPrecompileABI lacks ${signature}`);
  return found;
}

function fields(params: AbiFunction["inputs"][number] | undefined): [string, string][] {
  assert.ok(params && "components" in params, "expected a tuple parameter");
  return params.components.map((c) => [c.name ?? "", c.type]);
}

function decodeNamed(signature: string, data: Hex): Record<string, unknown> {
  const item = fn(signature);
  const decoded = decodeFunctionResult({ abi: [item], functionName: item.name, data });
  const values = item.outputs.length === 1 ? [decoded] : (decoded as readonly unknown[]);
  assert.equal(values.length, item.outputs.length);
  return Object.fromEntries(item.outputs.map((o, i) => [o.name ?? "", values[i]]));
}

describe("PerpsPrecompileABI", () => {
  it("abiCoversEveryCallSite", () => {
    assert.equal(CALL_SITES.length, 32);
    assert.equal(new Set(CALL_SITES.map((c) => c.method)).size, 32);
    assert.deepEqual(
      [...new Set(CALL_SITES.flatMap((c) => c.hooks))].sort(),
      ["useMarginMode.ts", "usePerpsOrders.ts", "usePortfolioStats.ts", "usePosition.ts", "useQuoter.ts", "useVault.ts"],
    );

    const names = new Set(functions.map((f) => f.name));
    for (const site of CALL_SITES) {
      assert.ok(names.has(site.method), `${site.method} keeps its selector at 0x1015`);
      assert.ok(site.removed || site.calls.length > 0, `${site.method} maps to a replacement or a removal`);
      for (const call of site.calls) assert.ok(signatures.has(call), `${site.method} -> ${call}`);
    }
    assert.deepEqual(
      CALL_SITES.filter((c) => c.removed).map((c) => c.method).sort(),
      ["createVault", "emergencyWithdraw", "getMarginMode", "getVault", "setMarginMode", "transferMargin"],
    );

    for (const sig of [
      `placeOrder(${ORDER_PARAMS})`,
      "closePositionWithParams(uint256,uint256,uint256,uint64)",
      "setPositionTpSl(uint256,uint256,uint256)",
      "cancelByCloid(uint256,bytes16)",
      "getAccount(address)",
      "getPositionDetails(uint256)",
      "quotePartialClose(uint256,uint256)",
      "getBalance(address)",
      "getBalance(address,address)",
      "getLockedBalance(address)",
      "getLockedBalance(address,address)",
      "getAccountSummary(address)",
      "getAccountSummary(address,address)",
      "getMarginMode()",
      "getMarginMode(address)",
    ]) {
      assert.ok(signatures.has(sig), sig);
    }
    assert.equal(functions.length, 67);
    assert.equal(names.size, 63);

    const errors = PerpsPrecompileABI.filter((item) => item.type === "error");
    assert.equal(errors.length, ERRORS.length);
    assert.deepEqual(
      errors.map((e) => [e.name, toFunctionSelector(`${e.name}()`)]).sort(),
      ERRORS.map(([name, selector]) => [name, selector]).sort(),
    );
    for (const e of errors) assert.equal(e.inputs.length, 0, `${e.name} is parameterless`);

    const events = PerpsPrecompileABI.filter((item) => item.type === "event");
    assert.deepEqual(events.map((e) => toEventSignature(e)).sort(), [...EVENTS].sort());

    const orderParams = fn(`placeOrder(${ORDER_PARAMS})`).inputs[0];
    assert.equal(orderParams?.internalType, "struct IPerps.OrderParams");
    assert.deepEqual(fields(orderParams), [
      ["marketId", "uint256"],
      ["isLong", "bool"],
      ["orderType", "uint8"],
      ["timeInForce", "uint8"],
      ["sizeUsd", "uint256"],
      ["leverage", "uint256"],
      ["limitPrice", "uint256"],
      ["triggerPrice", "uint256"],
      ["acceptablePrice", "uint256"],
      ["collateralAmount", "uint256"],
      ["reduceOnly", "bool"],
      ["takeProfitPrice", "uint256"],
      ["stopLossPrice", "uint256"],
      ["deadline", "uint64"],
      ["cloid", "bytes16"],
    ]);

    const positionDetails = fn("getPositionDetails(uint256)").outputs[0];
    assert.equal(positionDetails?.internalType, "struct IPerps.PositionDetails");
    assert.deepEqual(fields(positionDetails), [
      ["positionId", "uint256"],
      ["user", "address"],
      ["marketId", "uint256"],
      ["isLong", "bool"],
      ["sizeBase", "uint256"],
      ["sizeUsdAtMark", "uint256"],
      ["entryPrice", "uint256"],
      ["leverage", "uint256"],
      ["unrealizedPnlUsd", "int256"],
      ["fundingOwedUsd", "int256"],
      ["liquidationPrice", "uint256"],
      ["takeProfitPrice", "uint256"],
      ["stopLossPrice", "uint256"],
      ["openedAt", "uint256"],
    ]);

    const accountState = fn("getAccount(address)").outputs[0];
    assert.equal(accountState?.internalType, "struct IPerps.AccountState");
    assert.deepEqual(fields(accountState), [
      ["collateral", "uint256"],
      ["equityUsd", "int256"],
      ["initialMarginUsd", "uint256"],
      ["maintenanceMarginUsd", "uint256"],
      ["withdrawable", "uint256"],
      ["feeTier", "uint8"],
      ["volume14dUsd", "uint256"],
      ["positionCount", "uint256"],
      ["openOrderCount", "uint256"],
    ]);

    assert.deepEqual(
      { ...OrderType },
      { LIMIT: 0, STOP_LIMIT: 1, TAKE_PROFIT: 2, STOP_LOSS: 3, MARKET: 4, STOP_MARKET: 5 },
    );
    assert.deepEqual({ ...TimeInForce }, { GTC: 0, IOC: 1, FOK: 2, POST_ONLY: 3 });
  });

  it("reshapedViewsDecodeByName", () => {
    const fees = decodeNamed(
      "getFees()",
      encodeFunctionResult({ abi: PerpsPrecompileABI, functionName: "getFees", result: [700n, 150n, 0n, 0n] }),
    );
    assert.deepEqual(fees, { takerFeePpm: 700n, makerFeePpm: 150n, liquidationFeePpm: 0n, insuranceSharePpm: 0n });
    assert.equal(Number(fees.takerFeePpm) / 1e4, 0.07);

    const mark = decodeNamed(
      "getMarkPrice(uint256)",
      encodeFunctionResult({ abi: PerpsPrecompileABI, functionName: "getMarkPrice", result: 60012n * 10n ** 18n }),
    );
    assert.deepEqual(mark, { markPrice: 60012n * 10n ** 18n });

    const markDetails = decodeNamed(
      "getMarkPriceDetails(uint256)",
      encodeFunctionResult({
        abi: PerpsPrecompileABI,
        functionName: "getMarkPriceDetails",
        result: [60012n * 10n ** 18n, 60000n * 10n ** 18n, 60000n * 10n ** 18n, true],
      }),
    );
    assert.deepEqual(markDetails, {
      markPrice: 60012n * 10n ** 18n,
      indexPrice: 60000n * 10n ** 18n,
      midPrice: 60000n * 10n ** 18n,
      fresh: true,
    });

    const funding = decodeNamed(
      "getFundingState(uint256)",
      encodeFunctionResult({
        abi: PerpsPrecompileABI,
        functionName: "getFundingState",
        result: [1075n * 10n ** 16n, -1075n * 10n ** 16n, 1800000000n, 3472222222n],
      }),
    );
    assert.deepEqual(funding, {
      cumulativeLong: 1075n * 10n ** 16n,
      cumulativeShort: -1075n * 10n ** 16n,
      lastUpdate: 1800000000n,
      ratePerSecond: 3472222222n,
    });

    const fundingDetails = decodeNamed(
      "getFundingDetails(uint256)",
      encodeFunctionResult({
        abi: PerpsPrecompileABI,
        functionName: "getFundingDetails",
        result: [1075n * 10n ** 16n, 1800000000n, 12500000000000n, 0n, 300n],
      }),
    );
    assert.deepEqual(fundingDetails, {
      cumulativeFundingPerUnit: 1075n * 10n ** 16n,
      lastSettleTime: 1800000000n,
      lastHourlyRate: 12500000000000n,
      premiumSampleSum: 0n,
      premiumSampleCount: 300n,
    });

    const liquidation = fn("getLiquidationPrice(uint256)");
    assert.deepEqual(
      liquidation.inputs.map((i) => [i.name, i.type]),
      [["positionId", "uint256"]],
    );
    const liq = decodeNamed(
      "getLiquidationPrice(uint256)",
      encodeFunctionResult({
        abi: PerpsPrecompileABI,
        functionName: "getLiquidationPrice",
        result: 59399699849924962481240n,
      }),
    );
    assert.deepEqual(liq, { liquidationPrice: 59399699849924962481240n });
  });
});
