/* eslint-disable @typescript-eslint/no-explicit-any */
/* eslint-disable @typescript-eslint/no-unused-vars */
import {
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState,
} from "react";
import { SDKError } from "@paxeer-network/types";
import { Decimal } from "@paxeer-network/utils";
import { PaxeerNetworkContext } from "../paxeerNetworkContext";
import { useEventEmitter } from "../useEventEmitter";
import { useLocalStorage } from "../useLocalStorage";
import { useWS } from "../useWS";
import orderbooksService from "./orderbook.service";
import { useMarkPrice } from "./useMarkPrice";
import { useSymbolsInfo } from "./useSymbolsInfo";
import { useTickerStream } from "./useTickerStream";

export type OrderBookItem = number[];

export type OrderbookData = {
  asks: OrderBookItem[];
  bids: OrderBookItem[];
};

const paddingFn = (len: number) =>
  Array(len).fill([Number.NaN, Number.NaN, Number.NaN, Number.NaN]);

const asksSortFn = (a: OrderBookItem, b: OrderBookItem) => a[0] - b[0];

const bidsSortFn = (a: OrderBookItem, b: OrderBookItem) => b[0] - a[0];

const isNumber = (val: unknown): val is number => {
  return typeof val === "number" && !Number.isNaN(val);
};

export const getPriceKey = (
  rawPrice: number,
  depth: number,
  isAsks: boolean,
) => {
  return new Decimal(rawPrice)
    .div(depth)
    .toDecimalPlaces(0, isAsks ? Decimal.ROUND_CEIL : Decimal.ROUND_FLOOR)
    .mul(depth)
    .toNumber();
};

const reduceItems = (
  depth: number | undefined,
  data: OrderBookItem[],
  isAsks: boolean,
) => {
  if (!Array.isArray(data) || data.length === 0) {
    return [];
  }
  let newData = [...data];
  const result: OrderBookItem[] = [];

  if (typeof depth !== "undefined") {
    const pricesMap = new Map<number, [number, number, number]>();
    const len = data.length;
    for (let i = 0; i < len; i++) {
      const [rawPrice, quantity] = data[i];
      if (!isNumber(rawPrice) || !isNumber(quantity)) {
        continue;
      }

      const priceKey = getPriceKey(rawPrice, depth, isAsks);
      const amtByRaw = new Decimal(rawPrice).mul(quantity).toNumber();

      if (pricesMap.has(priceKey)) {
        const item = pricesMap.get(priceKey)!;
        const sumQty = new Decimal(item[1]).add(quantity).toNumber();
        const sumAmtByRaw = new Decimal(item[2] ?? 0).add(amtByRaw).toNumber();
        pricesMap.set(priceKey, [priceKey, sumQty, sumAmtByRaw]);
      } else {
        pricesMap.set(priceKey, [priceKey, quantity, amtByRaw]);
      }
    }

    newData = Array.from<[number, number, number]>(pricesMap.values());
  }

  for (let i = 0; i < newData.length; i++) {
    const [price, quantity, sumAmtByRaw] = newData[i];

    if (!isNumber(price) || !isNumber(quantity)) {
      continue;
    }

    const resLen = result.length;

    const newQuantity = new Decimal(quantity)
      .add(resLen ? result[resLen - 1][2] : 0)
      .toNumber();

    const pieceAmount = isNumber(sumAmtByRaw)
      ? sumAmtByRaw
      : new Decimal(quantity).mul(price).toNumber();

    const newAmount = new Decimal(pieceAmount)
      .add(resLen ? result[resLen - 1][3] : 0)
      .toNumber();

    result.push([price, quantity, newQuantity, newAmount]);
  }

  return result;
};

export const reduceOrderbook = (
  depth: number | undefined,
  level: number,
  padding: boolean,
  data: OrderbookData,
): OrderbookData => {
  let asks = reduceItems(depth, data.asks, true);
  let bids = reduceItems(depth, data.bids, false);

  /// not empty and asks.price <= bids.price
  if (asks.length !== 0 && bids.length !== 0 && asks[0][0] <= bids[0][0]) {
    if (asks.length === 1) {
      const [price, qty, newQuantity, newAmount] = asks[0];
      asks.shift();
      asks.push([
        price + (depth === undefined ? 0 : Number(depth)),
        qty,
        newQuantity,
        newAmount,
      ]);
    } else {
      const [bidPrice] = bids[0];
      while (asks.length > 0) {
        const [askPrice, askQty, newQuantity, newAmount] = asks[0];
        if (askPrice <= bidPrice) {
          asks.shift();
          for (let i = 0; i < asks.length; i++) {
            if (i === 0) {
              const quantity = new Decimal(asks[i][1]).add(askQty);
              asks[i][1] = quantity.toNumber();
              asks[i][2] = quantity.toNumber();
              asks[i][3] = quantity
                .toDecimalPlaces(0, Decimal.ROUND_CEIL)
                .mul(asks[i][0])
                .toNumber();
            } else {
              asks[i][3] = new Decimal(asks[i][0])
                .mul(asks[i][1])
                .add(asks[i - 1][3])
                .toNumber();
            }
          }
        } else {
          break;
        }
      }
    }
  }

  asks = asks.reverse();

  if (padding) {
    asks =
      asks.length < level ? paddingFn(level - asks.length).concat(asks) : asks;
    bids =
      bids.length < level ? bids.concat(paddingFn(level - bids.length)) : bids;
  }

  return {
    asks: asks,
    bids: bids,
  };
};

