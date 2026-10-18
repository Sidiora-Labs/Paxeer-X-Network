import React, {
  createContext,
  FC,
  PropsWithChildren,
  useContext,
  useState,
} from "react";
import { BasicSymbolInfo } from "../../../types/types";
import { QtyMode, TotalMode } from "./types";

export interface OrderBookContextValue {
  cellHeight: number;
  mode: QtyMode;
  depth?: string;
  onModeChange?: (mode: QtyMode) => void;
  onItemClick?: (item: number[]) => void;
  showTotal: boolean;
  totalMode: TotalMode;
  pendingOrders: ReadonlyArray<number> | number[];
  onTotalModeChange?: (mode: TotalMode) => void;
  symbolInfo: BasicSymbolInfo;
  showBuySellRatio: boolean;
  onShowBuySellRatioChange?: (show: boolean) => void;
}

export const OrderBookContext = createContext({
  cellHeight: 22,
} as OrderBookContextValue);

export const useOrderBookContext = () => useContext(OrderBookContext);

export const ORDERBOOK_COIN_TYPE_KEY = "orderbook_coin_type";

export const ORDERBOOK_MOBILE_COIN_TYPE_KEY = "orderbook_mobile_coin_unit";

interface OrderBookProviderProps {
  cellHeight: number;
  depth?: string;
  showTotal: boolean;
  pendingOrders: ReadonlyArray<number> | number[];
  onItemClick?: (item: number[]) => void;
  symbolInfo: BasicSymbolInfo;
  showBuySellRatio?: boolean;
  onShowBuySellRatioChange?: (show: boolean) => void;
}

export const OrderBookProvider: FC<
  PropsWithChildren<OrderBookProviderProps>
> = (props) => {
  const [mode, setMode] = useState<QtyMode>("quantity");
  const [totalMode, setTotalMode] = useState<QtyMode>("quantity");
  const memoizedValue = React.useMemo<OrderBookContextValue>(() => {
    return {
      cellHeight: props.cellHeight,
      onItemClick: props.onItemClick,
      mode,
      totalMode: totalMode || "quantity",
      depth: props.depth,
      onModeChange: setMode,
      onTotalModeChange: setTotalMode,
      showTotal: props.showTotal || false,
      pendingOrders: props.pendingOrders,
      symbolInfo: props.symbolInfo,
      showBuySellRatio: props.showBuySellRatio ?? true,
      onShowBuySellRatioChange: props.onShowBuySellRatioChange,
    };
  }, [
    mode,
    props.cellHeight,
    props.depth,
    props.onItemClick,
    props.pendingOrders,
    props.showTotal,
    props.symbolInfo,
    props.showBuySellRatio,
    props.onShowBuySellRatioChange,
    totalMode,
  ]);
  return (
    <OrderBookContext.Provider value={memoizedValue}>
      {props.children}
    </OrderBookContext.Provider>
  );
};
