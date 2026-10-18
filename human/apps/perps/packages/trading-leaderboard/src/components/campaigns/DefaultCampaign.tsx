import { FC } from "react";
import { useTranslation } from "@paxeer-network/i18n";
import { cn } from "@paxeer-network/ui";
import { useTradingLeaderboardContext } from "../provider";
import { CampaignConfig } from "./type";

export const DefaultCampaign: FC<{
  currentCampaignId: string;
  onCampaignChange: (campaignId: string) => void;
  style?: React.CSSProperties;
  className?: string;
  /**
   * Campaign list used to pick the entry's shape; falls back to the
   * leaderboard context. This is a data source, not a shape switch — the
   * shape itself always follows the campaign count.
   */
  campaigns?: CampaignConfig[];
}> = ({
  currentCampaignId,
  onCampaignChange,
  style,
  className,
  campaigns: campaignsProp,
}) => {
  const { t } = useTranslation();
  const { campaigns: campaignsFromContext } = useTradingLeaderboardContext();
  // With campaigns to switch between, the general entry stays a tile so it
  // reads as one of the choices; with none, it degrades to a quiet
  // display-style heading.
  const asCard = !!(campaignsProp ?? campaignsFromContext)?.length;

  return (
    <div
      className={cn([
        "oui-trading-leaderboard-title",
        "oui-flex oui-h-[78px] oui-items-center oui-justify-center oui-font-semibold",
        asCard
          ? [
              // Only the interactive tile reads as clickable; the plain
              // heading below is display-only.
              "oui-cursor-pointer",
              "oui-rounded-lg oui-border oui-border-solid oui-bg-base-9",
              currentCampaignId === "general"
                ? "oui-border-[rgba(var(--oui-gradient-brand-start))]"
                : "oui-border-transparent",
            ]
          : "oui-min-w-0 oui-text-[20px] oui-leading-[28px] xl:oui-text-[36px] xl:oui-leading-[44px]",
        className,
      ])}
      style={style}
      onClick={asCard ? () => onCampaignChange("general") : undefined}
    >
      {t("tradingLeaderboard.generalLeaderboard")}
    </div>
  );
};
