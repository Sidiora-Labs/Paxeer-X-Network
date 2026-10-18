import { FC, useEffect } from "react";
import { useSymbolsInfo } from "@paxeer-network/hooks";
import { useTranslation } from "@paxeer-network/i18n";
import { toast, useScreen } from "@paxeer-network/ui";
import {
  InjectableDesktopLayout,
  InjectableMobileLayout,
} from "./trading.injectable";
import type { TradingState } from "./trading.script";

export const Trading: FC<TradingState> = (props) => {
  const { isMobile } = useScreen();
  const { t } = useTranslation();
  // const symbolsInfo = useSymbolsInfo();
  // const symbol = props.symbol;

  // // listen to symbol trading status
  // // if the symbol is delisting, show a toast
  // useEffect(() => {
  //   if (!symbol || !symbolsInfo[symbol]) return;
  //   const status = symbolsInfo[symbol]("status");
  //   if (status === "DELISTING") {
  //     const displaySymbol =
  //       symbolsInfo[symbol]("display_symbol_name") ??
  //       symbolsInfo[symbol]("name") ??
  //       symbol;
  //     toast(t("trading.symbolDelisting", { symbol: displaySymbol }), {
  //       id: `symbol-delisting-${symbol}`,
  //     });
  //   }
  // }, [symbol, symbolsInfo, t]);

  if (isMobile) {
    return <InjectableMobileLayout {...props} />;
  }

  return (
    <InjectableDesktopLayout
      className="oui-h-[calc(100vh_-_48px_-_29px)] oui-bg-base-10"
      {...props}
    />
  );
};
