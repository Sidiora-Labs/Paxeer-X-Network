import { usePositionStream } from "@paxeer-network/hooks";
import { useDataTap } from "@paxeer-network/react-app";
import { useTradingLocalStorage } from "../../../hooks";

export const usePositionHeaderScript = (inputs: {
  pnlNotionalDecimalPrecision?: number;
  setPnlNotionalDecimalPrecision: (value: number) => void;
  unPnlPriceBasis: "markPrice" | "lastPrice";
  setUnPnlPriceBasic: (value: string) => void;
  symbol?: string;
}) => {
  const {
    pnlNotionalDecimalPrecision,
    setPnlNotionalDecimalPrecision,
    unPnlPriceBasis,
    setUnPnlPriceBasic,
    symbol,
  } = inputs;
  const calcMode = unPnlPriceBasis;

  const [data] = usePositionStream(symbol, {
    calcMode,
  });
  const aggregated = useDataTap(data.aggregated);

  const unrealPnL = aggregated?.total_unreal_pnl;
  const unrealPnlROI = aggregated?.unrealPnlROI;
  const notional = aggregated?.notional;
  const { showAllSymbol, setShowAllSymbol } = useTradingLocalStorage();

  return {
    pnlNotionalDecimalPrecision,
    unrealPnL,
    unrealPnlROI,
    notional,
    showAllSymbol,
    setShowAllSymbol,
    symbol,
    setPnlNotionalDecimalPrecision,
    unPnlPriceBasis,
    setUnPnlPriceBasic,
  };
};

export type PositionHeaderState = ReturnType<typeof usePositionHeaderScript>;
