import test from "node:test";
import assert from "node:assert/strict";
import {
  POSITIONS_QUERY,
  POSITION_CHANGED_QUERY,
} from "../packages/paxeer-backend/src/hooks/perps/useSubgraph";
import { walletPositions } from "../packages/hooks/src/native/NativeRuntime";

const fields = (document: string) =>
  document
    .slice(document.indexOf("{", document.indexOf("(")) + 1)
    .split(/\s+/)
    .filter((word) => /^\w+$/.test(word));

test("positionChanged subscription selects userAddress with the same fields as the positions query", () => {
  assert.match(POSITION_CHANGED_QUERY, /\buserAddress\b[^(]*\bmarketId\b/);
  assert.deepEqual(fields(POSITION_CHANGED_QUERY), fields(POSITIONS_QUERY));
});

test("native wallet filter keeps a still-open positionChanged row for the connected wallet", () => {
  const wallet = "0x00000000000000000000000000000000000000B2";
  const payload = {
    positionChanged: [
      {
        positionId: "7",
        userAddress: wallet.toLowerCase(),
        marketId: "1",
        isLong: true,
        sizeUsd: "1000000000000000000000",
        leverage: "2000000000000000000",
        entryPrice: "3000000000000000000000",
        collateralToken: "0x0000000000000000000000000000000000000001",
        collateralAmount: "500000000",
        collateralUsd: "500000000000000000000",
        status: "open",
        openedAt: "1000",
        openTxHash: "0xopen",
        market: { symbol: "ETH" },
      },
      {
        positionId: "8",
        userAddress: "0x00000000000000000000000000000000000000c3",
        marketId: "1",
        status: "open",
      },
    ],
  };
  const kept = walletPositions(payload.positionChanged, wallet);
  assert.deepEqual(
    kept.map((row) => row.positionId),
    ["7"],
  );
});
