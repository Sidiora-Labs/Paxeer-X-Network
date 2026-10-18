import { FC, useMemo, useState } from "react";
import { useTranslation } from "@paxeer-network/i18n";
import {
  cn,
  Text,
  Tooltip,
  InfoCircleIcon,
  ChevronRightIcon,
  Button,
} from "@paxeer-network/ui";
import { AuthGuard } from "@paxeer-network/ui-connector";
import { CampaignsScriptReturn } from "../campaigns.script";
import { CampaignConfig } from "../type";
import { formatPrizeAmount } from "../utils";

type NormalPricePoolUIProps = Partial<CampaignsScriptReturn> & {
  isMobile: boolean;
  campaign: CampaignConfig;
  isCampaignStarted: boolean;
  ticketPrizePool?: { amount: number; currency: string };
  highlightPool?: { label: string; total_prize: number; currency: string };
  tradingVolume: number;
  tooltipProps: any;
  showTradeButton: boolean;
};

export const NormalPricePoolUI: FC<NormalPricePoolUIProps> = ({
  totalPrizePool,
  ticketPrizePool,
  highlightPool,
  isMobile,
  isCampaignStarted,
  campaign,
  onLearnMore,
  onTradeNow,
  shouldShowJoinButton,
  joinCampaign,
  isJoining,
  canTrade,
  statistics,
  tradingVolume,
  tooltipProps,
  showTradeButton,
}) => {
  const { t } = useTranslation();

  const tooltipContent = useMemo(() => {
    if (!campaign?.prize_pools) {
      return null;
    }

    return (
      <div className="oui-flex oui-min-w-[240px] oui-flex-col oui-gap-1">
        {campaign?.prize_pools?.map((pool) => {
          return (
            <div
              key={pool.pool_id}
              className="oui-flex oui-h-[18px] oui-items-center oui-justify-between"
            >
              <Text
                size="2xs"
                weight="semibold"
                className="oui-text-base-contrast-54"
              >
                {pool.label}
              </Text>
              <div className="oui-flex oui-items-center oui-gap-1">
                <Text.numeral
                  dp={2}
                  size="2xs"
                  weight="semibold"
                  className="oui-text-base-contrast"
                >
                  {pool.total_prize}
                </Text.numeral>
                <Text
                  size="2xs"
                  weight="semibold"
                  className="oui-text-base-contrast"
                >
                  {pool.currency}
                </Text>
              </div>
            </div>
          );
        })}
      </div>
    );
  }, [campaign]);
  return (
    <div className="oui-overflow-hidden oui-rounded-2xl oui-border oui-border-solid oui-border-base-contrast/[0.08]">
      <div
        className={cn([
          "oui-flex oui-items-center oui-gap-4 oui-px-4 oui-py-3 oui-backdrop-blur-[10px]",
          isCampaignStarted ? "" : "oui-hidden",
        ])}
      >
        <div className="oui-flex oui-items-center oui-gap-1">
          <Text
            size="2xs"
            weight="semibold"
            className="oui-text-base-contrast-54"
          >
            {campaign?.user_account_label ||
              t("tradingLeaderboard.participants")}
          </Text>
          <Text
            size="2xs"
            weight="semibold"
            className="oui-text-base-contrast-80"
          >
            {statistics?.total_participants}
          </Text>
        </div>
        <div className="oui-flex oui-items-center oui-gap-1">
          <Text
            size="2xs"
            weight="semibold"
            className="oui-text-base-contrast-54"
          >
            {t("tradingLeaderboard.tradingVolume")}
          </Text>
          <Text.numeral
            dp={2}
            currency="$"
            size="2xs"
            weight="semibold"
            className="oui-text-base-contrast-80"
          >
            {tradingVolume}
          </Text.numeral>
        </div>
      </div>
      <div
        className={cn([
          "oui-rounded-2xl oui-border oui-border-solid oui-border-base-contrast/[0.08] oui-bg-primary/[0.08] oui-p-4 oui-backdrop-blur-[10px]",
          "oui-flex oui-flex-col oui-gap-4",
          isCampaignStarted ? "" : "oui-border-transparent",
        ])}
      >
        <div className="oui-flex oui-items-center oui-gap-6">
          <div className="oui-flex oui-flex-col">
            <div className="oui-flex oui-items-center oui-gap-1">
              <Text
                size={isMobile ? "2xs" : "xs"}
                weight="semibold"
                className="oui-text-base-contrast-54"
              >
                {/* {t("tradingLeaderboard.prizePool")} */}
                {"Total prize pool"}
              </Text>
              <Tooltip
                // @ts-ignore
                content={tooltipContent}
                {...tooltipProps}
                className="oui-max-w-[260px] oui-bg-base-5 oui-px-2 oui-py-1"
              >
                <div
                  className="oui-flex oui-size-4 oui-items-center oui-justify-center"
                  onClick={() => tooltipProps.onOpenChange(true)}
                >
                  <InfoCircleIcon className="oui-cursor-pointer" />
                </div>
              </Tooltip>
            </div>
            <div>
              <Text.gradient
                weight="bold"
                color="brand"
                className={cn([
                  "oui-trading-leaderboard-title",
                  isMobile
                    ? "oui-text-[20px] oui-leading-[24px]"
                    : "oui-text-[24px] oui-leading-[28px]",
                ])}
              >
                {totalPrizePool
                  ? formatPrizeAmount(
                      totalPrizePool.amount,
                      totalPrizePool.currency,
                    )
                  : "0 USDC"}
              </Text.gradient>
            </div>
          </div>
          {(ticketPrizePool || highlightPool) && (
            <div className="oui-flex oui-flex-col">
              <div className="oui-flex oui-items-center oui-gap-1">
                <Text
                  size={isMobile ? "2xs" : "xs"}
                  weight="semibold"
                  className="oui-text-base-contrast-54"
                >
                  {/* {t("tradingLeaderboard.ticketPrizePool")} */}
                  {highlightPool?.label || "Raffle prize"}
                </Text>
              </div>
              <div>
                <Text.gradient
                  weight="bold"
                  color="brand"
                  className={cn([
                    "oui-trading-leaderboard-title",
                    isMobile
                      ? "oui-text-[20px] oui-leading-[24px]"
                      : "oui-text-[24px] oui-leading-[28px]",
                  ])}
                >
                  {formatPrizeAmount(
                    ticketPrizePool?.amount || highlightPool?.total_prize || 0,
                    ticketPrizePool?.currency ||
                      highlightPool?.currency ||
                      "USDC",
                  )}
                </Text.gradient>
              </div>
            </div>
          )}
        </div>
        {isMobile && campaign?.rule_url && (
          <div
            className="-oui-mb-1 -oui-mt-2 oui-flex oui-items-center oui-gap-0.5 oui-text-2xs oui-leading-[15px] oui-text-base-contrast-36"
            onClick={onLearnMore}
          >
            {t("tradingLeaderboard.viewRules")}
            <ChevronRightIcon size={16} className="oui-text-base-contrast-36" />
          </div>
        )}
        <div
          className={cn(
            "oui-flex oui-gap-4",
            campaign?.rule_url || canTrade ? "" : "oui-hidden",
          )}
        >
          {!isMobile && campaign?.rule_url && (
            <Button
              size="md"
              variant="outlined"
              className="oui-flex-1 
        oui-border-[rgb(var(--oui-gradient-brand-start))] 
        oui-text-[rgb(var(--oui-gradient-brand-start))] 
        hover:oui-bg-[rgb(var(--oui-gradient-brand-start))]/[0.08]
        active:oui-bg-[rgb(var(--oui-gradient-brand-start))]/[0.08]
        "
              onClick={onLearnMore}
              fullWidth
            >
              {t("tradingLeaderboard.viewRules")}
            </Button>
          )}
          {(shouldShowJoinButton || showTradeButton) && (
            <div className="oui-w-full">
              <AuthGuard
                buttonProps={{
                  fullWidth: true,
                  size: isMobile ? "sm" : "md",
                  className: "oui-px-5",
                }}
              >
                {shouldShowJoinButton && (
                  <Button
                    size={isMobile ? "sm" : "md"}
                    variant="contained"
                    color="primary"
                    className="oui-flex-1"
                    loading={isJoining}
                    disabled={isJoining}
                    onClick={async () => {
                      try {
                        await joinCampaign?.({
                          campaign_id: Number(campaign.campaign_id),
                        });
                      } catch (error) {
                        console.error("Failed to join campaign:", error);
                      }
                    }}
                    fullWidth
                  >
                    {t("tradingLeaderboard.joinNow")}
                  </Button>
                )}
                {showTradeButton && (
                  <Button
                    size={isMobile ? "sm" : "md"}
                    variant="contained"
                    color="primary"
                    className="oui-flex-1"
                    onClick={onTradeNow}
                    fullWidth
                  >
                    {campaign?.trading_config?.format ||
                      t("tradingLeaderboard.tradeNow")}
                  </Button>
                )}
              </AuthGuard>
            </div>
          )}
        </div>
      </div>
    </div>
  );
};
