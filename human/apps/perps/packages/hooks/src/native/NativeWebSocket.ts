import {
  subscribeNativeOrderbook,
  subscribeNativeTrades,
} from "@paxeer-network/paxeer-backend/hooks/perps/useSidPaxMarketStream";
import EventEmitter from "eventemitter3";
import {
  getNativeSnapshot,
  subscribeNativeSnapshot,
} from "@paxeer-network/paxeer-backend/runtime";
import {
  nativeMarketRows,
  toNativeSymbol,
} from "@paxeer-network/paxeer-backend/nativeMetadata";
import {
  nativeRequest,
  nativePositionRows,
} from "@paxeer-network/paxeer-backend/nativeClient";
import { activePoll } from "@paxeer-network/paxeer-backend/core/net/realtime";
export class NativeWebSocket extends EventEmitter {
  constructor(
    private readonly streams = {
      subscribeNativeOrderbook,
      subscribeNativeTrades,
    },
  ) {
    super();
  }
  client = { public: { readyState: 0 }, private: { readyState: 0 } };
  subscribe(topic: any, options: any = {}) {
    const name =
      typeof topic === "string"
        ? topic
        : (topic.topic ?? topic.id ?? topic.event ?? "");
    let active = true;
    const emit = (data: any) => {
      if (!active) return;
      const message = { topic: name, data, ts: Date.now() };
      options.onMessage?.(
        options.formatter ? options.formatter(message) : data,
      );
    };
    const publish = () => {
      const state = getNativeSnapshot();
      const rows = nativeMarketRows();
      if (state.markets.length && this.client.public.readyState !== 1) {
        this.client.public.readyState = 1;
        this.emit("status:change", { type: "open", isPrivate: false });
      }
      if (name === "markprices")
        emit(
          rows
            .filter((row) => Number.isFinite(row.mark_price))
            .map((row) => ({ symbol: row.symbol, price: row.mark_price })),
        );
      else if (name === "indexprices")
        emit(
          rows
            .filter((row) => Number.isFinite(row.index_price))
            .map((row) => ({ symbol: row.symbol, price: row.index_price })),
        );
      else if (name === "openinterests")
        emit(
          rows
            .filter((row) => Number.isFinite(row.open_interest))
            .map((row) => ({
              symbol: row.symbol,
              openInterest: row.open_interest,
            })),
        );
      else if (name === "tickers")
        emit(
          rows.map((row) => ({
            ...row,
            open: row["24h_open"],
            close: row["24h_close"],
            high: row["24h_high"],
            low: row["24h_low"],
            volume: row["24h_volume"],
            amount: row["24h_amount"],
          })),
        );
      else if (name.endsWith("@indexprice") || name.endsWith("@openinterest")) {
        const row = rows.find((row) => row.symbol === name.split("@")[0]);
        if (row)
          emit({
            symbol: row.symbol,
            price: row.index_price,
            openInterest: row.open_interest,
          });
      } else if (name.endsWith("@ticker")) {
        const row = rows.find((row) => row.symbol === name.split("@")[0]);
        if (row && Number.isFinite(row.mark_price))
          emit({
            symbol: row.symbol,
            open: row["24h_open"],
            close: row["24h_close"],
            high: row["24h_high"],
            low: row["24h_low"],
            volume: row["24h_volume"],
          });
      } else if (name === "position" && state.connected)
        emit({ positions: nativePositionRows() });
    };
    const unsubscribe = subscribeNativeSnapshot(publish);
    queueMicrotask(publish);
    const symbol = (
      name.includes("@") ? name.split("@")[0] : topic?.params?.symbol
    ) as string | undefined;
    let stopPoll: (() => void) | undefined;
    const requestType =
      topic?.event === "request" ? topic.params?.type : undefined;
    if (symbol && (name.includes("orderbook") || requestType === "orderbook")) {
      stopPoll = this.streams.subscribeNativeOrderbook(
        toNativeSymbol(symbol),
        (book) =>
          emit({
            symbol,
            snapshot: true,
            ts: book.ts,
            asks: book.asks.map((level) => [level.price, level.size]),
            bids: book.bids.map((level) => [level.price, level.size]),
          }),
      );
    } else if (symbol && (name.includes("trade") || requestType === "trade")) {
      const seen = new Set<string>();
      stopPoll = this.streams.subscribeNativeTrades(
        toNativeSymbol(symbol),
        (trades) => {
          const rows = trades.map((trade) => ({
            symbol,
            side: trade.side,
            price: trade.price,
            size: trade.contracts,
            executed_quantity: trade.contracts,
            executed_price: trade.price,
            executed_timestamp: trade.trade_time,
            ts: trade.trade_time,
            trade_id:
              trade.trade_id ??
              `${trade.trade_time}:${trade.side}:${trade.price}:${trade.contracts}`,
          }));
          // History requests return a list; live subscriptions deliver one trade.
          if (requestType === "trade") {
            emit(rows.slice(0, topic.params.limit ?? 50));
            return;
          }
          for (const trade of rows.reverse()) {
            const id = trade.trade_id;
            if (seen.has(id)) continue;
            seen.add(id);
            emit(trade);
          }
          if (seen.size > 2000) {
            const recent = [...seen].slice(-1000);
            seen.clear();
            recent.forEach((id) => seen.add(id));
          }
        },
      );
    }
    return () => {
      if (!active) return;
      active = false;
      unsubscribe();
      stopPoll?.();
    };
  }
  onceSubscribe(topic: any, options: any = {}) {
    let stop: (() => void) | undefined;
    let received = false;
    stop = this.subscribe(topic, {
      ...options,
      onMessage: (data: any) => {
        if (received) return;
        received = true;
        try {
          options.onMessage?.(data);
        } finally {
          stop?.();
        }
      },
    });
    // Cached snapshots can invoke onMessage before subscribe returns its cleanup.
    if (received) stop();
    return stop;
  }
  privateSubscribe(topic: any, options: any = {}) {
    return this.subscribe(topic, options);
  }
  openPrivate() {
    this.client.private.readyState = getNativeSnapshot().connected ? 1 : 0;
  }
  closePrivate() {
    this.client.private.readyState = 0;
  }
  close() {
    this.client.public.readyState = 0;
    this.closePrivate();
  }
  send(message: any, options?: any) {
    if (options?.onMessage) return this.subscribe(message, options);
    return undefined;
  }
  sendPrivate(message: any, options?: any) {
    return this.send(message, options);
  }
}
export const nativeWebSocket = new NativeWebSocket();
