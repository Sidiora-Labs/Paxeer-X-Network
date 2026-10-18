import { FC } from "react";
import { useTranslation } from "@paxeer-network/i18n";
import { cn, Text, ChevronRightIcon, Button } from "@paxeer-network/ui";
import { AuthGuard } from "@paxeer-network/ui-connector";
import { useCanTrade } from "../../../../hooks/useCanTrade";
import { CampaignConfig, PrizePool } from "../type";
import { formatPrizeAmount, formatTradingVolume } from "../utils";
import { PricePoolScriptReturn } from "./pricePool.script";
import {
  calculateProgressWidth,
  getCurrentTierIndex,
  getProgressLeft,
} from "./utils";

type PricePoolDesktopUIProps = PricePoolScriptReturn & {
  isMobile?: boolean;
  tradingVolume: number;
  isCampaignStarted: boolean;
  tooltipProps: any;
  onLearnMore: () => void;
  statistics: any;
  campaign: CampaignConfig;
  shouldShowJoinButton: boolean;
  joinCampaign: (data: { campaign_id: string | number }) => Promise<any>;
  isJoining: boolean;
  showTradeButton: boolean;
  onTradeNow: () => void;
  totalPrizePool: { amount: number; currency: string } | null;
};

export const PricePoolDesktopUI: FC<PricePoolDesktopUIProps> = ({
  totalPrizePool,
  ticketPrizePool,
  highlightPool,
  isMobile,
  onLearnMore,
  statistics,
  tradingVolume,
  campaign,
  shouldShowJoinButton,
  joinCampaign,
  isJoining,
  showTradeButton,
  onTradeNow,
  status,
}) => {
  const { t } = useTranslation();
  const canTrade = useCanTrade();

  const renderButton = () => {
    if (campaign?.emphasisConfig?.hideConnectWallet && !canTrade) {
      return null;
    }

    if (shouldShowJoinButton || showTradeButton) {
      return (
        <div className="oui-w-full">
          <AuthGuard
            buttonProps={{
              size: "md",
              fullWidth: true,
              className: cn(["oui-px-5", !isMobile && "oui-w-[590px]"]),
            }}
          >
            {shouldShowJoinButton && (
              <Button
                size={"md"}
                variant="gradient"
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
                size={"md"}
                variant="gradient"
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
      );
    }
    return null;
  };

  return (
    <div
      className={cn([
        "oui-rounded-[24px] oui-border oui-border-solid oui-border-base-contrast/[0.08] oui-bg-primary/[0.08] oui-p-6 oui-backdrop-blur-[10px]",
        "oui-flex oui-flex-col",
        isMobile ? "oui-w-[calc(100vw-24px)]" : "oui-w-[640px]",
      ])}
    >
      <div className="oui-flex oui-items-center">
        <div className="oui-flex oui-flex-1 oui-flex-col">
          <div className="oui-text-xs oui-font-semibold oui-text-base-contrast-54">
            Current total prize pool
          </div>
          <div>
            <Text.gradient
              weight="bold"
              color="brand"
              className={cn([
                "oui-trading-leaderboard-title ",
                isMobile
                  ? "oui-text-[20px] oui-leading-[28px]"
                  : "oui-text-[32px] oui-leading-[28px]",
              ])}
            >
              {totalPrizePool
                ? formatPrizeAmount(
                    totalPrizePool.amount,
                    totalPrizePool.currency,
                  )
                : "USDC"}
            </Text.gradient>
          </div>
        </div>
        {(ticketPrizePool || highlightPool) && (
          <div className="oui-flex oui-flex-1 oui-flex-col">
            <div className="oui-text-xs oui-font-semibold oui-text-base-contrast-54">
              {highlightPool?.label || "Raffle prize"}
            </div>
            <div>
              <Text.gradient
                weight="bold"
                color="brand"
                className={cn([
                  "oui-trading-leaderboard-title oui-leading-[28px]",
                  isMobile
                    ? "oui-text-[20px] oui-leading-[28px]"
                    : "oui-text-[32px] oui-leading-[28px]",
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

      <div className="oui-mb-3 oui-mt-4">
        <TradingVolumeAndParticipation
          onLearnMore={onLearnMore}
          tradingVolume={tradingVolume}
          statistics={statistics}
        />
      </div>

      <PricePoolProgress
        isMobile={isMobile}
        tieredPrizePools={campaign?.tiered_prize_pools || []}
        tradingVolume={tradingVolume}
      />

      <div
        className={cn([
          "oui-flex oui-gap-4",
          isMobile ? "oui-mt-3" : canTrade && "oui-mt-6",
        ])}
      >
        {isMobile && campaign?.rule_url && (
          <div className="oui-w-full">
            <Button
              fullWidth
              size="md"
              variant="outlined"
              className="oui-flex-1 
      oui-border-[rgb(var(--oui-gradient-brand-start))] 
      oui-text-[rgb(var(--oui-gradient-brand-start))] 
      hover:oui-bg-[rgb(var(--oui-gradient-brand-start))]/[0.08]
      active:oui-bg-[rgb(var(--oui-gradient-brand-start))]/[0.08]
      "
              onClick={onLearnMore}
            >
              {t("tradingLeaderboard.viewRules")}
            </Button>
          </div>
        )}
        {renderButton()}
      </div>
    </div>
  );
};

const TradingVolumeAndParticipation = ({
  onLearnMore,
  tradingVolume,
  statistics,
}: {
  onLearnMore: () => void;
  tradingVolume: number;
  statistics: any;
}) => {
  const { t } = useTranslation();
  return (
    <div className="oui-flex oui-items-center oui-text-2xs oui-font-semibold oui-text-base-contrast-36">
      <div>{t("tradingLeaderboard.tradingVolume")}</div>
      <div className="oui-ms-1 oui-me-2">
        <Text.gradient color="brand">
          <Text.numeral dp={2} prefix="$">
            {tradingVolume}
          </Text.numeral>
        </Text.gradient>
      </div>
      <div>{t("tradingLeaderboard.participants")}</div>
      <div className="oui-ms-1 oui-text-base-contrast-54">
        <Text.numeral dp={0}>{statistics?.total_participants}</Text.numeral>
      </div>
      <div
        className="oui-ms-auto oui-flex oui-cursor-pointer oui-items-center oui-gap-0.5 oui-text-2xs oui-leading-[15px] oui-text-base-contrast-36"
        onClick={onLearnMore}
      >
        {t("tradingLeaderboard.viewRules")}
        <ChevronRightIcon size={16} className="oui-text-base-contrast-36" />
      </div>
    </div>
  );
};

type PricePoolProgressProps = {
  isMobile?: boolean;
  tieredPrizePools: Array<Array<PrizePool>>;
  tradingVolume: number;
};

const PricePoolProgress: FC<PricePoolProgressProps> = ({
  isMobile,
  tieredPrizePools,
  tradingVolume,
}) => {
  // Calculate the progress bar width b ased on current trading volume
  const progressWidth = calculateProgressWidth(
    tradingVolume,
    tieredPrizePools,
    isMobile ? 6 : 2,
  );
  const currentTierIndex = getCurrentTierIndex(tradingVolume, tieredPrizePools);

  return (
    <div
      className={cn([
        "oui-relative oui-flex oui-items-center oui-p-4 oui-pe-12 oui-font-medium",
        "oui-rounded-[10px] oui-border oui-border-solid oui-border-base-contrast/[0.12]",
        "oui-trading-leaderboard-title",
        isMobile ? "oui-h-[88px] oui-text-3xs" : "oui-h-[114px] oui-text-sm",
      ])}
    >
      <div className="oui-absolute oui-flex oui-h-6 oui-w-[calc(100%-64px)] oui-items-center">
        <div
          className={cn([
            "oui-relative oui-flex oui-flex-1 oui-flex-col oui-rounded-[14px] oui-bg-base-5 oui-p-0.5",
            isMobile ? "oui-h-3" : "oui-h-4",
          ])}
        >
          <div
            className="oui-h-full oui-rounded-[14px] oui-bg-[linear-gradient(270deg,rgb(var(--oui-gradient-brand-start))_0%,rgb(var(--oui-gradient-brand-end))_100%)]"
            style={{ width: `${progressWidth}%` }}
          />
          <div
            style={{
              position: "absolute",
              left: `${progressWidth}%`,
              transform: `translate(-90%, ${isMobile ? "-44" : "-25"}%)`,
              scale: isMobile ? "0.8" : "1",
              zIndex: 50,
            }}
          >
            <Dot />
          </div>
        </div>
      </div>
      <div
        className={cn(
          "oui-absolute oui-start-1 oui-flex oui-w-[calc(100%-64px)] oui-items-center",
          "oui-p-4 oui-pe-12",
        )}
      >
        {tieredPrizePools?.map((prizePool: PrizePool[], index: number) => {
          const isReached = index <= currentTierIndex;
          const isCurrentTier = index === currentTierIndex;

          const left = getProgressLeft(tieredPrizePools.length, index);

          return (
            <div
              key={prizePool[0].pool_id}
              className="oui-flex oui-items-center"
            >
              <div
                className={cn(
                  "oui-absolute oui-top-[-12px] lg:oui-top-[-24px]",
                  "-oui-translate-x-1/2",
                  isReached
                    ? "oui-text-base-contrast"
                    : "oui-text-base-contrast-36",
                )}
                style={{ left }}
              >{`>${formatTradingVolume(prizePool[0].volume_limit || 0, 0)}`}</div>
              <div
                className={cn(
                  "oui-absolute",
                  " oui-rounded-full",
                  isMobile ? "oui-size-[4px]" : "oui-size-[6px]",
                  isReached
                    ? "oui-bg-base-contrast-80"
                    : "oui-bg-[rgb(var(--oui-gradient-brand-start))]",
                )}
                style={{ left }}
              />
              <div
                className="oui-absolute oui-bottom-[-12px] -oui-translate-x-1/2 lg:oui-bottom-[-24px]"
                style={{ left }}
              >
                {isCurrentTier ? (
                  <Text.gradient color="brand" weight="bold">
                    <Text.numeral dp={0} prefix={"$"}>
                      {prizePool[0].total_prize}
                    </Text.numeral>
                  </Text.gradient>
                ) : (
                  <Text.numeral
                    dp={0}
                    prefix={"$"}
                    className={cn([
                      isReached
                        ? "oui-text-base-contrast"
                        : "oui-text-base-contrast-36",
                    ])}
                  >
                    {prizePool[0].total_prize}
                  </Text.numeral>
                )}
              </div>
            </div>
          );
        })}
      </div>
    </div>
  );
};

const Dot = () => {
  return (
    <svg
      width="24"
      height="24"
      viewBox="0 0 24 24"
      fill="none"
      xmlns="http://www.w3.org/2000/svg"
    >
      <path
        d="M12 0.749939C18.2132 0.749939 23.25 5.78674 23.25 11.9999C23.25 18.2131 18.2132 23.2499 12 23.2499C5.7868 23.2499 0.75 18.2131 0.75 11.9999C0.75 5.78674 5.7868 0.749939 12 0.749939Z"
        fill="rgb(var(--oui-color-base-10))"
      />
      <path
        d="M12 0.749939C18.2132 0.749939 23.25 5.78674 23.25 11.9999C23.25 18.2131 18.2132 23.2499 12 23.2499C5.7868 23.2499 0.75 18.2131 0.75 11.9999C0.75 5.78674 5.7868 0.749939 12 0.749939Z"
        stroke="url(#paint0_linear_31433_10290)"
        strokeWidth="1.5"
      />
      <path
        d="M8.30771 17.9999V16.3999H6.66669V7.59994H8.30771V5.99994H9.53848V7.59994H11.1795V16.3999H9.53848V17.9999H8.30771ZM7.89746 15.1999H9.94874V8.79994H7.89746V15.1999ZM14.4616 17.9999V14.1537H12.8205V9.12314H14.4616V5.99994H15.6923V9.12314H17.3334V14.1537H15.6923V17.9999H14.4616ZM14.0513 12.9537H16.1026V10.3229H14.0513V12.9537Z"
        fill="url(#paint1_linear_31433_10290)"
      />
      <defs>
        <linearGradient
          id="paint0_linear_31433_10290"
          x1="24"
          y1="11.9999"
          x2="-1.39122e-07"
          y2="11.9999"
          gradientUnits="userSpaceOnUse"
        >
          <stop stopColor="rgb(var(--oui-gradient-brand-end))" />
          <stop offset="1" stopColor="rgb(var(--oui-gradient-brand-start))" />
        </linearGradient>
        <linearGradient
          id="paint1_linear_31433_10290"
          x1="17.3334"
          y1="11.9999"
          x2="6.66669"
          y2="11.9999"
          gradientUnits="userSpaceOnUse"
        >
          <stop stopColor="rgb(var(--oui-gradient-brand-end))" />
          <stop offset="1" stopColor="rgb(var(--oui-gradient-brand-start))" />
        </linearGradient>
      </defs>
    </svg>
  );
};
