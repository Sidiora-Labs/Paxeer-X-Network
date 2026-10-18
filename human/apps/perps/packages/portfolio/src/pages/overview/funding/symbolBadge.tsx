import { type FC } from "react";
import { useBadgeBySymbol } from "@paxeer-network/hooks";
import { SymbolBadge as UISymbolBadge } from "@paxeer-network/ui";

export const SymbolBadge: FC<{ symbol: string }> = (props) => {
  const { brokerId, brokerName, brokerNameRaw } = useBadgeBySymbol(
    props.symbol,
  );
  const badge = brokerName ?? brokerId ?? undefined;
  return <UISymbolBadge badge={badge} fullName={brokerNameRaw} />;
};
