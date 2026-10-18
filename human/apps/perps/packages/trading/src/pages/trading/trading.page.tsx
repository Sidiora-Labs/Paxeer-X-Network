import React from "react";
import { TradingPageProvider } from "../../provider/tradingPageProvider";
import { TradingPageProps } from "../../types/types";
import { TradingWidget } from "./trading.widget";

export const TradingPage: React.FC<TradingPageProps> = (props) => {
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
    >
      <TradingWidget />
    </TradingPageProvider>
  );
};
