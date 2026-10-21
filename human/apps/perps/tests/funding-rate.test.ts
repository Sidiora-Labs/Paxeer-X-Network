// Venue funding rate: exact parse of last_funding_rate_hourly, the ×24 projection, labels, and the removed est_funding_rate name.
// Run via `node --test --import tsx tests/funding-rate.test.ts`.

import { describe, it } from "node:test";
import { strict as assert } from "node:assert";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

import {
  FUNDING_LABEL_1H,
  FUNDING_LABEL_24H,
  formatFundingRatePct,
  fundingFigures,
  parseFundingRateHourly,
  projectFunding24h,
} from "../src/core/perps/fundingRate";

const ROOT = fileURLToPath(new URL("..", import.meta.url));

describe("parseFundingRateHourly", () => {
  it("parses decimal strings exactly at 18 decimals", () => {
    assert.equal(parseFundingRateHourly("0.0001"), 100_000_000_000_000n);
    assert.equal(parseFundingRateHourly("-0.0000125"), -12_500_000_000_000n);
    assert.equal(parseFundingRateHourly("0.000000000000000001"), 1n);
    assert.equal(parseFundingRateHourly("1"), 10n ** 18n);
    assert.equal(parseFundingRateHourly("0"), 0n);
    assert.equal(parseFundingRateHourly("123456789.123456789123456789"), 123_456_789_123_456_789_123_456_789n);
  });

  it("refuses anything that is not an exact decimal string", () => {
    for (const bad of [null, undefined, "", "1e-4", "0.0000000000000000001", " 0.1", "+0.1", ".1", "1.", "NaN", "0x10"]) {
      assert.throws(() => parseFundingRateHourly(bad), /not an exact decimal string/);
    }
    assert.throws(() => parseFundingRateHourly(0.0001 as unknown as string));
  });
});

describe("projectFunding24h", () => {
  it("is exactly 24 times the settled hourly rate", () => {
    assert.equal(projectFunding24h(parseFundingRateHourly("0.0001")), 2_400_000_000_000_000n);
    assert.equal(projectFunding24h(parseFundingRateHourly("-0.000000000000000001")), -24n);
    const big = parseFundingRateHourly("98765.432109876543210987");
    assert.equal(projectFunding24h(big), big * 24n);
    assert.equal(projectFunding24h(big), 2_370_370_370_637_037_037_063_688n);
  });

  it("labels the hourly figure as last settled and the daily one as a ×24 projection", () => {
    assert.deepEqual(fundingFigures(parseFundingRateHourly("0.0001")), [
      { label: FUNDING_LABEL_1H, rate: 100_000_000_000_000n, text: "0.0100%" },
      { label: FUNDING_LABEL_24H, rate: 2_400_000_000_000_000n, text: "0.2400%" },
    ]);
    assert.equal(FUNDING_LABEL_1H, "last settled funding (1h)");
    assert.match(FUNDING_LABEL_24H, /×24 projection/);
    for (const label of [FUNDING_LABEL_1H, FUNDING_LABEL_24H]) assert.doesNotMatch(label, /estimate|est\b/i);
    assert.equal(formatFundingRatePct(-12_500_000_000_000n), "-0.0013%");
  });
});

describe("quoter funding source", () => {
  const files = ["src/hooks/perps/useQuoter.ts", "src/core/perps/fundingRate.ts"];

  it("no est_funding_rate reference remains in the quoter or its venue type", () => {
    for (const file of files) assert.doesNotMatch(readFileSync(ROOT + file, "utf8"), /est_funding_rate/, file);
  });

  it("the quoter reads last_funding_rate_hourly through the exact parser, with no Number()", () => {
    const src = readFileSync(ROOT + files[0], "utf8");
    assert.match(src, /parseFundingRateHourly\(t\.last_funding_rate_hourly\)/);
    assert.match(src, /fundingRate24h: projectFunding24h\(/);
    for (const line of src.split("\n").filter((l) => /funding/i.test(l))) assert.doesNotMatch(line, /Number\(|toBigInt18/, line);
  });
});
