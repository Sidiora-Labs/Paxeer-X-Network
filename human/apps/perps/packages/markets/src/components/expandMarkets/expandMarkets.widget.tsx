import { MarketsProvider, MarketsProviderProps } from "../marketsProvider";
import { useMarketCategories } from "../shared/hooks/useMarketCategories";
import {
  useExpandMarketsScript,
  ExpandMarketsScriptOptions,
} from "./expandMarkets.script";
import { ExpandMarkets } from "./expandMarkets.ui";

export type ExpandMarketsWidgetProps = MarketsProviderProps &
  ExpandMarketsScriptOptions;

export const ExpandMarketsWidget: React.FC<ExpandMarketsWidgetProps> = (
  props,
) => {
  const state = useExpandMarketsScript({
    activeTab: props.activeTab,
    onTabChange: props.onTabChange,
  });
  const tabs = useMarketCategories("expandMarkets");

  return (
    <MarketsProvider
      symbol={props.symbol}
      onSymbolChange={props.onSymbolChange}
      tabs={tabs}
    >
      <ExpandMarkets {...state} />
    </MarketsProvider>
  );
};
