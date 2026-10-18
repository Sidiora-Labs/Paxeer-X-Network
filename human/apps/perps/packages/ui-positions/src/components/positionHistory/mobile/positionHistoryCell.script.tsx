import { API } from "@paxeer-network/types";
import { SharePnLConfig } from "@paxeer-network/ui-share";
import { useSymbolContext } from "../../../provider/symbolContext";
import { PositionHistoryExt } from "../positionHistory.script";

export const usePositionHistoryCellScript = (props: {
  item: PositionHistoryExt;
  onSymbolChange?: (symbol: API.Symbol) => void;
  index: number;
  classNames?: {
    root?: string;
  };
  sharePnLConfig?: SharePnLConfig;
}) => {
  const symbolInfo = useSymbolContext();

  return {
    ...props,
    ...symbolInfo,
  };
};

export type PositionHistoryCellState = ReturnType<
  typeof usePositionHistoryCellScript
>;
