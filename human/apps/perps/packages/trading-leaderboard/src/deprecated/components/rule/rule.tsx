import { FC } from "react";
import { useTranslation } from "@paxeer-network/i18n";
import { cn } from "@paxeer-network/ui";
import { LeaderboardTitle } from "../../pages/leaderboard/page";
import {
  DescriptionContent,
  DescriptionItem,
  DescriptionConfig,
} from "./description";

type RuleUIProps = {
  id: string;
  className?: string;
  isMobile?: boolean;
  rules?: DescriptionItem[];
  ruleConfig?: DescriptionConfig;
};

export const CampaignRuleUI: FC<RuleUIProps> = ({
  id,
  className,
  isMobile,
  rules,
  ruleConfig,
}) => {
  const { t } = useTranslation();
  return (
    <div
      className={cn("oui-mx-auto oui-max-w-[992px] oui-py-10", className)}
      id={id}
    >
      <LeaderboardTitle
        isMobile={isMobile}
        title={t("tradingLeaderboard.rules")}
      />
      <DescriptionContent description={rules || []} config={ruleConfig} />
    </div>
  );
};
