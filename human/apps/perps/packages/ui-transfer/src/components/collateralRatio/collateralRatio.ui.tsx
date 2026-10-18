import React from "react";
import { useTranslation } from "@paxeer-network/i18n";
import { Box, Flex, Text, Tooltip } from "@paxeer-network/ui";
import { Decimal } from "@paxeer-network/utils";
import { TooltipIcon } from "../icons/tooltipIcon";

const TooltipContent: React.FC = () => {
  const { t } = useTranslation();
  return (
    <Box className="oui-w-72 oui-max-w-72">
      <Text size="2xs" intensity={80}>
        {t("portfolio.overview.column.collateralRatio.explain")}
      </Text>{" "}
      <a
        href="https://paxeer-network.network/docs/introduction/trade-on-paxeer-network/multi-collateral"
        target="_blank"
        rel="noreferrer"
        className={
          "oui-border-b oui-border-dashed oui-border-line-12 oui-text-2xs oui-text-primary"
        }
      >
        {t("tradingLeaderboard.learnMore")}
      </a>
    </Box>
  );
};

export const CollateralRatioUI: React.FC<{ value: number }> = (props) => {
  const { t } = useTranslation();
  const { value } = props;
  return (
    <Flex width="100%" itemAlign="center" justify="between">
      <Flex justify="start" itemAlign="center">
        <Text size="2xs" intensity={36}>
          {t("portfolio.overview.column.collateralRatio")}
        </Text>
        <Tooltip className="oui-p-2" content={<TooltipContent />}>
          <TooltipIcon className="oui-ms-[2px] oui-cursor-pointer oui-text-base-contrast-36" />
        </Tooltip>
      </Flex>
      <Text.numeral
        dp={2}
        rm={Decimal.ROUND_DOWN}
        coloring
        className="oui-font-semibold"
        rule="percentages"
      >
        {value}
      </Text.numeral>
    </Flex>
  );
};
