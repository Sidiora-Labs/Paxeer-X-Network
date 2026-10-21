import { it } from "node:test";
import { strict as assert } from "node:assert";

import { gasWithMargin } from "../src/core/network/gas";

it("gasIsEstimateTimesTwelveTenths", () => {
  assert.equal(gasWithMargin(500_000n), 600_000n);
  assert.equal(gasWithMargin(21_000n), 25_200n);
  assert.equal(gasWithMargin(7n), 8n);
  assert.equal(gasWithMargin(0n), 0n);
});