const mergeItems = (data: OrderBookItem[], update: OrderBookItem[]) => {
  // let index = -1;
  if (data.length === 0) {
    return update;
  }

  data = data.filter(([price]) => isNumber(price));

  while (update.length > 0) {
    const item = update.shift();

    if (item) {
      const [price, quantity] = item;

      const index = data.findIndex(([p]) => p === price);

      if (index === -1) {
        if (quantity === 0) {
          continue;
        }
        data.push(item);
      } else {
        if (quantity === 0) {
          data.splice(index, 1);
        } else {
          data[index] = item;
        }
      }
    }
  }

  return data;
};

export const mergeOrderbook = (data: OrderbookData, update: OrderbookData) => {
  let asks = [...data.asks];
  let bids = [...data.bids];

  asks = mergeItems(asks, update.asks).sort(asksSortFn);
  bids = mergeItems(bids, update.bids).sort(bidsSortFn);

  return {
    asks: asks,
    bids: bids,
  };
};

/**
 * Configuration for the Order Book
 */
export type OrderbookOptions = {
  /** Indicates the number of data entries to return for ask/bid, default is 10 */
  level?: number;
  /** Whether to fill in when the actual data entries are less than the level. If filled, it will add [nan, nan, nan, nan]. Default is true */
  padding?: boolean;
};

export const PAXEER_NETWORK_ORDERBOOK_DEPTH_KEY =
  "paxeerNetwork_orderbook_depth_key";

const INIT_DATA: OrderbookData = {
  asks: [],
  bids: [],
};

/**
 * @description React hook that returns the current orderbook for a given market
 */
