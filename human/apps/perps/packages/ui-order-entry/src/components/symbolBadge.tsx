import { type FC } from "react";
import { useBadgeBySymbol } from "@paxeer-network/hooks";
import { Flex, SymbolBadge as UISymbolBadge } from "@paxeer-network/ui";

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

  if (!badge) {
    return null;
  }

  return (
    <Flex as="span" display="inlineFlex" className={props.className}>
      <UISymbolBadge badge={badge} fullName={brokerNameRaw} />
    </Flex>
  );
};
