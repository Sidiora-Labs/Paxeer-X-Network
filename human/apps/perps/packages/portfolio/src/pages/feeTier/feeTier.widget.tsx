import React from "react";
import { useFeeTierScript, type UseFeeTierScriptOptions } from ".";
import { FeeTier } from "./feeTier.ui";

export type FeeTierWidgetProps = UseFeeTierScriptOptions;

export const FeeTierWidget: React.FC<FeeTierWidgetProps> = (props) => {
  const state = useFeeTierScript(props);
  return <FeeTier {...state} />;
};