export const useOrderbookStream = (
  symbol: string,
  initial: OrderbookData = INIT_DATA,
  options?: OrderbookOptions,
) => {
  if (!symbol) {
    throw new SDKError("Symbol is required");
  }

  const level = options?.level ?? 10;
  const padding = options?.padding ?? true;

  const symbolRef = useRef<string>(symbol);

  symbolRef.current = symbol;

  const {
    defaultOrderbookTickSizes: DEFAULT_TICK_SIZES = {},
    defaultOrderbookSymbolDepths: DEFAULT_SYMBOL_DEPTHS = {},
  } = useContext(PaxeerNetworkContext);

  const [data, setData] = useState<OrderbookData>(initial);
  const [isLoading, setIsLoading] = useState(true);
  // const [level, setLevel] = useState(() => options?.level ?? 10);

  const config = useSymbolsInfo()[symbol];

  const [depthObject, setDepthObject] = useLocalStorage(
    PAXEER_NETWORK_ORDERBOOK_DEPTH_KEY,
    {},
  );

  // markPrice, lastPrice
  const prevMiddlePrice = useRef<number>(0);

  const tick = config("quote_tick");

  const depths = useMemo(() => {
    if (DEFAULT_SYMBOL_DEPTHS[symbol]) {
      return DEFAULT_SYMBOL_DEPTHS[symbol];
    }
    if (typeof tick === "undefined") {
      return [];
    }
    try {
      const base = new Decimal(tick);
      return [
        base.toNumber(),
        base.mul(10).toNumber(),
        base.mul(100).toNumber(),
        base.mul(1000).toNumber(),
      ];
    } catch {
      //
    }
    return [tick];
  }, [symbol, tick]);

  useEffect(() => {
    if (depthObject[symbol]) {
      return;
    }
    if (DEFAULT_TICK_SIZES[symbol]) {
      // @ts-ignore
      setDepthObject((prev) => ({
        ...prev,
        [symbol]: Number(DEFAULT_TICK_SIZES[symbol]),
      }));
    } else {
      // @ts-ignore
      setDepthObject((prev) => ({
        ...prev,
        [symbol]: tick,
      }));
    }
  }, [depthObject, tick, symbol, DEFAULT_TICK_SIZES]);

  const onDepthChange = useCallback(
    (val: number) => {
      // @ts-ignore
      setDepthObject((prev) => ({ ...prev, [symbol]: val }));
    },
    [symbol],
  );

  const ws = useWS();

  const ticker = useTickerStream(symbol);

  const eventEmitter = useEventEmitter();

  // const orderbookRequest =

  useEffect(() => {
    let needRequestFullOrderbook = true;
    setIsLoading(true);

    let fullOrderBookUpdateSub: any;

    const orderBookUpdateSub = ws.subscribe(
      {
        event: "subscribe",
        topic: `${symbol}@orderbookupdate`,
      },
      {
        formatter: (message) => message,
        onMessage: (message: any) => {
          const { data: wsData } = message;
          const ts = wsData.ts ?? message.ts;
          const { symbol, asks, bids, prevTs } = wsData;
          // Effect cleanup releases the old symbol's subscription.
          if (symbolRef.current !== symbol) {
            return;
          }
          // Stream outage: drop stale rows and stay loading until the next snapshot.
          if (wsData.reset === true) {
            orderbooksService.resetOrderBook(symbol);
            setData(INIT_DATA);
            setIsLoading(true);
            return;
          }
          if (wsData.snapshot === true) {
            orderbooksService.setFullOrderbook(symbol, { asks, bids, ts });
            setIsLoading(false);
          } else {
            orderbooksService.updateOrderbook(
              symbol,
              { asks, bids, ts, prevTs },
              () => {
                needRequestFullOrderbook = true;
              },
            );
          }

          const data = orderbooksService.getRawOrderbook(symbol);
          if (!data) return;
          setData({ bids: data.bids, asks: data.asks });
        },
      },
    );

    if (needRequestFullOrderbook) {
      setIsLoading(true);

      fullOrderBookUpdateSub = ws.onceSubscribe(
        {
          event: "request",
          id: `${symbol}@orderbook`,
          params: {
            type: "orderbook",
            symbol: symbol,
          },
        },
        {
          formatter: (message) => message,
          onMessage: (message: any) => {
            // when current symbol is not the same as the ws symbol, skip update data
            const { symbol, asks, bids, ts, reset } = message.data;
            if (symbolRef.current !== symbol) {
              return;
            }
            if (reset === true) {
              orderbooksService.resetOrderBook(symbol);
              setData(INIT_DATA);
              setIsLoading(true);
              return;
            }
            orderbooksService.setFullOrderbook(symbol, { asks, bids, ts });
            const data = orderbooksService.getRawOrderbook(symbol);
            setData({ bids: data.bids, asks: data.asks });

            setIsLoading(false);
          },
        },
      );
      needRequestFullOrderbook = false;
    }

    return () => {
      // unsubscribe
      orderBookUpdateSub?.();
      fullOrderBookUpdateSub?.();
      orderbooksService.resetOrderBook(symbol);
      setData(INIT_DATA);
    };
  }, [symbol]);

  // const {data:markPrices} = useMarkPricesStream();

  const { data: markPrice } = useMarkPrice(symbol);

  const onItemClick = useCallback((item: OrderBookItem) => {
    eventEmitter.emit("orderbook:item:click", item);
  }, []);

  const reducedData = reduceOrderbook(depthObject[symbol], level, padding, {
    asks: data.asks,
    bids: data.bids,
  });

  useEffect(() => {
    eventEmitter.emit("orderbook:update", reducedData);
  }, [reducedData]);

  const middlePrice = useMemo(() => {
    let asksFrist = 0;
    let bidsFirst = 0;

    if (data.asks.length > 0) {
      asksFrist = reducedData.asks?.[reducedData.asks.length - 1]?.[0];
    }

    if (data.bids.length > 0) {
      bidsFirst = data.bids[0][0];
    }

    if (!isNumber(asksFrist) || !isNumber(bidsFirst) || !ticker) {
      return 0;
    }

    return [asksFrist, bidsFirst, ticker["24h_close"]].sort()[1];
  }, [ticker?.["24h_close"], data]);

  useEffect(() => {
    prevMiddlePrice.current = middlePrice;
  }, [middlePrice]);

  return [
    {
      asks: reducedData.asks.slice(-level),
      bids: reducedData.bids.slice(0, level),
      markPrice: markPrice,
      middlePrice: [prevMiddlePrice.current, middlePrice],
    },
    {
      onDepthChange: onDepthChange,
      depth: depthObject[symbol] as number,
      allDepths: depths,
      isLoading,
      onItemClick,
    },
  ];
};

export type useOrderbookStreamReturn = ReturnType<typeof useOrderbookStream>;
