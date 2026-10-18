import React from "react";
import { injectable } from "@paxeer-network/ui";
import { TradingPageProvider } from "../../provider/tradingPageProvider";
import { TradingPageProps } from "../../types/types";
import { TradingWidget } from "./trading.widget";

/** Inner page: receives props and renders provider + widget. Layout strategy from props (host or plugin). */
const TradingPageInner: React.FC<TradingPageProps> = (props) => {
  return (
    <TradingPageProvider
      symbol={props.symbol}
      tradingViewConfig={props.tradingViewConfig}
      onSymbolChange={props.onSymbolChange}
      disableFeatures={props.disableFeatures}
      overrideFeatures={props.overrideFeatures}
      referral={props.referral}
      tradingRewards={props.tradingRewards}
      bottomSheetLeading={props.bottomSheetLeading}
      sharePnLConfig={props.sharePnLConfig}
      layoutStrategy={props.layoutStrategy}
      getInitialLayout={props.getInitialLayout}
    >
      <TradingWidget />
    </TradingPageProvider>
  );
};

/** Injectable so layout plugins can intercept and inject layoutStrategy/getInitialLayout when host does not pass them */
export const TradingPage = injectable(TradingPageInner, "Trading.Page");
