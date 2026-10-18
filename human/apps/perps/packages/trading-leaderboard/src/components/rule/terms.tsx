import { FC } from "react";
import { useTranslation } from "@paxeer-network/i18n";
import { EMPTY_LIST } from "@paxeer-network/types";
import { cn } from "@paxeer-network/ui";
import { LeaderboardTitle } from "../../pages/leaderboard/page";
import { DescriptionContent, DescriptionItem } from "./description";

type TermsUIProps = {
  className?: string;
  isMobile?: boolean;
  termsConfig?: DescriptionItem[];
};

export const CampaignTermsUI: FC<TermsUIProps> = ({
  className,
  isMobile,
  termsConfig,
}) => {
  const { t } = useTranslation();
  return (
    <div className={cn("oui-mx-auto oui-max-w-[992px] oui-py-10", className)}>
      <LeaderboardTitle
        isMobile={isMobile}
        title={t("tradingLeaderboard.termsAndConditions")}
      />
      <DescriptionContent description={termsConfig || EMPTY_LIST} />
    </div>
  );
};
