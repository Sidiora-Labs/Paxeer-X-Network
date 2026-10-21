import { describe, it } from "node:test";
import { strict as assert } from "node:assert";
import { readFileSync } from "node:fs";

import { toCollateralUnits } from "../src/core/perps/collateralInputs";

const read = (file: string) => readFileSync(new URL("../" + file, import.meta.url), "utf8");

describe("collateral modals", () => {
  it("modalsUseCollateralUnits", () => {
    const adjust = read("src/widgets/trading/modals/AdjustCollateralModal.tsx");
    const topUp = read("src/widgets/trading/modals/TopUpPositionModal.tsx");

    assert.deepEqual(adjust.match(/parseEther\(.*?\)\)/g), ["parseEther(lev.toString())"]);
    assert.equal(adjust.match(/parseEther\(/g)?.length, 1);
    assert.equal(topUp.includes("parseEther"), false);

    assert.match(adjust, /const collateralUnits = toCollateralUnits\(amount\);/);
    assert.match(adjust, /positionHook\.addCollateral\(posId, collateralUnits\)/);
    assert.match(adjust, /positionHook\.removeCollateral\(posId, collateralUnits\)/);
    assert.match(adjust, /positionHook\.addSize\(posId, collateralUnits, parseEther\(lev\.toString\(\)\)\)/);
    assert.match(topUp, /const collateralUnits = toCollateralUnits\(amount\);/);
    assert.match(topUp, /execute\(\{ positionId: BigInt\(positionId\), amount: collateralUnits \}\)/);

    for (const source of [adjust, topUp]) {
      assert.match(source, /import \{ toCollateralUnits \} from "@\/core\/perps\/collateralInputs";/);
    }
  });

  it("topUpAmountIsSixDecimals", () => {
    assert.equal(toCollateralUnits("15"), 15_000_000n);
    assert.equal(toCollateralUnits("0.29"), 290_000n);
    assert.equal(toCollateralUnits("1.1234567"), 1_123_456n);
    assert.equal(toCollateralUnits(""), 0n);
    assert.equal(toCollateralUnits("-5"), 0n);
    assert.equal(toCollateralUnits("1e3"), 0n);
  });
});
