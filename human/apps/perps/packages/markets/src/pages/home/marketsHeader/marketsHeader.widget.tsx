import { FC } from "react";
import { useScreen } from "@paxeer-network/ui";
import { MobileMarketsHeader } from "./marketsHeader.mobile.ui";
import { useMarketsHeaderScript } from "./marketsHeader.script";
import { MarketsHeader } from "./marketsHeader.ui";

type MarketsHeaderWidgetProps = {
  className?: string;
};

export const MarketsHeaderWidget: FC<MarketsHeaderWidgetProps> = (props) => {
  const state = useMarketsHeaderScript();
  const { isMobile } = useScreen();
  return isMobile ? (
    <MobileMarketsHeader className={props.className} {...state} />
  ) : (
    <MarketsHeader className={props.className} {...state} />
  );
};
