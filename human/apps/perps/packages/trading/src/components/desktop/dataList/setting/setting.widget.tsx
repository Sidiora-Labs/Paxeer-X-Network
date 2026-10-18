import React from "react";
import { useSettingScript } from "./setting.script";
import { Setting } from "./setting.ui";

export type SettingWidgetProps = {
  pnlNotionalDecimalPrecision: any;
  setPnlNotionalDecimalPrecision: (value: number) => void;
  unPnlPriceBasis: "markPrice" | "lastPrice";
  setUnPnlPriceBasic: (value: string) => void;
  hideOtherSymbols: any;
  setHideOtherSymbols: (value: boolean) => void;
};

export const SettingWidget: React.FC<SettingWidgetProps> = (props) => {
  const state = useSettingScript(props);
  return <Setting {...state} />;
};
