import type { PositionsProps } from "@paxeer-network/ui-positions";
import {
  usePendingOrderCount,
  usePositionsCount,
  useTradingLocalStorage,
} from "../../../hooks";
import { useTradingPageContext } from "../../../provider/tradingPageContext";

export enum DataListTabType {
  positions = "Positions",
  pending = "Pending",
  tp_sl = "TP/SL",
  filled = "Filled",
  positionHistory = "Position history",
  orderHistory = "Order history",
  liquidation = "Liquidation",
  assets = "Assets",
}

export const useDataListScript = (
  inputs: { current?: DataListTabType } & PositionsProps,
) => {
  const {
    current,
    pnlNotionalDecimalPrecision,
    sharePnLConfig,
    symbol,
    includedPendingOrder,
  } = inputs;

  const localStorage = useTradingLocalStorage({ pnlNotionalDecimalPrecision });

  const { onSymbolChange } = useTradingPageContext();

  const { positionCount } = usePositionsCount(symbol);

  const { pendingOrderCount, tpSlOrderCount } = usePendingOrderCount(symbol);

  return {
    current,
    sharePnLConfig,
    symbol,
    calcMode: localStorage.unPnlPriceBasis,
    includedPendingOrder,
    ...localStorage,
    positionCount,
    pendingOrderCount,
    tpSlOrderCount,
    onSymbolChange,
  };
};

export type DataListState = ReturnType<typeof useDataListScript>;
