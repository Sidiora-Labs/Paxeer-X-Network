import test from "node:test";
import assert from "node:assert/strict";
import { nativeRequest } from "../packages/paxeer-backend/src/nativeClient";
import { updateNativeSnapshot } from "../packages/paxeer-backend/src/runtime";
import { get } from "../packages/net/src/fetch/index";
import {
  calculateAssetHistoryData,
  sumTransfersAfterSnapshot,
} from "../packages/portfolio/src/pages/overview/shared/useAssetHistory";
import { Decimal, zero } from "../packages/utils/src";
import type { API } from "../packages/types/src";

const wallet = "0x00000000000000000000000000000000000000a1";
const end = Date.now();
const start = end - 10 * 24 * 60 * 60 * 1000;
const transferUrl = (side: "IN" | "OUT") =>
  `/v1/internal_transfer_history?page=1&size=200&side=${side}&start_t=${start}&end_t=${end}`;

const convertToUSDCAndOperate = (inputs: {
  token: string;
  amount: string | number;
  value: Decimal;
  op?: "add" | "sub";
}) =>
  inputs.op === "add"
    ? inputs.value.add(inputs.amount)
    : inputs.value.sub(inputs.amount);

test("native internal transfer history answers an empty list for both sides", async () => {
  updateNativeSnapshot({ address: wallet, connected: true });
  try {
    for (const side of ["OUT", "IN"] as const) {
      const response = await nativeRequest(transferUrl(side));
      assert.equal(response.success, true);
      assert.deepEqual(response.data.rows, []);
      assert.deepEqual(response.data.meta, {
        current_page: 1,
        records_per_page: 200,
        total: 0,
      });
      const rows = await get(transferUrl(side), {});
      assert.ok(Array.isArray(rows));
      assert.deepEqual(rows, []);
    }
  } finally {
    updateNativeSnapshot({ address: null, connected: false });
  }
});

test("empty native transfer history no longer blocks the daily performance rows", async () => {
  updateNativeSnapshot({ address: wallet, connected: true });
  try {
    const transferOutHistory = await get(transferUrl("OUT"), {});
    const transferInHistory = await get(transferUrl("IN"), {});
    const daily: API.DailyRow[] = [
      {
        account_value: 100,
        broker_id: "paxeer",
        date: "2026-10-15",
        perp_volume: 40,
        pnl: 3,
        snapshot_time: 1,
      },
      {
        account_value: 104,
        broker_id: "paxeer",
        date: "2026-10-16",
        perp_volume: 60,
        pnl: 4,
        snapshot_time: 2,
      },
    ];
    const lastItem = daily[daily.length - 1];
    const totalTransferIn = sumTransfersAfterSnapshot(
      transferInHistory,
      lastItem,
      convertToUSDCAndOperate,
    );
    const totalTransferOut = sumTransfersAfterSnapshot(
      transferOutHistory,
      lastItem,
      convertToUSDCAndOperate,
    );
    assert.ok(totalTransferIn !== null && totalTransferIn.eq(zero));
    assert.ok(totalTransferOut !== null && totalTransferOut.eq(zero));
    assert.equal(
      sumTransfersAfterSnapshot(undefined, lastItem, convertToUSDCAndOperate),
      null,
    );

    const historical = calculateAssetHistoryData({
      data: daily,
      realtime: false,
      totalValue: 110,
      totalTransferIn,
      totalTransferOut,
      todayVol: 0,
      periodValue: 7,
      today: new Date("2026-10-18T12:00:00Z"),
      assetHistory: [],
      convertToUSDCAndOperate,
      totalDeposit: { current: zero },
    });
    assert.deepEqual(historical, daily);

    const realtime = calculateAssetHistoryData({
      data: daily,
      realtime: true,
      totalValue: 110,
      totalTransferIn,
      totalTransferOut,
      todayVol: 25,
      periodValue: 7,
      today: new Date("2026-10-18T12:00:00Z"),
      assetHistory: [],
      convertToUSDCAndOperate,
      totalDeposit: { current: zero },
    });
    assert.equal(realtime.length, 3);
    assert.deepEqual(realtime.slice(0, 2), daily);
    const todayRow = realtime[2] as API.DailyRow & { __isCalculated?: boolean };
    assert.equal(todayRow.date, "2026-10-18");
    assert.equal(todayRow.pnl, 6);
    assert.equal(todayRow.perp_volume, 25);
    assert.equal(todayRow.__isCalculated, true);
  } finally {
    updateNativeSnapshot({ address: null, connected: false });
  }
});
