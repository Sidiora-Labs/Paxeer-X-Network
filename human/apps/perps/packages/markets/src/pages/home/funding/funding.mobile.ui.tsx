import type { FC } from "react";
import { useTranslation } from "@paxeer-network/i18n";
import { Box, cn, TabPanel, Tabs } from "@paxeer-network/ui";
import { FundingComparisonWidget } from "../../../components/fundingComparison";
import { FundingOverviewWidget } from "../../../components/fundingOverview";
import { SearchInput } from "../../../components/searchInput";
import { FundingScriptReturn } from "./funding.script";

export const MobileFunding: FC<FundingScriptReturn> = (props) => {
  const { t } = useTranslation();

  const search = (
    <SearchInput
      classNames={{
        root: cn("oui-mb-2 oui-mt-5"),
      }}
    />
  );

  return (
    <Box
      intensity={900}
      p={3}
      mt={2}
      mb={5}
      r="xl"
      height="100%"
      className="oui-markets-funding-list"
    >
      <Tabs
        variant="contained"
        size="lg"
        value={props.activeTab}
        onValueChange={props.onTabChange as (value: string) => void}
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
          <>
            {search}
            <FundingOverviewWidget />
          </>
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
          <>
            {search}
            <FundingComparisonWidget />
          </>
        </TabPanel>
      </Tabs>
    </Box>
  );
};
