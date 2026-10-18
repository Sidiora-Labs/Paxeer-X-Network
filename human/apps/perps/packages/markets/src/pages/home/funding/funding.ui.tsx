import React from "react";
import { useTranslation } from "@paxeer-network/i18n";
import { Box, TabPanel, Tabs } from "@paxeer-network/ui";
import type { FundingScriptReturn } from "./funding.script";

const LazySearchInput = React.lazy(() =>
  import("../../../components/searchInput").then((mod) => {
    return { default: mod.SearchInput };
  }),
);

const LazyFundingOverviewWidget = React.lazy(() =>
  import("../../../components/fundingOverview").then((mod) => {
    return { default: mod.FundingOverviewWidget };
  }),
);

const LazyFundingComparisonWidget = React.lazy(() =>
  import("../../../components/fundingComparison").then((mod) => {
    return { default: mod.FundingComparisonWidget };
  }),
);

export const Funding: React.FC<FundingScriptReturn> = (props) => {
  const { t } = useTranslation();

  return (
    <Box
      intensity={900}
      p={6}
      mt={4}
      r="2xl"
      className="oui-markets-funding-list"
    >
      <Tabs
        variant="contained"
        size="lg"
        value={props.activeTab}
        onValueChange={props.onTabChange as (value: string) => void}
        trailing={
          <React.Suspense fallback={null}>
            <LazySearchInput classNames={{ root: "oui-my-1 oui-w-[240px]" }} />
          </React.Suspense>
        }
      >
        <TabPanel
          classNames={{
            trigger: "oui-tabs-funding-overview-trigger",
            content: "oui-tabs-funding-overview-content",
          }}
          title={t("common.overview")}
          value="overview"
          testid="oui-testid-funding-overview-tab"
        >
          <React.Suspense fallback={null}>
            <LazyFundingOverviewWidget />
          </React.Suspense>
        </TabPanel>
        <TabPanel
          classNames={{
            trigger: "oui-tabs-funding-comparison-trigger",
            content: "oui-tabs-funding-comparison-content",
          }}
          title={t("markets.funding.comparison")}
          value="comparison"
          testid="oui-testid-funding-comparison-tab"
        >
          <React.Suspense fallback={null}>
            <LazyFundingComparisonWidget />
          </React.Suspense>
        </TabPanel>
      </Tabs>
    </Box>
  );
};
