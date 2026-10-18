import React from "react";
import { cn, Flex, Text, TokenIcon, Divider } from "@paxeer-network/ui";
import { Decimal } from "@paxeer-network/utils";
import { SymbolDisplay } from "../symbolDisplay";

export interface MarketItemProps {
  symbol: string;
  tickerData: {
    "24h_close": number;
    change: number;
    quote_dp: number;
  };
  isActive: boolean;
  onSymbolClick: (symbol: string) => void;
}

const MarketItemComponent: React.FC<MarketItemProps> = (props) => {
  const { symbol, tickerData, isActive, onSymbolClick } = props;

  const handleClick = React.useCallback(() => {
    onSymbolClick(symbol);
  }, [symbol, onSymbolClick]);

  return (
    <Flex
      direction="row"
      className={cn(
        "oui-me-3 oui-h-[18px] oui-shrink-0 oui-cursor-pointer oui-items-center oui-rounded",
        "oui-transition-all oui-duration-200",
      )}
      onClick={handleClick}
    >
      {/* Symbol */}
      <Flex gapX={1} itemAlign="center" className="oui-me-[6px]">
        <TokenIcon symbol={symbol} className="oui-size-[18px]" />
        <SymbolDisplay
          formatString="base"
          size="2xs"
          className="oui-text-base-contrast-80"
        >
          {symbol}
        </SymbolDisplay>
      </Flex>

      {/* Price */}
      <Flex gapX={1} className="oui-me-[6px]">
        <Text.numeral
          dp={tickerData.quote_dp || 2}
          size="xs"
          className="oui-text-base-contrast-80"
        >
          {tickerData["24h_close"]}
        </Text.numeral>
      </Flex>

      {/* Change */}
      <Flex gapX={1} className="oui-me-3">
        <Text.numeral
          rule="percentages"
          coloring
          rm={Decimal.ROUND_DOWN}
          showIdentifier
          size="xs"
        >
          {tickerData.change}
        </Text.numeral>
      </Flex>

      {/* Divider */}
      <Divider direction="vertical" className="oui-h-[18px] oui-border-line" />
    </Flex>
  );
};

// Memoize the component to prevent unnecessary re-renders
export const MarketItem = React.memo<MarketItemProps>(
  MarketItemComponent,
  (prevProps, nextProps) => {
    return (
      prevProps.symbol === nextProps.symbol &&
      prevProps.isActive === nextProps.isActive &&
      prevProps.tickerData["24h_close"] === nextProps.tickerData["24h_close"] &&
      prevProps.tickerData.change === nextProps.tickerData.change &&
      prevProps.tickerData.quote_dp === nextProps.tickerData.quote_dp &&
      prevProps.onSymbolClick === nextProps.onSymbolClick
    );
  },
);

MarketItem.displayName = "MarketItem";
