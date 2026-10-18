import {
  useSymbolInfoBarScript,
  UseSymbolInfoBarScriptOptions,
} from "./symbolInfoBar.script";
import { SymbolInfoBar, SymbolInfoBarProps } from "./symbolInfoBar.ui";

export type SymbolInfoBarWidgetPros = UseSymbolInfoBarScriptOptions &
  Pick<SymbolInfoBarProps, "className" | "trailing" | "onSymbol">;

export const SymbolInfoBarWidget: React.FC<SymbolInfoBarWidgetPros> = (
  props,
) => {
  const { symbol, ...rest } = props;

  const state = useSymbolInfoBarScript({ symbol });
  return <SymbolInfoBar {...state} {...rest} />;
};
