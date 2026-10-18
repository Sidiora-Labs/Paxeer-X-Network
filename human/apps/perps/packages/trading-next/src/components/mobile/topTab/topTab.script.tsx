import { useState } from "react";
import { useTradingPageContext } from "../../../provider/tradingPageContext";

export enum TopTabType {
  chart = "chart",
  trades = "trades",
  data = "data",
}

export const useTopTabScript = () => {
  const [tab, setTab] = useState<TopTabType>(TopTabType.chart);
  const { symbol } = useTradingPageContext();
  const [visible, setVisible] = useState(true);

  const toggleContentVisible = () => {
    setVisible((e) => !e);
  };

  return {
    tab,
    setTab,
    symbol,
    toggleContentVisible,
    setVisible,
    visible,
  };
};

export type TopTabState = ReturnType<typeof useTopTabScript>;
