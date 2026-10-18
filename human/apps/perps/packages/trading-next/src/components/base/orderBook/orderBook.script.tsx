import { useEffect, useMemo, useState } from "react";
import {
  useLocalStorage,
  useOrderbookStream,
  useOrderStream,
  useSymbolsInfo,
} from "@paxeer-network/hooks";
import { OrderStatus } from "@paxeer-network/types";
import { useScreen } from "@paxeer-network/ui";
import { Decimal, removeTrailingZeros } from "@paxeer-network/utils";
import { getBasicSymbolInfo } from "../../../utils/utils";

const CELL_MAX = 30;

const DEFAULT_CELL_HEIGHT = 20;

const SPACE = 104;
const BUY_SELL_RATIO_BAR_HEIGHT_MOBILE = 28; // padding 10px*2 + content height ~14px
const BUY_SELL_RATIO_BAR_HEIGHT_DESKTOP = 38; // padding 10px*2 + content height ~14px

export const ORDERBOOK_SHOW_BUY_SELL_RATIO_KEY =
  "orderbook_show_buy_sell_ratio";

export interface BuySellRatio {
  buyPercentage: number;
  sellPercentage: number;
  buyAmount: number;
  sellAmount: number;
}

export const useOrderBookScript = (props: {
  symbol: string;
  height?: number;
}) => {
  const { symbol, height } = props;
  const symbolInfo = useSymbolsInfo()[symbol];

  const [cellHeight, setCellHeight] = useState(DEFAULT_CELL_HEIGHT);

  const [level, setLevel] = useState(10);
  const { base, quote, quote_dp } = getBasicSymbolInfo(symbolInfo);
  // const counter = useRef(0);

  const [data, { onDepthChange, isLoading, onItemClick, depth, allDepths }] =
    useOrderbookStream(symbol, undefined, { level });

  // useEffect(() => {
  //   counter.current = 0;
  // }, [symbol]);

  const pendingOrders = usePendingOrderStream(symbol);

  const { isMobile } = useScreen();

  const [showBuySellRatio, setShowBuySellRatio] = useLocalStorage(
    ORDERBOOK_SHOW_BUY_SELL_RATIO_KEY,
    true,
  );

  useEffect(() => {
    if (height) {
      // Calculate available space considering BuySellRatioBar height
      const availableSpace =
        SPACE +
        (showBuySellRatio
          ? isMobile
            ? BUY_SELL_RATIO_BAR_HEIGHT_MOBILE
            : BUY_SELL_RATIO_BAR_HEIGHT_DESKTOP
          : 0);

      //   setCellHeight(height.content / level);
      const level = Math.floor(
        (height - availableSpace) / ((DEFAULT_CELL_HEIGHT + 1) * 2),
      );

      const cellsHeight = (DEFAULT_CELL_HEIGHT + 1) * 2 * level;

      const restSpace = height - availableSpace - cellsHeight;

      if (restSpace > 10) {
        setCellHeight(DEFAULT_CELL_HEIGHT + restSpace / level / 2);
      } else {
        setCellHeight(DEFAULT_CELL_HEIGHT);
      }

      setLevel(level);
    }
  }, [height, showBuySellRatio]);

  const selDepth = useMemo(() => {
    if (typeof depth === "undefined" || typeof quote_dp === "undefined") {
      return undefined;
    }
    // // FIXME: hardcode for now, need to optimize it
    // counter.current++;
    // if (counter.current === 1 && DEFAULT_DEPTH[symbol]) {
    //   return DEFAULT_DEPTH[symbol];
    // }

    return removeTrailingZeros(depth);
  }, [depth, quote_dp, symbol]);

  const depths = useMemo(() => {
    return allDepths?.map((e) => removeTrailingZeros(e)) || [];
  }, [allDepths, quote_dp]);

  const buySellRatio = useMemo<BuySellRatio | null>(() => {
    if (!data?.asks || !data?.bids || level === undefined) {
      return null;
    }

    // Helper function to check if a value is a valid number
    const isValidNumber = (value: any): value is number => {
      return (
        typeof value === "number" &&
        !Number.isNaN(value) &&
        Number.isFinite(value) &&
        value >= 0
      );
    };

    // Get visible orders within the level range
    const visibleAsks = data.asks.slice(0, level);
    const visibleBids = data.bids.slice(-level);
    // get asks first and bids last, get amount ratio
    if (visibleAsks.length > 0 && visibleBids.length > 0) {
      // Find the first ask that meets the condition (in forward order)
      const firstAsk = visibleAsks.find(
        (ask) =>
          Array.isArray(ask) &&
          ask.length === 4 &&
          ask[3] !== undefined &&
          ask[3] !== null &&
          isValidNumber(ask[3]),
      );
      // Find the first bid that meets the condition (in reverse order)
      const lastBid = [...visibleBids]
        .reverse()
        .find(
          (bid) =>
            Array.isArray(bid) &&
            bid.length === 4 &&
            bid[3] !== undefined &&
            bid[3] !== null &&
            isValidNumber(bid[3]),
        );
      if (firstAsk && lastBid) {
        // sell amount is the sum of all visible asks
        const askAmount = new Decimal(firstAsk[3]);
        // buy amount is the sum of all visible bids
        const bidAmount = new Decimal(lastBid[3]);
        const totalAmount = askAmount.add(bidAmount);
        const buyPercentage = bidAmount.div(totalAmount).mul(100).toNumber();
        const sellPercentage = 100 - buyPercentage;
        return {
          buyPercentage: buyPercentage,
          sellPercentage: sellPercentage,
          buyAmount: askAmount.toNumber(),
          sellAmount: bidAmount.toNumber(),
        };
      }
    }
    return null;
  }, [data?.asks, data?.bids, level]);

  return {
    level,
    asks: data?.asks,
    bids: data?.bids,
    markPrice: data?.markPrice,
    lastPrice: data?.middlePrice,
    depths,
    selDepth,
    /** Canonical API / route symbol (do not reconstruct from base+quote). */
    symbol,
    base,
    quote,
    isLoading,
    onItemClick,
    cellHeight,
    onDepthChange,
    pendingOrders,
    symbolInfo: getBasicSymbolInfo(symbolInfo),
    isMobile,
    showBuySellRatio,
    setShowBuySellRatio,
    buySellRatio,
  };
};

export const usePendingOrderStream = (symbol: string): number[] => {
  const [data] = useOrderStream({
    status: OrderStatus.INCOMPLETE,
    symbol: symbol,
  });

  const pendingOrders = useMemo(() => {
    const info = data
      ?.filter((item) => item.symbol === symbol)
      .reduce((a, b) => {
        // TODO: check if this is the correct price, when the data is from WS, it crashes
        const price = b.price || b.trigger_price || 0;
        return [...a, price];
      }, []);

    return info;
  }, [data, symbol]);

  return pendingOrders;
};

export type OrderBookState = ReturnType<typeof useOrderBookScript>;
