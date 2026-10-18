import React from "react";
import { MarketsProvider, MarketsProviderProps } from "../marketsProvider";
import { useMarketCategories } from "../shared/hooks/useMarketCategories";
import { useMarketsSheetScript } from "./marketsSheet.script";
import { MarketsSheet, MarketsSheetProps } from "./marketsSheet.ui";

export type MarketsSheetWidgetProps = MarketsProviderProps &
  Partial<Pick<MarketsSheetProps, "className">>;

export const MarketsSheetWidget: React.FC<MarketsSheetWidgetProps> = (
  props,
) => {
  const state = useMarketsSheetScript();
  const tabs = useMarketCategories("marketsSheet");

  return (
    <MarketsProvider
      symbol={props.symbol}
      onSymbolChange={props.onSymbolChange}
      tabs={tabs}
    >
      <MarketsSheet {...state} className={props.className} />
    </MarketsProvider>
  );
};
