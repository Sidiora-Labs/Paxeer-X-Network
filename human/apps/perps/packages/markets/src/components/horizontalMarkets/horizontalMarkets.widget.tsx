import React from "react";
import { MarketsProvider, MarketsProviderProps } from "../marketsProvider";
import { useMarketCategories } from "../shared/hooks/useMarketCategories";
import { useHorizontalMarketsScript } from "./horizontalMarkets.script";
import type { MarketType } from "./horizontalMarkets.script";
import type { HorizontalMarketsProps } from "./horizontalMarkets.ui";
import type { DropdownPos } from "./marketTypeFilter.ui";

const LazyHorizontalMarkets = React.lazy(() =>
  import("./horizontalMarkets.ui").then((mod) => {
    return { default: mod.HorizontalMarkets };
  }),
);

export type HorizontalMarketsWidgetProps = MarketsProviderProps &
  Partial<Pick<HorizontalMarketsProps, "symbols" | "className">> & {
    maxItems?: number;
    defaultMarketType?: MarketType;
    dropdownPos?: DropdownPos;
  };

const HorizontalMarketsInner: React.FC<
  Pick<
    HorizontalMarketsWidgetProps,
    "symbols" | "maxItems" | "defaultMarketType" | "className" | "dropdownPos"
  >
> = (props) => {
  const { symbols, maxItems, className, defaultMarketType, dropdownPos } =
    props;
  const state = useHorizontalMarketsScript({
    symbols,
    maxItems,
    defaultMarketType,
  });
  return (
    <React.Suspense fallback={null}>
      <LazyHorizontalMarkets
        {...state}
        className={className}
        dropdownPos={dropdownPos}
      />
    </React.Suspense>
  );
};

export const HorizontalMarketsWidget: React.FC<HorizontalMarketsWidgetProps> = (
  props,
) => {
  const {
    symbols,
    maxItems,
    className,
    defaultMarketType,
    dropdownPos,
    ...providerProps
  } = props;

  const tabs = useMarketCategories("horizontalMarkets");

  return (
    <MarketsProvider {...providerProps} tabs={tabs}>
      <HorizontalMarketsInner
        symbols={symbols}
        maxItems={maxItems}
        className={className}
        defaultMarketType={defaultMarketType}
        dropdownPos={dropdownPos}
      />
    </MarketsProvider>
  );
};
