import React, { FC, useState } from "react";
import { useTranslation } from "@paxeer-network/i18n";
import type { RouterAdapter } from "@paxeer-network/types";
import { Box, cn, TabPanel, Tabs, useScreen } from "@paxeer-network/ui";
import { LeftNavProps, LeftNavUI } from "@paxeer-network/ui-scaffold";
import {
  MarketsProvider,
  type MarketsProviderProps,
} from "../../components/marketsProvider";
import { useMarketCategories } from "../../components/shared/hooks/useMarketCategories";
import { MarketsPageTab } from "../../type";

const LazyMarketsHeaderWidget = React.lazy(() =>
  import("./marketsHeader/marketsHeader.widget").then((mod) => {
    return { default: mod.MarketsHeaderWidget };
  }),
);

const LazyMarketsDataListWidget = React.lazy(() =>
  import("./marketsDataList").then((mod) => {
    return { default: mod.MarketsDataListWidget };
  }),
);

const LazyFundingWidget = React.lazy(() =>
  import("./funding/funding.widget").then((mod) => {
    return { default: mod.FundingWidget };
  }),
);

export type MarketsHomePageProps = MarketsProviderProps & {
  className?: string;
};

export const MarketsHomePage: FC<MarketsHomePageProps> = (props) => {
  const { isMobile } = useScreen();

  const [activeTab, setActiveTab] = useState<MarketsPageTab>(
    MarketsPageTab.Markets,
  );
  const tabs = useMarketCategories("marketsDataList");

  return (
    <MarketsProvider
      symbol={props.symbol}
      onSymbolChange={props.onSymbolChange}
      navProps={props.navProps}
      comparisonProps={props.comparisonProps}
      tabs={tabs}
    >
      <div
        id="oui-markets-home-page"
        className={cn("oui-font-semibold", props.className)}
      >
        {isMobile ? (
          <MarketsMobileContent
            activeTab={activeTab}
            onTabChange={setActiveTab}
            navProps={props.navProps}
          />
        ) : (
          <MarketsDesktopContent
            activeTab={activeTab}
            onTabChange={setActiveTab}
          />
        )}
      </div>
    </MarketsProvider>
  );
};

type MarketsContentProps = {
  activeTab: MarketsPageTab;
  onTabChange: (value: MarketsPageTab) => void;
  // only for mobile
  navProps?: {
    logo?: {
      src: string;
      alt: string;
    };
    routerAdapter?: RouterAdapter;
    leftNav?: LeftNavProps;
  };
};

const MarketsDesktopContent: React.FC<MarketsContentProps> = (props) => {
  const { t } = useTranslation();

  return (
    <Box p={6}>
      <Tabs
        size="xl"
        value={props.activeTab}
        onValueChange={props.onTabChange as (value: string) => void}
      >
        <TabPanel
          classNames={{
            trigger: "oui-tabs-markets-trigger",
            content: "oui-tabs-markets-content",
          }}
          title={t("common.markets")}
          value={MarketsPageTab.Markets}
          testid="oui-testid-markets-tab"
        >
          <React.Suspense fallback={null}>
            <LazyMarketsHeaderWidget className="oui-mt-4" />
          </React.Suspense>
          <React.Suspense fallback={null}>
            <LazyMarketsDataListWidget />
          </React.Suspense>
        </TabPanel>
        <TabPanel
          classNames={{
            trigger: "oui-tabs-funding-trigger",
            content: "oui-tabs-funding-content",
          }}
          title={t("common.funding")}
          value={MarketsPageTab.Funding}
          testid="oui-testid-funding-tab"
        >
          <React.Suspense fallback={null}>
            <LazyFundingWidget />
          </React.Suspense>
        </TabPanel>
      </Tabs>
    </Box>
  );
};

const MarketsMobileContent: React.FC<MarketsContentProps> = (props) => {
  const { t } = useTranslation();

  return (
    <Tabs
      size="xl"
      value={props.activeTab}
      onValueChange={props.onTabChange as (value: string) => void}
      classNames={{
        tabsListContainer: "oui-border-0",
        tabsList: "oui-mx-6 oui-my-2",
        tabsContent: "oui-px-3",
      }}
      leading={
        props.navProps?.leftNav && (
          <LeftNavUI
            className="-oui-me-4 oui-ms-3"
            {...props?.navProps?.leftNav}
            logo={props?.navProps?.logo}
            routerAdapter={props?.navProps?.routerAdapter}
          />
        )
      }
    >
      <TabPanel
        title={t("common.markets")}
        value="markets"
        testid="oui-testid-markets-tab"
      >
        <React.Suspense fallback={null}>
          <LazyMarketsHeaderWidget className="oui-mt-2" />
        </React.Suspense>
        <React.Suspense fallback={null}>
          <LazyMarketsDataListWidget />
        </React.Suspense>
      </TabPanel>
      <TabPanel
        title={t("common.funding")}
        value="funding"
        testid="oui-testid-funding-tab"
      >
        <React.Suspense fallback={null}>
          <LazyFundingWidget />
        </React.Suspense>
      </TabPanel>
    </Tabs>
  );
};
