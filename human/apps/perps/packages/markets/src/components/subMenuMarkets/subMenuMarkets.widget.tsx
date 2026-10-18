import React from "react";
import { MarketsProvider, type MarketsProviderProps } from "../marketsProvider";
import { useMarketCategories } from "../shared/hooks/useMarketCategories";
import {
  useSubMenuMarketsScript,
  type SubMenuMarketsScriptOptions,
} from "./subMenuMarkets.script";
import { SubMenuMarkets } from "./subMenuMarkets.ui";
import type { SubMenuMarketsProps } from "./subMenuMarkets.ui";

export type SubMenuMarketsWidgetProps = MarketsProviderProps &
  SubMenuMarketsScriptOptions &
  Pick<SubMenuMarketsProps, "className">;

export const SubMenuMarketsWidget: React.FC<SubMenuMarketsWidgetProps> = (
  props,
) => {
  const state = useSubMenuMarketsScript({
    activeTab: props.activeTab,
    onTabChange: props.onTabChange,
  });
  const tabs = useMarketCategories("subMenuMarkets");

  return (
    <MarketsProvider
      symbol={props.symbol}
      onSymbolChange={props.onSymbolChange}
      tabs={tabs}
    >
      <SubMenuMarkets
        activeTab={state.activeTab}
        onTabChange={state.onTabChange}
        tabSort={state.tabSort}
        onTabSort={state.onTabSort}
        className={props.className}
      />
    </MarketsProvider>
  );
};
