import { type FC } from "react";
import { useBadgeBySymbol } from "@paxeer-network/hooks";
import { Flex, SymbolBadge as UISymbolBadge } from "@paxeer-network/ui";

export const BrokerIdBadge: FC<{ symbol: string; className?: string }> = (
  props,
) => {
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

export const SymbolBadge = BrokerIdBadge;
