import assert from "node:assert/strict";
import { test } from "node:test";
import { Transform } from "./transform";

test("multi-field volume sorting keeps invalid values after valid markets", () => {
  const rows = [
    { symbol: "HIGH", "24h_amount": 2000 },
    { symbol: "INVALID", "24h_amount": Number.NaN },
    { symbol: "MID", "24h_amount": 1500 },
    { symbol: "MISSING" },
  ];
  const sorted = Transform.dataSource(
    rows,
    [
      {
        dataIndex: "24h_amount",
        multiSort: { fields: [{ sortKey: "24h_amount" }] },
      },
    ],
    [{ id: "24h_amount", desc: true }],
  );

  assert.deepEqual(
    sorted.map((row) => row.symbol),
    ["HIGH", "MID", "INVALID", "MISSING"],
  );
});
