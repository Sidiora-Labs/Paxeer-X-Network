import { useTranslation } from "@paxeer-network/i18n";
import {
  Flex,
  Grid,
  TabsBase,
  TabsContent,
  TabsList,
  TabsTrigger,
  cn,
  Text,
} from "@paxeer-network/ui";
import { AffiliateIcon } from "../../../icons/affiliateIcon";
import { TraderIcon } from "../../../icons/traderIcon";
import { TabTypes, useReferralContext } from "../../../provider";
import { MultiLevelTraderPage } from "../trader/page";
import { CommissionChartWidget } from "./commissionChart";
import { MultiLevelReferralWidget } from "./multiLevelReferral";
import { ReferralInfoWidget } from "./referralInfo";
import { ReferrerTableWidget } from "./referrerTable/referrerTable.widget";
import { SummaryWidget } from "./summary";

export const MultiLevelAffiliatePage = () => {
  const { t } = useTranslation();

  const {
    multiLevelRebateInfo,
    refereeKickbackInfo,
    referralInfo,
    tab,
    setTab,
  } = useReferralContext();
  const showRoleTabs =
    (refereeKickbackInfo?.referee_kickback_rate ?? 0) > 0 ||
    (referralInfo?.referee_info?.total_referee_rebate ?? 0) > 0 ||
    !!referralInfo?.referee_info?.referer_code;
  const activeTab =
    tab === TabTypes.trader ? TabTypes.trader : TabTypes.affiliate;

  const affiliateContent = (
    <Flex width="100%" direction="column" className="oui-gap-8 md:oui-gap-10">
      <Grid
        width={"100%"}
        height={"100%"}
        gap={6}
        cols={1}
        rows={1}
        className="md:oui-grid-cols-2"
      >
        <div className="oui-multiLevel-affiliate-summary-container oui-order-2 md:oui-order-1">
          <SummaryWidget />
        </div>
        <div className="oui-order-1 md:oui-order-2">
          {multiLevelRebateInfo?.referral_code ? (
            <ReferralInfoWidget />
          ) : (
            <MultiLevelReferralWidget />
          )}
        </div>
      </Grid>

      <CommissionChartWidget />

      <div className="oui-multiLevel-affiliate-referrerTables-container">
        <ReferrerTableWidget />
      </div>
    </Flex>
  );

  return (
    <Flex
      className={cn(
        "oui-multiLevel-affiliate-page",
        "oui-h-lvw",
        "oui-font-semibold",
        "oui-pb-24 oui-pt-16",
      )}
      direction={"column"}
      gap={4}
    >
      <div className="oui-w-full oui-px-5 md:oui-mx-auto md:oui-max-w-[1040px]">
        <Flex
          width={"100%"}
          direction={"column"}
          className="oui-gap-8 md:oui-gap-10"
        >
          <Flex width={"100%"} direction={"column"} itemAlign={"start"} gap={4}>
            <Text className="oui-text-[32px] oui-leading-[40px] md:oui-text-6xl md:oui-leading-[72px]">
              {t("affiliate.referralRewards")}
            </Text>
            <Text size="sm" intensity={54}>
              {t("affiliate.referralRewards.description")}
            </Text>
          </Flex>

          {showRoleTabs ? (
            <TabsBase
              value={activeTab}
              onValueChange={(value) => setTab(value as TabTypes)}
              className="oui-w-full"
            >
              <TabsList
                variant="text"
                className="oui-flex oui-h-11 oui-w-full oui-items-end oui-justify-center oui-gap-0 oui-rounded-xl oui-border oui-border-line-6 oui-bg-base-9 oui-px-4 oui-pt-[9px]"
              >
                {/* Affiliate manages referrals and the commissions they earn. */}
                <TabsTrigger
                  value={TabTypes.affiliate}
                  variant="text"
                  size="xl"
                  className="oui-h-[26px] oui-px-3 oui-pb-[9px]"
                >
                  <span className="oui-flex oui-items-center oui-gap-1">
                    <AffiliateIcon size={16} />
                    {t("common.affiliate")}
                  </span>
                </TabsTrigger>
                {/* Trader shows fee rebates earned as a referee or individual trader. */}
                <TabsTrigger
                  value={TabTypes.trader}
                  variant="text"
                  size="xl"
                  className="oui-h-[26px] oui-px-3 oui-pb-[9px]"
                >
                  <span className="oui-flex oui-items-center oui-gap-1">
                    <TraderIcon size={16} />
                    {t("affiliate.trader")}
                  </span>
                </TabsTrigger>
              </TabsList>
              <TabsContent
                value={TabTypes.affiliate}
                className="oui-mt-8 md:oui-mt-10"
              >
                {affiliateContent}
              </TabsContent>
              <TabsContent
                value={TabTypes.trader}
                className="oui-mt-8 md:oui-mt-10"
              >
                <MultiLevelTraderPage />
              </TabsContent>
            </TabsBase>
          ) : (
            affiliateContent
          )}
        </Flex>
      </div>
    </Flex>
  );
};
