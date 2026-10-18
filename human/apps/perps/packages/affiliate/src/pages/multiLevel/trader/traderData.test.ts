import assert from "node:assert/strict";
import test from "node:test";
import {
  calendarDateString,
  calendarDayDateRange,
  joinRebateVolume,
  rebateChartData,
  resolveRebateRows,
  sumRebates,
  sumVolume,
  utcDayWindow,
  utcYesterday,
} from "./traderData";

const legacyRow = { date: "2026-09-25", referee_rebate: 0.1, fee: 1 };
const multiRow = { date: "2026-09-26", referee_rebate: 0.2, fee: 2 };

test("UTC settlement windows end yesterday and include exactly the selected days", () => {
  assert.equal(utcYesterday(new Date("2026-09-28T00:01:00Z")), "2026-09-27");
  assert.equal(utcYesterday(new Date("2026-09-27T23:59:00Z")), "2026-09-26");
  assert.equal(utcYesterday(new Date("2024-03-01T00:00:00Z")), "2024-02-29");
  assert.deepEqual(utcDayWindow(7, "2026-01-02"), {
    startDate: "2025-12-27",
    endDate: "2026-01-02",
  });
  assert.deepEqual(utcDayWindow(2, "2024-03-01"), {
    startDate: "2024-02-29",
    endDate: "2024-03-01",
  });
});

test("default table range covers exactly 90 days ending at the end date", () => {
  const cases = [
    ["2026-09-29", "2026-07-02"],
    ["2024-03-01", "2023-12-03"],
    ["2026-01-31", "2025-11-03"],
  ];

  cases.forEach(([endDate, expectedStart]) => {
    const range = calendarDayDateRange(endDate, 90);
    assert.equal(calendarDateString(range.from), expectedStart);
    assert.equal(calendarDateString(range.to), endDate);
    assert.equal(utcDayWindow(90, endDate).startDate, expectedStart);
  });
});

test("multi-level rebate rows have priority and legacy rows are used when needed", () => {
  assert.deepEqual(resolveRebateRows([multiRow], undefined, [legacyRow]).rows, [
    multiRow,
  ]);
  assert.deepEqual(resolveRebateRows([], undefined, [legacyRow]).rows, [
    legacyRow,
  ]);
  assert.deepEqual(resolveRebateRows([], undefined, []).rows, []);
  assert.deepEqual(
    resolveRebateRows(undefined, new Error("multi"), [legacyRow]).rows,
    [legacyRow],
  );
  assert.ok(resolveRebateRows(undefined, new Error("multi"), []).error);
  assert.ok(
    resolveRebateRows([], undefined, undefined, new Error("legacy")).error,
  );
});

test("rebates and volume use decimal sums and rows join by date", () => {
  const rows = [legacyRow, { ...multiRow, referee_rebate: 0.2 }];
  const volumes = [
    { date: "2026-09-26", perp_volume: 10.1 },
    { date: "2026-09-25", perp_volume: 20.2 },
  ];
  assert.equal(sumRebates(rows), 0.3);
  assert.equal(sumVolume(volumes), 30.3);
  assert.deepEqual(
    joinRebateVolume(rows, volumes).map(({ date, volume }) => ({
      date,
      volume,
    })),
    [
      { date: "2026-09-26", volume: 10.1 },
      { date: "2026-09-25", volume: 20.2 },
    ],
  );
  assert.deepEqual(rebateChartData(rows, 3, "2026-09-26"), [
    { date: "2026-09-24", volume: 0 },
    { date: "2026-09-25", volume: 0.1 },
    { date: "2026-09-26", volume: 0.2 },
  ]);
});
