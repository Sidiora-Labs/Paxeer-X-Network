import React from "react";
import { Box, cn, Flex, Marquee } from "@paxeer-network/ui";
import { useMarketsContext } from "../marketsProvider";
import type { HorizontalMarketsScriptReturn } from "./horizontalMarkets.script";
import { MarketItem } from "./marketItem.ui";
import { MarketTypeFilter } from "./marketTypeFilter.ui";
import type { DropdownPos } from "./marketTypeFilter.ui";

export type HorizontalMarketsProps = HorizontalMarketsScriptReturn & {
  className?: string;
  dropdownPos?: DropdownPos;
};

export const HorizontalMarkets = React.memo<HorizontalMarketsProps>((props) => {
  const {
    symbols,
    tickerData,
    currentSymbol,
    onSymbolClick,
    selectedMarketType,
    onMarketTypeChange,
    className,
    dropdownPos,
  } = props;
  const { tabs } = useMarketsContext();

  // Memoize the render function to prevent unnecessary re-renders
  const renderMarketItem = React.useCallback(
    (symbol: string, index: number) => {
      const data = tickerData[symbol];
      const isActive = currentSymbol === symbol;

      if (!data) {
        return null;
      }

      return (
        <MarketItem
          key={symbol}
          symbol={symbol}
          tickerData={data}
          isActive={isActive}
          onSymbolClick={onSymbolClick}
        />
      );
    },
    [tickerData, currentSymbol, onSymbolClick],
  );

  const carouselOptions = React.useMemo(
    () => ({
      loop: true,
      align: "start" as const,
      axis: "x" as const,
    }),
    [],
  );

  const autoScrollOptions = React.useMemo(
    () => ({
      speed: 1,
      direction: "forward" as const,
      stopOnMouseEnter: true,
    }),
    [],
  );

  return (
    <Box
      className={cn(
        "oui-horizontal-markets",
        "oui-bg-base-9 oui-rounded-[12px]",
        "oui-w-full oui-px-3 oui-py-2",
        className,
      )}
    >
      <Flex
        direction="row"
        gapX={3}
        itemAlign="center"
        className="oui-size-full"
      >
        {/* Filter Button */}
        <MarketTypeFilter
          selectedMarketType={selectedMarketType}
          onMarketTypeChange={onMarketTypeChange}
          position={dropdownPos}
          tabs={tabs}
        />

        {/* Markets List */}
        <Marquee
          data={symbols}
          renderItem={renderMarketItem}
          carouselOptions={carouselOptions}
          autoScrollOptions={autoScrollOptions}
          className="oui-h-full"
        />
      </Flex>
    </Box>
  );
});

if (process.env.NODE_ENV !== "production") {
  HorizontalMarkets.displayName = "HorizontalMarkets";
}
