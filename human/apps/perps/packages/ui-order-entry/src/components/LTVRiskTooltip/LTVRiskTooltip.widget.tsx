import React from "react";
import { MarginMode } from "@paxeer-network/types";
import { useLTVTooltipScript } from "./LTVRiskTooltip.script";
import { LTVRiskTooltipUI } from "./LTVRiskTooltip.ui";

export const LTVRiskTooltipWidget: React.FC<{ marginMode?: MarginMode }> = ({
  marginMode,
}) => {
  const state = useLTVTooltipScript(marginMode);
  return <LTVRiskTooltipUI {...state} />;
};
