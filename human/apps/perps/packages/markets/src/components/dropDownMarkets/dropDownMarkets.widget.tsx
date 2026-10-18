import React, { PropsWithChildren } from "react";
import { MarketsProvider, MarketsProviderProps } from "../marketsProvider";
import { useMarketCategories } from "../shared/hooks/useMarketCategories";
import { useDropDownMarketsScript } from "./dropDownMarkets.script";
import { DropDownMarkets, DropDownMarketsProps } from "./dropDownMarkets.ui";

export type DropDownMarketsWidgetProps = MarketsProviderProps &
  Pick<DropDownMarketsProps, "contentClassName">;

export const DropDownMarketsWidget: React.FC<
  PropsWithChildren<DropDownMarketsWidgetProps>
> = (props) => {
  const state = useDropDownMarketsScript();
  const tabs = useMarketCategories("dropDownMarkets");

  return (
    <MarketsProvider
      symbol={props.symbol}
      onSymbolChange={props.onSymbolChange}
      tabs={tabs}
    >
      <DropDownMarkets {...state} contentClassName={props.contentClassName}>
        {props.children}
      </DropDownMarkets>
    </MarketsProvider>
  );
};
