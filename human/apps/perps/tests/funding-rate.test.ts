// Venue funding rate: exact parse of last_funding_rate_hourly, the ×24 projection, labels, and the removed est_funding_rate name.
// Run via `node --test --import tsx tests/funding-rate.test.ts`.

import { describe, it, mock } from "node:test";
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

describe("venue funding consumers", () => {
  const read = (file: string) => readFileSync(ROOT + file, "utf8");

  it("the markets stream parses last_funding_rate_hourly exactly and keeps an absent rate null", async () => {
    const { normalizeMarket, parseVenueFundingHourly } = await import("../src/hooks/perps/useMarketsStream");
    assert.equal(normalizeMarket({ symbol: "BTC", last_funding_rate_hourly: "0.0000125" })?.last_funding_rate_hourly, 12_500_000_000_000n);
    assert.equal(normalizeMarket({ symbol: "ETH", last_funding_rate_hourly: "-0.000000000000000001" })?.last_funding_rate_hourly, -1n);
    assert.equal(normalizeMarket({ symbol: "SOL" })?.last_funding_rate_hourly, null);
    assert.equal(normalizeMarket({ symbol: "SOL", last_funding_rate_hourly: null })?.last_funding_rate_hourly, null);
    assert.equal(parseVenueFundingHourly(undefined), null);
    assert.throws(() => parseVenueFundingHourly("1e-4"), /not an exact decimal string/);
  });

  it("a malformed rate nulls only that market's funding and warns once per symbol and field per minute", async (t) => {
    const { normalizeMarket } = await import("../src/hooks/perps/useMarketsStream");
    const warn = t.mock.method(console, "warn", () => {});
    t.mock.timers.enable({ apis: ["Date"], now: 1_000_000 });
    const bad = normalizeMarket({ symbol: "WARN1", price: 7, last_funding_rate_hourly: 0.0001 })!;
    assert.equal(bad.last_funding_rate_hourly, null);
    assert.equal(bad.price, 7);
    assert.equal(normalizeMarket({ symbol: "WARN1", last_funding_rate_hourly: "1e-4" })?.last_funding_rate_hourly, null);
    assert.equal(warn.mock.callCount(), 1);
    assert.match(String(warn.mock.calls[0].arguments[0]), /WARN1 last_funding_rate_hourly/);
    normalizeMarket({ symbol: "WARN2", last_funding_rate_hourly: "x" });
    assert.equal(warn.mock.callCount(), 2);
    t.mock.timers.tick(59_999);
    normalizeMarket({ symbol: "WARN1", last_funding_rate_hourly: "x" });
    assert.equal(warn.mock.callCount(), 2);
    t.mock.timers.tick(1);
    normalizeMarket({ symbol: "WARN1", last_funding_rate_hourly: "x" });
    assert.equal(warn.mock.callCount(), 3);
  });

  it("one bad market in a markets@all frame or the /tickers map does not drop its siblings", async (t) => {
    t.mock.method(console, "warn", () => {});
    const { normalizeMarketsFrame } = await import("../src/hooks/perps/useMarketsStream");
    const { tickersFromWire } = await import("../src/hooks/perps/useRealtimePrices");
    const frame = normalizeMarketsFrame([
      { symbol: "BTC", price: 100, last_funding_rate_hourly: "0.0001" },
      { symbol: "ETH", price: 10, last_funding_rate_hourly: 0.0001 },
      { symbol: "SOL", price: 1, last_funding_rate_hourly: "-0.0000125" },
    ]);
    assert.deepEqual(frame.map((m) => [m.symbol, m.price, m.last_funding_rate_hourly]), [
      ["BTC", 100, 100_000_000_000_000n], ["ETH", 10, null], ["SOL", 1, -12_500_000_000_000n],
    ]);
    const row = (paxeerSymbol: string, last_funding_rate_hourly: string | null) => ({
      symbol: `PERP_${paxeerSymbol}_USDC`, paxeerSymbol, mark_price: 1, index_price: 1, open_interest: 0, last_funding_rate_hourly,
      "24h_open": 0, "24h_close": 0, "24h_high": 0, "24h_low": 0, "24h_amount": 0, "24h_volume": 0,
    });
    const map = tickersFromWire([row("BTC", "0.0001"), row("ETH", "1e-4"), row("SOL", null)]);
    assert.deepEqual(Object.keys(map), ["BTC", "ETH", "SOL"]);
    assert.equal(map.BTC.last_funding_rate_hourly, 100_000_000_000_000n);
    assert.equal(map.ETH.last_funding_rate_hourly, null);
    assert.equal(map.ETH.mark_price, 1);
    assert.equal(map.SOL.last_funding_rate_hourly, null);
  });

  it("the realtime tickers carry the parsed hourly rate from the stream and from the /tickers wire row", async () => {
    const { normalizeMarket } = await import("../src/hooks/perps/useMarketsStream");
    const { tickerFromSnapshot, tickerFromWire } = await import("../src/hooks/perps/useRealtimePrices");
    const snap = normalizeMarket({ symbol: "BTC", price: 100, last_funding_rate_hourly: "0.0001" })!;
    assert.equal(tickerFromSnapshot(snap).last_funding_rate_hourly, 100_000_000_000_000n);
    const wire = {
      symbol: "PERP_BTC_USDC", paxeerSymbol: "BTC", mark_price: 1, index_price: 1, open_interest: 0,
      last_funding_rate_hourly: "-0.0000125",
      "24h_open": 0, "24h_close": 0, "24h_high": 0, "24h_low": 0, "24h_amount": 0, "24h_volume": 0,
    };
    assert.equal(tickerFromWire(wire).last_funding_rate_hourly, -12_500_000_000_000n);
    assert.equal(tickerFromWire({ ...wire, last_funding_rate_hourly: null }).last_funding_rate_hourly, null);
    const warn = mock.method(console, "warn", () => {});
    try {
      assert.equal(tickerFromWire({ ...wire, last_funding_rate_hourly: "0.1 " }).last_funding_rate_hourly, null);
    } finally { warn.mock.restore(); }
    assert.equal("last_funding_rate" in tickerFromSnapshot(snap), false);
  });

  it("no funding display reads an 8h value as the hourly one or keeps a second Last Funding chip", () => {
    const header = read("src/widgets/trading/PairHeader.tsx");
    assert.doesNotMatch(header, /Last Funding|last_funding_rate\b|funding_8h/);
    assert.doesNotMatch(read("src/hooks/perps/useRealtimePrices.ts"), /funding_8h|last_funding_rate:/);
    assert.match(header, /marketFundingHourly\(market\.symbol, "last_funding_rate_hourly"/);
    assert.doesNotMatch(header, /parseVenueFundingHourly/);
  });

  it("the subgraph documents request last_funding_rate_hourly and no timestamp of the removed field", () => {
    const src = read("src/hooks/perps/useSubgraph.ts");
    assert.equal(src.match(/\blast_funding_rate_hourly\b/g)?.length, 4);
    const schema = read("src/core/perps/perpetual.graphql");
    assert.equal(schema.match(/^\s*last_funding_rate_hourly: String$/gm)?.length, 2);
    const types = read("src/types/perps.ts");
    assert.equal(types.match(/^\s*last_funding_rate_hourly: string \| null;$/gm)?.length, 2);
  });

  it("the symbol socket stats keep no estimated-rate alias", () => {
    assert.doesNotMatch(read("src/hooks/perps/useSidPaxMarketStream.ts"), /est_funding|estFunding/);
  });

  it("the trading header, mobile row and funding tab render the venue rate under the fundingRate.ts labels, with no Number()", () => {
    const header = read("src/widgets/trading/PairHeader.tsx");
    assert.match(header, /fundingFigures\(venueHourly\)/);
    assert.match(header, /\[FUNDING_LABEL_1H, FUNDING_LABEL_24H\]/);
    assert.match(read("src/widgets/trading/MobileTradingLayout.tsx"), /label: FUNDING_LABEL_1H, text: venueHourly === null \? "\\u2014" : formatFundingRatePct\(venueHourly\)/);
    const tab = read("src/page-modules/markets/index.tsx");
    assert.match(tab, /\{FUNDING_LABEL_1H\}/);
    assert.match(tab, /formatFundingRatePct\(row\.fundingHourly\)/);
    for (const file of ["src/widgets/trading/PairHeader.tsx", "src/widgets/trading/MobileTradingLayout.tsx", "src/widgets/markets/useMarketsRows.ts", "src/hooks/perps/useMarketsStream.ts", "src/hooks/perps/useRealtimePrices.ts"]) {
      for (const line of read(file).split("\n").filter((l) => /funding_rate_hourly|venueHourly|fundingHourly/.test(l))) {
        assert.doesNotMatch(line, /Number\(|parseFloat|toBigInt18/, `${file}: ${line}`);
      }
    }
  });

  it("the two 24h views label the subgraph figure as on-chain funding paid, never as the venue rate", () => {
    const table = read("src/widgets/analytics/MarketSnapshotTable.tsx");
    const chart = read("src/widgets/analytics/FundingHistoryChart.tsx");
    assert.match(table, /export const FUNDING_LABEL_ONCHAIN_24H = "funding paid \(24h, on-chain\)";/);
    assert.match(table, /<th [^>]*>\{FUNDING_LABEL_ONCHAIN_24H\}<\/th>/);
    assert.equal(chart.match(/FUNDING_LABEL_ONCHAIN_24H/g)?.length, 3);
    for (const src of [table, chart]) {
      assert.doesNotMatch(src, /Funding( Rate)? \(24h\)/);
      assert.doesNotMatch(src, /FUNDING_LABEL_1H|FUNDING_LABEL_24H|last_funding_rate_hourly/);
    }
  });
});

describe("exact funding_8h, perp_funding_rate and symbol socket rates", () => {
  it("the markets stream parses funding_8h and perp_funding_rate strings exactly and keeps null as null", async () => {
    const { normalizeMarket } = await import("../src/hooks/perps/useMarketsStream");
    const m = normalizeMarket({ symbol: "BTC", funding_8h: "0.0001", perp_funding_rate: "0.0000125", last_funding_rate_hourly: "0.0000125" })!;
    assert.equal(m.funding_8h, 100_000_000_000_000n);
    assert.equal(m.perp_funding_rate, 12_500_000_000_000n);
    assert.equal(m.funding_8h, m.perp_funding_rate! * 8n);
    assert.equal(normalizeMarket({ symbol: "ETH", funding_8h: "-0.000000000000000008" })?.funding_8h, -8n);
    const none = normalizeMarket({ symbol: "SOL", funding_8h: null })!;
    assert.equal(none.funding_8h, null);
    assert.equal(none.perp_funding_rate, null);
  });

  it("a malformed funding_8h nulls only that market and warns once per symbol and field per minute", async (t) => {
    const { normalizeMarketsFrame } = await import("../src/hooks/perps/useMarketsStream");
    const warn = t.mock.method(console, "warn", () => {});
    t.mock.timers.enable({ apis: ["Date"], now: 5_000_000 });
    const frame = normalizeMarketsFrame([
      { symbol: "F8A", price: 100, funding_8h: "0.0008", perp_funding_rate: "0.0001" },
      { symbol: "F8B", price: 10, funding_8h: 0.0008, perp_funding_rate: "1e-4" },
      { symbol: "F8C", price: 1, funding_8h: "-0.0008" },
    ]);
    assert.deepEqual(frame.map((m) => [m.symbol, m.price, m.funding_8h, m.perp_funding_rate]), [
      ["F8A", 100, 800_000_000_000_000n, 100_000_000_000_000n], ["F8B", 10, null, null], ["F8C", 1, -800_000_000_000_000n, null],
    ]);
    assert.equal(warn.mock.callCount(), 2);
    assert.match(String(warn.mock.calls[0].arguments[0]), /F8B funding_8h/);
    assert.match(String(warn.mock.calls[1].arguments[0]), /F8B perp_funding_rate/);
    normalizeMarketsFrame([{ symbol: "F8B", funding_8h: "x" }]);
    assert.equal(warn.mock.callCount(), 2);
    t.mock.timers.tick(60_000);
    normalizeMarketsFrame([{ symbol: "F8B", funding_8h: "x" }]);
    assert.equal(warn.mock.callCount(), 3);
  });

  it("a JSON number rate becomes its exact plain decimal text and absent is null, never zero", async () => {
    const { fundingRateFromJsonNumber } = await import("../src/core/perps/fundingRate");
    assert.equal(fundingRateFromJsonNumber(JSON.parse("0.0001")), "0.0001");
    assert.equal(fundingRateFromJsonNumber(JSON.parse("1.25e-07")), "0.000000125");
    assert.equal(fundingRateFromJsonNumber(JSON.parse("-1.2345678901234568e-5")), "-0.000012345678901234568");
    assert.equal(fundingRateFromJsonNumber(JSON.parse("1e21")), "1000000000000000000000");
    assert.equal(fundingRateFromJsonNumber(0), "0");
    assert.equal(fundingRateFromJsonNumber("0.0000125"), "0.0000125");
    for (const bad of [null, undefined, "", "1e-4", "abc", Number.NaN, Infinity, {}]) assert.equal(fundingRateFromJsonNumber(bad), null);
  });

  it("a null or malformed rate renders as a dash and a present one rounds exactly", async () => {
    const { formatFundingDecimalPct } = await import("../src/core/perps/fundingRate");
    assert.equal(formatFundingDecimalPct(null), "—");
    assert.equal(formatFundingDecimalPct("1e-4"), "—");
    assert.equal(formatFundingDecimalPct("0.0001"), "0.0100%");
    assert.equal(formatFundingDecimalPct("-0.000012345678901234567"), "-0.0012%");
    assert.equal(formatFundingDecimalPct("0.000000005"), "0.0000%");
    assert.equal(formatFundingDecimalPct("0.0000005"), "0.0001%");
    assert.equal(formatFundingDecimalPct("-0.0000000001"), "0.0000%");
    assert.equal(formatFundingDecimalPct("0"), "0.0000%");
    assert.equal(formatFundingRatePct(parseFundingRateHourly("0.0008")), formatFundingDecimalPct("0.0008"));
  });

  it("the markets table and funding tab render a missing 8h rate as a dash and the present one through formatFundingRatePct", () => {
    const read = (file: string) => readFileSync(ROOT + file, "utf8");
    assert.match(read("src/widgets/markets/useMarketsRows.ts"), /funding8h: snap\?\.funding_8h \?\? null,/);
    assert.match(read("src/widgets/markets/MarketsTable.tsx"), /row\.funding8h === null \? "—" : `\$\{row\.funding8h >= 0n \? "\+" : ""\}\$\{formatFundingRatePct\(row\.funding8h\)\}`/);
    assert.match(read("src/page-modules/markets/index.tsx"), /row\.funding8h === null \? "—" : formatFundingRatePct\(row\.funding8h\)/);
    assert.match(read("src/widgets/trading/PairHeader.tsx"), /formatFundingDecimalPct\(estFunding\)/);
    assert.match(read("src/widgets/trading/MobileTradingLayout.tsx"), /text: formatFundingDecimalPct\(wsStats\.funding_rate\)/);
    const stream = read("src/hooks/perps/useSidPaxMarketStream.ts");
    assert.match(stream, /funding_rate: {9}fundingRateFromJsonNumber\(d\.funding_rate\),/);
    assert.match(stream, /last_funding_rate: {4}fundingRateFromJsonNumber\(d\.last_funding_rate\),/);
  });
});
