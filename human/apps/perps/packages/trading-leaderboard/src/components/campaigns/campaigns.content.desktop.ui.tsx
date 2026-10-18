import { FC, useMemo, useState } from "react";
import { cn, Flex, Text } from "@paxeer-network/ui";
import { CampaignsScriptReturn } from "./campaigns.script";
import { NormalPricePoolUI } from "./pricePool/normalPricePool.ui";
import { PricePoolWidget } from "./pricePool/pricePool.widget";
import { CampaignConfig } from "./type";
import {
  formatCampaignDateRange,
  getTradingVolume,
  getTicketPrizePool,
} from "./utils";

type CampaignsContentDesktopUIProps = Partial<CampaignsScriptReturn> & {
  classNames?: {
    container?: string;
    title?: string;
    description?: string;
    time?: string;
    topContainer?: string;
    descriptionContainer?: string;
  };
  isMobile?: boolean;
  campaign: CampaignConfig;
};
export const CampaignsContentDesktopUI: FC<CampaignsContentDesktopUIProps> = ({
  campaign,
  statistics,
  onLearnMore,
  onTradeNow,
  backgroundSrc,
  classNames,
  isMobile,
  shouldShowJoinButton,
  joinCampaign,
  isJoining,
  canTrade,
  totalPrizePool,
}) => {
  const bgSrc = campaign?.image || backgroundSrc;
  const dateRange = formatCampaignDateRange(
    campaign?.start_time || "",
    campaign?.end_time || "",
  );
  // for mobile
  const [tooltipOpen, setTooltipOpen] = useState(false);

  const tooltipProps = useMemo(() => {
    if (!isMobile) {
      return {};
    }
    return {
      open: tooltipOpen,
      onOpenChange: setTooltipOpen,
    };
  }, [tooltipOpen, isMobile, setTooltipOpen]);

  // Get campaign data using utility functions
  const tradingVolume = getTradingVolume(statistics);
  const ticketPrizePool = getTicketPrizePool(campaign);

  const showTradeButton = useMemo(() => {
    return (
      !shouldShowJoinButton && new Date().toISOString() < campaign.end_time
    );
  }, [shouldShowJoinButton, campaign.end_time]);

  const isCampaignStarted = useMemo(() => {
    return campaign.start_time < new Date().toISOString();
  }, [campaign]);

  const highlightPool = useMemo(() => {
    if (!campaign?.highlight_pool_id) {
      return null;
    }
    return campaign?.prize_pools?.find(
      (pool) => pool.pool_id === campaign.highlight_pool_id,
    );
  }, [campaign]);

  const showTieredPricePool = useMemo(() => {
    return (
      campaign?.tiered_prize_pools && campaign?.tiered_prize_pools.length > 0
    );
  }, [campaign]);

  const renderContent = () => {
    return (
      <>
        <Text
          className={cn([
            "oui-trading-leaderboard-title oui-text-center oui-text-[48px] oui-font-bold oui-leading-[56px] oui-text-base-contrast",
            classNames?.title,
          ])}
        >
          {campaign.title}
        </Text>
        <div
          className={cn([
            "oui-w-[342px] oui-text-center",
            classNames?.descriptionContainer,
          ])}
        >
          <Text
            size="sm"
            weight="semibold"
            className={cn([
              "oui-text-base-contrast-54",
              classNames?.description,
            ])}
          >
            {campaign.description}
          </Text>
        </div>
      </>
    );
  };

  return (
    <div
      className={cn([
        "oui-flex oui-h-[500px] oui-w-full oui-flex-col oui-items-center oui-justify-center oui-gap-10",
        `oui-bg-cover oui-bg-center oui-bg-no-repeat`,
        campaign?.tiered_prize_pools?.length && !isMobile && "oui-h-[634px]",
        classNames?.container,
      ])}
      style={{
        backgroundImage: `linear-gradient(180deg, rgba(var(--oui-color-base-10) / 1) 0%, rgba(var(--oui-color-base-10) / 0.8) 15%, rgba(var(--oui-color-base-10) / 0.4) 40%, rgba(var(--oui-color-base-10) / 0.4) 60%, rgba(var(--oui-color-base-10) / 0.8) 85%, rgba(var(--oui-color-base-10) / 1) 100%), url(${bgSrc})`,
      }}
    >
      <div
        className={cn([
          "oui-flex oui-flex-col oui-items-center oui-justify-center oui-gap-[10px]",
          classNames?.topContainer,
        ])}
      >
        <Text
          size="sm"
          weight="semibold"
          className={cn(["oui-text-base-contrast-54", classNames?.time])}
        >
          {dateRange}
        </Text>
        {renderContent()}
      </div>
      {showTieredPricePool ? (
        <PricePoolWidget
          onLearnMore={onLearnMore as () => void}
          onTradeNow={onTradeNow as () => void}
          shouldShowJoinButton={shouldShowJoinButton as boolean}
          joinCampaign={
            joinCampaign as (data: {
              campaign_id: string | number;
            }) => Promise<any>
          }
          isJoining={isJoining as boolean}
          canTrade={canTrade as boolean}
          campaign={campaign}
          statistics={statistics}
          tradingVolume={tradingVolume}
          isMobile={isMobile as boolean}
          isCampaignStarted={isCampaignStarted}
          tooltipProps={tooltipProps}
          showTradeButton={showTradeButton}
          totalPrizePool={totalPrizePool as any}
        />
      ) : (
        <NormalPricePoolUI
          totalPrizePool={totalPrizePool}
          onLearnMore={onLearnMore}
          onTradeNow={onTradeNow}
          shouldShowJoinButton={shouldShowJoinButton}
          campaign={campaign}
          statistics={statistics}
          tradingVolume={tradingVolume}
          ticketPrizePool={ticketPrizePool as any}
          highlightPool={highlightPool as any}
          isMobile={isMobile as boolean}
          isCampaignStarted={isCampaignStarted}
          tooltipProps={tooltipProps}
          showTradeButton={showTradeButton}
          joinCampaign={
            joinCampaign as (data: {
              campaign_id: string | number;
            }) => Promise<any>
          }
          isJoining={isJoining as boolean}
          canTrade={canTrade as boolean}
        />
      )}
    </div>
  );
};
