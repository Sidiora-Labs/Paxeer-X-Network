import React from "react";
import { SharePnLConfig } from "@paxeer-network/ui-share";
import { useShareButtonScript } from "./shareButton.script";
import { ShareButton } from "./shareButton.ui";

export const ShareButtonWidget: React.FC<{
  order: any;
  sharePnLConfig?: SharePnLConfig;
  modalId: string;
  iconSize?: number;
}> = (props) => {
  const state = useShareButtonScript(props);
  return <ShareButton {...state} />;
};
