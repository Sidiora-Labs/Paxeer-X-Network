import { type FC } from "react";
import { useBadgeBySymbol } from "@paxeer-network/hooks";
import { SymbolBadge as UISymbolBadge } from "@paxeer-network/ui";

export type SymbolBadgeProps = {
  symbol: string;
  className?: string;
  size?: "xs" | "sm" | "md" | "lg";
};

export const SymbolBadge: FC<SymbolBadgeProps> = (props) => {
  const { brokerId, brokerName, brokerNameRaw } = useBadgeBySymbol(
    props.symbol,
  );
  const badge = brokerName ?? brokerId ?? undefined;
  return <UISymbolBadge badge={badge} fullName={brokerNameRaw} />;
};
