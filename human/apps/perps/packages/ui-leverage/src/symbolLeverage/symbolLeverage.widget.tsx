import { FC } from "react";
import {
  SymbolLeverageScriptOptions,
  useSymbolLeverageScript,
} from "./symbolLeverage.script";
import { SymbolLeverage } from "./symbolLeverage.ui";

export type SymbolLeverageWidgetProps = {
  close?: () => void;
} & SymbolLeverageScriptOptions;

export const SymbolLeverageWidget: FC<SymbolLeverageWidgetProps> = (props) => {
  const state = useSymbolLeverageScript(props);
  return <SymbolLeverage {...state} />;
};
