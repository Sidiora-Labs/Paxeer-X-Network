import { describe, it } from "node:test";
import { strict as assert } from "node:assert";
import { existsSync, readFileSync } from "node:fs";

import { PRECOMPILES } from "../src/core/network/precompiles";
import {
  PARTIAL_CLOSE_BPS,
  partialCloseAction,
  partialCloseSize,
  positionActions,
  setTpSlAction,
  unitsFromIndexed,
} from "../src/core/perps/positionActions";

const E18 = 10n ** 18n;
const path = (file: string) => new URL("../" + file, import.meta.url);
const read = (file: string) => readFileSync(path(file), "utf8");

function refusedWith(code: string): (err: unknown) => boolean {
  return (err) => {
    assert.equal((err as { code?: unknown }).code, code);
    return true;
  };
}

describe("positions panel", () => {
  it("actionListHasNoTransferMargin", () => {
    assert.deepEqual(
      positionActions.map((a) => [a.kind, a.calls, a.quote]),
      [
        ["close", ["closePositionWithParams"], "quoteClosePosition"],
        ["partialClose", ["closePositionWithParams"], "quotePartialClose"],
        ["adjustCollateral", ["addCollateral", "removeCollateral", "placeOrder"], "quoteOpenPosition"],
        ["topUp", ["topUpPosition"], undefined],
        ["setTpSl", ["setPositionTpSl"], undefined],
        ["cancel", ["cancelOrder"], undefined],
      ],
    );
    for (const action of positionActions) {
      assert.equal(action.calls.includes("transferMargin"), false);
      assert.equal(action.calls.includes("setMarginMode"), false);
    }
    assert.deepEqual(PARTIAL_CLOSE_BPS, [2_500, 5_000, 7_500]);
  });

  it("partialCloseShortBoundRoundsUp", () => {
    const action = partialCloseAction({ positionId: 7n, isLong: false, sizeUsd: 2000n * E18, bps: 5_000 });
    assert.equal(action.closeSizeUsd, 1000n * E18);
    const call = action.call(90_000n * E18);
    assert.equal(call.address, PRECOMPILES.perps);
    assert.equal(call.functionName, "closePositionWithParams");
    assert.equal(call.args[0], 7n);
    assert.equal(call.args[1], 1000n * E18);
    assert.equal(call.args[2], 90_450n * E18);
    assert.ok(call.args[3] > 0n);
  });

  it("partialCloseLongBoundRoundsDown", () => {
    const action = partialCloseAction({ positionId: 3n, isLong: true, sizeUsd: 400n * E18, bps: 2_500 });
    assert.equal(action.closeSizeUsd, 100n * E18);
    assert.equal(action.call(2_000n * E18).args[2], 1_990n * E18);
  });

  it("partialCloseSizeStaysInsideThePosition", () => {
    assert.equal(partialCloseSize(2000n * E18, 7_500), 1500n * E18);
    assert.throws(() => partialCloseSize(2000n * E18, 0), RangeError);
    assert.throws(() => partialCloseSize(2000n * E18, 10_000), RangeError);
    assert.throws(() => partialCloseSize(2000n * E18, 2_500.5), RangeError);
    assert.throws(() => partialCloseSize(1n, 5_000), RangeError);
  });

  it("indexedSizeParsesExactly", () => {
    assert.equal(unitsFromIndexed("2000000000000000000000"), 2000n * E18);
    assert.equal(unitsFromIndexed("2000e+18"), 2000n * E18);
    assert.equal(unitsFromIndexed("1.5e21"), 1500n * E18);
    assert.equal(unitsFromIndexed("12.50e1"), 125n);
    assert.throws(() => unitsFromIndexed("1.5"), RangeError);
    assert.throws(() => unitsFromIndexed("-5e3"), RangeError);
    assert.throws(() => unitsFromIndexed("5e-3"), RangeError);
    assert.throws(() => unitsFromIndexed(""), RangeError);
  });

  it("setTpSlIsOneCallAt18Decimals", () => {
    const call = setTpSlAction(7n, true, "100000", "94000");
    assert.equal(call.address, PRECOMPILES.perps);
    assert.equal(call.functionName, "setPositionTpSl");
    assert.deepEqual(call.args, [7n, 100_000n * E18, 94_000n * E18]);
    assert.deepEqual(setTpSlAction(7n, true, "100000", "").args, [7n, 100_000n * E18, 0n]);
    assert.deepEqual(setTpSlAction(7n, false, " ", "101000.5").args, [7n, 0n, 101_000_500_000_000_000_000_000n]);
    assert.throws(() => setTpSlAction(7n, true, "94000", "100000"), refusedWith("TRADE_TP_SL_INVALID"));
    assert.throws(() => setTpSlAction(7n, false, "100000", "94000"), refusedWith("TRADE_TP_SL_INVALID"));
  });

  it("panelDropsTransferMarginAndMarginMode", () => {
    const panel = read("src/widgets/trading/PerpsPositionsPanel.tsx");
    for (const name of ["TransferMarginModal", "MarginModeToggle", "useMarginMode", "transferMargin"]) {
      assert.equal(panel.includes(name), false, name);
    }
    assert.match(panel, /const handlePartialClose = useCallback\(/);
    assert.match(panel, /quoterRef\.current\.quotePartialClose\(BigInt\(positionId\), action\.closeSizeUsd\)/);
    assert.match(panel, /await ordersRef\.current\.cancelOrder\(BigInt\(orderId\)\);/);
    assert.equal(existsSync(path("src/widgets/trading/modals/TransferMarginModal.tsx")), false);
    assert.equal(existsSync(path("src/widgets/trading/modals/MarginModeToggle.tsx")), false);
  });

  it("setTpSlModalSendsOneCall", () => {
    const modal = read("src/widgets/trading/modals/SetTpSlModal.tsx");
    assert.equal(modal.includes("placeTakeProfitOrder"), false);
    assert.equal(modal.includes("placeStopLossOrder"), false);
    assert.equal(modal.includes("parseEther"), false);
    assert.equal(modal.match(/writeContract\(/g)?.length, 1);
    assert.match(modal, /writeContract\(setTpSlAction\(BigInt\(positionId\), isLong, tpPrice, slPrice\)\)/);
  });
});
