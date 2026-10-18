import { FC } from "react";
import { useTranslation } from "@paxeer-network/i18n";
import { Tabs, TabPanel, Flex } from "@paxeer-network/ui";
import { CommissionTable } from "./commissionTable";
import { RefereesTable } from "./refereesTable";
import { ReferralCodesTable } from "./referralCodesTable";
import {
  ReferrerTableScriptReturns,
  ReferrerTableTab,
} from "./referrerTable.script";

export const ReferrerTableUI: FC<ReferrerTableScriptReturns> = (props) => {
  const { t } = useTranslation();

  return (
    <Flex
      direction="column"
      className="oui-affiliate-referrerTable oui-rounded-2xl oui-bg-base-9 [&_td]:oui-font-medium [&_td]:oui-tracking-[0.03em] [&_th]:oui-font-semibold"
      px={3}
      pt={4}
      gap={4}
      width="100%"
    >
      <Tabs
        value={props.activeTab}
        onValueChange={(value) => props.setActiveTab(value as ReferrerTableTab)}
        className="oui-referrerTable-tabs oui-w-full oui-text-base"
        variant="text"
        classNames={{
          tabsListContainer: "oui-px-3",
          trigger: "oui-pb-[9px] oui-tracking-[0.01em]",
          scrollIndicator:
            "[&_.oui-scroll-indicator-leading]:oui-hidden [&_.oui-scroll-indicator-tailing]:oui-hidden",
        }}
        showScrollIndicator
      >
        <TabPanel value="commission" title={t("affiliate.commission")}>
          <CommissionTable enabled={props.activeTab === "commission"} />
        </TabPanel>
        <TabPanel value="referees" title={t("affiliate.referees")}>
          <RefereesTable enabled={props.activeTab === "referees"} />
        </TabPanel>
        <TabPanel value="codes" title={t("affiliate.referralCodes")}>
          <ReferralCodesTable enabled={props.activeTab === "codes"} />
        </TabPanel>
      </Tabs>
    </Flex>
  );
};
