import test from "node:test";
import assert from "node:assert/strict";
import { NativeWebSocket } from "../packages/hooks/src/native/NativeWebSocket";
import type { SidPaxTrade } from "../packages/paxeer-backend/src/hooks/perps/useSidPaxMarketStream";

const trade = (id: string, ts: number): SidPaxTrade => ({
  trade_id: id,
  trade_time: ts,
  side: "BUY",
  price: 2400,
  contracts: 2,
  notional: 4800,
  size: 4800,
  liquidation: false,
});

test("native orderbook delivery identifies complete snapshots and preserves their source timestamp", () => {
  const messages: any[] = [];
  const socket = new NativeWebSocket({
    subscribeNativeOrderbook: (_symbol, callback) => {
      callback({
        ts: 1234,
        asks: [{ price: 2401, size: 3 }],
        bids: [{ price: 2400, size: 2 }],
      });
      return () => {};
    },
    subscribeNativeTrades: () =>
      assert.fail("Orderbooks must not subscribe to trade history"),
  });
  const stop = socket.subscribe("PERP_ETH_USDC@orderbookupdate", {
    onMessage: (data: unknown) => messages.push(data),
  });
  assert.deepEqual(messages, [
    {
      symbol: "PERP_ETH_USDC",
      snapshot: true,
      ts: 1234,
      asks: [[2401, 3]],
      bids: [[2400, 2]],
    },
  ]);
  stop();
});

test("trade history requests return a limited array and release synchronous cached subscriptions", () => {
  let stopped = 0;
  const messages: any[] = [];
  const socket = new NativeWebSocket({
    subscribeNativeOrderbook: () =>
      assert.fail("Trade history must not subscribe to an orderbook"),
    subscribeNativeTrades: (symbol, callback) => {
      assert.equal(symbol, "ETH");
      callback([trade("new", 2000), trade("old", 1000)]);
      return () => {
        stopped++;
      };
    },
  });
  const stop = socket.onceSubscribe(
    {
      id: "PERP_ETH_USDC@trade",
      event: "request",
      params: { type: "trade", symbol: "PERP_ETH_USDC", limit: 1 },
    },
    { onMessage: (data: unknown) => messages.push(data) },
  );

  assert.equal(messages.length, 1);
  assert.equal(Array.isArray(messages[0]), true);
  assert.equal(messages[0].length, 1);
  assert.equal(messages[0][0].trade_id, "new");
  assert.equal(messages[0][0].ts, 2000);
  assert.equal(stopped, 1);
  stop();
  assert.equal(stopped, 1);
});

test("live trades append individual unseen records with source timestamps and contract quantities", () => {
  let publish!: (trades: SidPaxTrade[]) => void;
  const messages: any[] = [];
  const socket = new NativeWebSocket({
    subscribeNativeOrderbook: () =>
      assert.fail("Live trades must not subscribe to an orderbook"),
    subscribeNativeTrades: (_symbol, callback) => {
      publish = callback;
      return () => {};
    },
  });
  const stop = socket.subscribe("PERP_ETH_USDC@trade", {
    onMessage: (data: unknown) => messages.push(data),
  });
  publish([trade("first", 1000)]);
  publish([trade("second", 2000), trade("first", 1000)]);
  assert.deepEqual(
    messages.map((row) => row.trade_id),
    ["first", "second"],
  );
  assert.equal(Array.isArray(messages[1]), false);
  assert.equal(messages[1].symbol, "PERP_ETH_USDC");
  assert.equal(messages[1].ts, 2000);
  assert.equal(messages[1].executed_timestamp, 2000);
  assert.equal(messages[1].size, 2);
  assert.equal(messages[1].executed_quantity, 2);
  stop();
  publish([trade("after-cleanup", 3000)]);
  assert.equal(messages.length, 2);
});
