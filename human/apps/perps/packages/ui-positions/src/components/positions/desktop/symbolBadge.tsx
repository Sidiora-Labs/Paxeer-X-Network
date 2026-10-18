import { useBadgeBySymbol } from "@paxeer-network/hooks";
import { SymbolBadge as UISymbolBadge } from "@paxeer-network/ui";

export const SymbolBadge = ({ symbol }: { symbol: string }) => {
  const { brokerId, brokerName, brokerNameRaw } = useBadgeBySymbol(symbol);
  const badge = brokerName ?? brokerId ?? undefined;
  return <UISymbolBadge badge={badge} fullName={brokerNameRaw} />;
};
