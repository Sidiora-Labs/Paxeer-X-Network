import { FC } from "react";
import { Flex, Text } from "@paxeer-network/ui";
import { AsTraderWidget } from "../asATrader";
import { AsAnAffiliateWidget } from "../asAnAffilate";
import { CardReturns } from "./card.script";

export const Card: FC<CardReturns> = (props) => {
  if (typeof props.overwrite === "function") {
    return props.overwrite?.(props.state);
  }
  return (
    <Flex
      id="oui-affiliate-home-card"
      className="oui-flex oui-w-full oui-flex-col oui-items-stretch oui-gap-6 xl:oui-flex-row xl:oui-gap-[36px]"
    >
      <AsAnAffiliateWidget />
      <AsTraderWidget />
    </Flex>
  );
};
