import React, { FC } from "react";
import { useTranslation } from "@paxeer-network/i18n";
import { useAppContext } from "@paxeer-network/react-app";
import {
  Flex,
  Text,
  Box,
  Tooltip,
  modal,
  gradientTextVariants,
  cn,
  EditIcon,
} from "@paxeer-network/ui";
import { LeverageWidgetWithDialogId } from "@paxeer-network/ui-leverage";
import { TooltipContent } from "../assetView/assetView.ui";
import { RiskRateState } from "./riskRate.script";

export const RiskRate: FC<RiskRateState> = (props) => {
  const { riskRate, riskRateColor, currentLeverage, maxLeverage } = props;
  const { isHigh, isMedium, isLow } = riskRateColor;
  const { wrongNetwork } = useAppContext();
  const { t } = useTranslation();

  const textColor = wrongNetwork
    ? ""
    : isHigh
      ? "oui-text-danger"
      : isMedium
        ? "oui-text-warning-darken"
        : isLow
          ? gradientTextVariants({ color: "brand" })
          : "";

  const boxClsName = wrongNetwork
    ? "oui-bg-gradient-to-r oui-opacity-20 oui-from-[rgb(var(--oui-gradient-brand-start))] oui-via-[rgb(var(--oui-color-warning-darken))] oui-to-[rgb(var(--oui-color-danger-darken))] oui-h-1.5 oui-rounded-full"
    : isHigh
      ? "oui-bg-gradient-to-tr oui-from-[rgb(var(--oui-gradient-danger-end))] oui-to-[rgb(var(--oui-gradient-danger-start))] oui-h-1.5 oui-rounded-full"
      : isMedium
        ? "oui-bg-gradient-to-tr oui-from-[rgb(var(--oui-gradient-warning-end))] oui-to-[rgb(var(--oui-gradient-warning-start))] oui-h-1.5 oui-rounded-full"
        : isLow
          ? "oui-bg-gradient-to-tr oui-from-[rgb(var(--oui-gradient-brand-end))] oui-to-[rgb(var(--oui-gradient-brand-start))] oui-h-1.5 oui-rounded-full"
          : "oui-bg-gradient-to-r oui-opacity-20 oui-from-[rgb(var(--oui-gradient-brand-start))] oui-via-[rgb(var(--oui-color-warning-darken))] oui-to-[rgb(var(--oui-color-danger-darken))] oui-h-1.5 oui-rounded-full";

  return (
    <Box className="oui-riskRate oui-space-y-2" data-risk={""}>
      <Flex
        itemAlign="center"
        justify="start"
        className="oui-w-full oui-bg-base-6 oui-rounded-full oui-h-2 oui-px-[1px]"
      >
        <Box
          className={boxClsName}
          style={
            riskRate && riskRate !== "--"
              ? { width: riskRate }
              : { width: "100%" }
          }
        />
      </Flex>

      <Flex direction="row" justify="between">
        <Tooltip
          content={
            <TooltipContent
              description={t("trading.riskRate.tooltip")}
              formula={t("trading.riskRate.formula")}
            />
          }
        >
          <Text
            size="2xs"
            color="neutral"
            weight="semibold"
            className={cn(
              "oui-cursor-pointer",
              "oui-border-b oui-border-dashed oui-border-b-white/10",
            )}
          >
            {t("trading.riskRate")}
          </Text>
        </Tooltip>
        <Text
          size="xs"
          color="neutral"
          weight="semibold"
          className={cn(textColor)}
        >
          {riskRate ?? "--"}
        </Text>
      </Flex>
    </Box>
  );
};
