import { FC } from "react";
import { TopReturns } from "./top.script";
import { TitleWidget } from "../title";
import { SubtitleWidget } from "../subtitle";
import { Flex } from "@paxeer-network/ui";

export const Top: FC<TopReturns> = (props) => {
  if (props.overwriteTop !== undefined) {
    return props.overwriteTop?.(props.state);
  }
  return (
    <Flex id="oui-affiliate-home-top" direction="column" gap={6}>
      <TitleWidget />
      <SubtitleWidget />
    </Flex>
  );
};
