import React from "react";
import { useTranslation } from "@paxeer-network/i18n";
import { Box, cn, TabPanel, Tabs } from "@paxeer-network/ui";
import { useTradingPageContext } from "../../../provider/tradingPageContext";
import { TopTabState, TopTabType } from "./topTab.script";

const LazyTradingviewWidget = React.lazy(() =>
  import("../tradingview/tradingview.widget").then((mod) => {
    return { default: mod.TradingviewWidget };
  }),
);

const LazyMWebLastTrades = React.lazy(() =>
  import("../lastTrades/lastTrades.widget").then((mod) => {
    return { default: mod.MWebLastTrades };
  }),
);

const LazyTradeDataWidget = React.lazy(() =>
  import("../tradeData").then((mod) => {
    return { default: mod.TradeDataWidget };
  }),
);

export const TopTab: React.FC<TopTabState & { className?: string }> = (
  props,
) => {
  const { t } = useTranslation();
  const { tradingViewConfig } = useTradingPageContext();

  return (
    <Tabs
      variant="contained"
      value={props.tab}
      contentVisible={props.visible}
      onValueChange={(e) => {
        props.setTab(e as TopTabType);
        props.setVisible(true);
      }}
      className={cn("oui-trading-topTab", props.className)}
      classNames={{
        tabsList: "oui-p-2",
        tabsContent: "oui-min-h-[176px] oui-max-h-[396px]",
      }}
      style={{
        marginBottom: props.tab === TopTabType.chart ? "8px" : 0,
      }}
      trailing={
        <button className="oui-px-5" onClick={props.toggleContentVisible}>
          <ChevronIcon
            className={props.visible ? "oui-rotate-0" : "oui-rotate-180"}
          />
        </button>
      }
    >
      <TabPanel title={t("trading.tabs.chart")} value={TopTabType.chart}>
        <React.Suspense fallback={null}>
          <LazyTradingviewWidget
            symbol={props.symbol}
            tradingViewConfig={tradingViewConfig}
          />
        </React.Suspense>
      </TabPanel>
      <TabPanel title={t("trading.tabs.trades")} value={TopTabType.trades}>
        <React.Suspense fallback={null}>
          <LazyMWebLastTrades symbol={props.symbol} />
        </React.Suspense>
      </TabPanel>
      <TabPanel title={t("trading.tabs.data")} value={TopTabType.data}>
        <Box px={3}>
          <React.Suspense fallback={null}>
            <LazyTradeDataWidget symbol={props.symbol} />
          </React.Suspense>
        </Box>
      </TabPanel>
    </Tabs>
  );
};

const ChevronIcon: React.FC<{ className?: string }> = (props) => {
  return (
    <svg
      width="12"
      height="12"
      viewBox="0 0 12 12"
      fill="currentColor"
      xmlns="http://www.w3.org/2000/svg"
      className={cn(
        "oui-fill-base-contrast-54 hover:oui-fill-base-contrast-80",
        props.className,
      )}
    >
      <path
        d="M5.721 4.585 2.726 6.578a.51.51 0 0 0-.14.7.51.51 0 0 0 .702.14l2.714-1.806 2.715 1.806c.23.153.549.089.702-.14a.51.51 0 0 0-.14-.7L6.283 4.585a.51.51 0 0 0-.562 0"
        // fill="url(#a)"
      />
      <defs>
        {/* <linearGradient
          id="a"
          x1="9.502"
          y1="6.001"
          x2="2.502"
          y2="6.001"
          gradientUnits="userSpaceOnUse"
        >
          <stop stopColor="rgb(var(--oui-gradient-brand-end))" />
          <stop offset="1" stopColor="rgb(var(--oui-gradient-brand-start))" />
        </linearGradient> */}
      </defs>
    </svg>
  );
};
