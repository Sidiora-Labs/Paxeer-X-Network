import { FC } from "react";
import { useTranslation } from "@paxeer-network/i18n";
import { cn } from "@paxeer-network/ui";

export const DefaultCampaign: FC<{
  currentCampaignId: string;
  onCampaignChange: (campaignId: string) => void;
  style?: React.CSSProperties;
  className?: string;
}> = ({ currentCampaignId, onCampaignChange, style, className }) => {
  const { t } = useTranslation();
  return (
    <div
      className={cn([
        "oui-trading-leaderboard-title",
        "oui-flex oui-items-center oui-justify-center oui-cursor-pointer",
        "oui-bg-base-9 oui-h-[78px] oui-rounded-lg oui-border oui-border-solid",
        currentCampaignId === "general"
          ? "oui-border-[rgba(var(--oui-gradient-brand-start))]"
          : "oui-border-transparent",
        className,
      ])}
      style={style}
      onClick={() => onCampaignChange("general")}
    >
      {t("tradingLeaderboard.generalLeaderboard")}
    </div>
  );
};
