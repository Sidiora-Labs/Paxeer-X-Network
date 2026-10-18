import React from "react";
import { useTranslation } from "@paxeer-network/i18n";
import { cn, Flex, Tooltip, Text, Box } from "@paxeer-network/ui";
import { TooltipIcon } from "../icons/tooltipIcon";
import type { LtvScriptReturns } from "./ltv.script";

const calculateTextColor = (val: number): string => {
  if (val >= 0 && val < 50) {
    return "oui-text-success";
  } else if (val >= 50 && val < 80) {
    return "oui-text-warning";
  } else if (val >= 80) {
    return "oui-text-danger";
  } else {
    return "";
  }
};

const TooltipContent: React.FC<{
  isLoading: boolean;
  ltv_threshold: string;
}> = (props) => {
  const { isLoading, ltv_threshold } = props;
  const { t } = useTranslation();
  return (
    <Box className="oui-w-72 oui-max-w-72">
      <Text size="2xs" intensity={80}>
        {t("transfer.LTV.description", {
          threshold: isLoading ? "-" : ltv_threshold,
        })}
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

export const LtvUI: React.FC<
  Readonly<
    LtvScriptReturns & {
      currentLtv: number;
      nextLTV: number;
      showDiff?: boolean;
    }
  >
> = (props) => {
  const { t } = useTranslation();
  const { currentLtv, nextLTV, showDiff, ltv_threshold, isLoading } = props;
  return (
    <Flex width="100%" itemAlign="center" justify="between">
      <Flex justify="start" itemAlign="center">
        <Text size="2xs" intensity={36}>
          {t("transfer.LTV")}
        </Text>
        <Tooltip
          className="oui-p-2"
          content={
            <TooltipContent
              isLoading={isLoading}
              ltv_threshold={ltv_threshold}
            />
          }
        >
          <TooltipIcon className="oui-ms-[2px] oui-cursor-pointer oui-text-base-contrast-36" />
        </Tooltip>
      </Flex>
      {showDiff ? (
        <Flex itemAlign="center" justify="between" gap={1}>
          <Text
            size="2xs"
            className={cn("oui-font-semibold", calculateTextColor(currentLtv))}
          >
            {currentLtv}%
          </Text>
          →
          <Text
            size="2xs"
            className={cn("oui-font-semibold", calculateTextColor(nextLTV))}
          >
            {nextLTV}%
          </Text>
        </Flex>
      ) : (
        <Text
          size="2xs"
          className={cn("oui-font-semibold", calculateTextColor(currentLtv))}
        >
          {currentLtv}%
        </Text>
      )}
    </Flex>
  );
};
