import { useBadgeBySymbol } from "@paxeer-network/hooks";
import { API } from "@paxeer-network/types";
import { cn, Flex, SymbolBadgeTextProps, Text } from "@paxeer-network/ui";

export interface PositionSymbolCellProps {
  symbol: string;
  onSymbolChange?: (symbol: API.Symbol) => void;
}

export const PositionSymbolCell = (
  props: PositionSymbolCellProps &
    SymbolBadgeTextProps & { formatString?: string },
) => {
  const { symbol, onSymbolChange, className, formatString, ...rest } = props;
  const { brokerId, brokerName, brokerNameRaw } = useBadgeBySymbol(symbol);

  return (
    <Text.symbolBadge
      {...rest}
      className={cn(className, "oui-cursor-pointer")}
      badge={brokerName ?? brokerId ?? undefined}
      fullName={brokerNameRaw}
      onClick={(e) => {
        onSymbolChange?.({ symbol } as API.Symbol);
        e.stopPropagation();
        e.preventDefault();
      }}
    >
      {symbol}
    </Text.symbolBadge>
  );
};
