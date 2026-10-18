import { FC } from "react";
import { MarketsProviderProps } from "../marketsProvider";
import { SymbolInfoBarFullInjectable } from "./symbolInfoBarFull.injectable";
import {
  useSymbolInfoBarFullScript,
  UseSymbolInfoBarFullScriptOptions,
} from "./symbolInfoBarFull.script";
import { SymbolInfoBarFullProps } from "./symbolInfoBarFull.ui";

export type SymbolInfoBarFullWidgetPros = UseSymbolInfoBarFullScriptOptions &
  Pick<SymbolInfoBarFullProps, "className" | "trailing"> &
  Pick<MarketsProviderProps, "onSymbolChange"> & {
    closeCountdown?: () => void;
    showCountdown?: boolean;
  };

export const SymbolInfoBarFullWidget: FC<SymbolInfoBarFullWidgetPros> = (
  props,
) => {
  const { symbol, ...rest } = props;

  const state = useSymbolInfoBarFullScript({ symbol });
  return <SymbolInfoBarFullInjectable {...state} {...rest} />;
};
