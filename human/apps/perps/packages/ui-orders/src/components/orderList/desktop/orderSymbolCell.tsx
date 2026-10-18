import { useBadgeBySymbol } from "@paxeer-network/hooks";
import { API } from "@paxeer-network/types";
import { Text } from "@paxeer-network/ui";

export interface OrderSymbolCellProps {
  symbol: string;
  onSymbolChange?: (symbol: API.Symbol) => void;
}

export const OrderSymbolCell = (props: OrderSymbolCellProps) => {
  const { symbol, onSymbolChange } = props;
  const { brokerId, brokerName, brokerNameRaw } = useBadgeBySymbol(symbol);

  return (
    <Text.symbolBadge
      className="oui-cursor-pointer"
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
