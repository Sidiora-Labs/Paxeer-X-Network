import React from "react";
import { useTranslation } from "@paxeer-network/i18n";
import { MarginMode } from "@paxeer-network/types";
import { Button, cn, Divider, Flex, Text } from "@paxeer-network/ui";
import { removeTrailingZeros } from "@paxeer-network/utils";
import type { LTVTooltipScriptReturn } from "./LTVRiskTooltip.script";

const calculateLTVColor = (val: number): string => {
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

export const LTVRiskTooltipUI: React.FC<LTVTooltipScriptReturn> = (props) => {
  const { t } = useTranslation();
  const {
    ltv_threshold,
    negative_usdc_threshold,
    isThresholdLoading,
    holdingData = [],
    currentLtv,
    onConvert,
    marginMode,
  } = props;
  return (
    <Flex
      gap={1}
      className="oui-orderEntry-ltvRiskTooltip oui-w-72 oui-max-w-72"
      direction="column"
      itemAlign="start"
    >
      <Flex width={"100%"} justify="between" itemAlign="center">
        <Text intensity={36} size="xs">
          {t("common.assets")}
        </Text>
        <Text intensity={36} size="xs">
          {t("transfer.deposit.collateralContribution")}
        </Text>
      </Flex>
      {holdingData.map((asset, index) => {
        return (
          <Flex
            key={`item-${index}`}
            width={"100%"}
            justify="between"
            itemAlign="center"
          >
            <Text intensity={80} size="xs">
              {asset.token}
            </Text>
            <Text
              size="xs"
              intensity={80}
              className={cn(
                Number(asset.collateralContribution) < 0 && "oui-text-warning",
              )}
            >
              {removeTrailingZeros(asset.collateralContribution)}
            </Text>
          </Flex>
        );
      })}
      <Divider className="oui-w-full" />
      <Flex width={"100%"} justify="between" itemAlign="center">
        <Text intensity={36} size="xs">
          {t("transfer.LTV.currentLTV")}
        </Text>
        <Text
          size="xs"
          intensity={36}
          className={cn("oui-select-none", calculateLTVColor(currentLtv))}
        >
          {currentLtv}%
        </Text>
      </Flex>

      <Text className="oui-py-2" intensity={54} size="2xs">
        {t("transfer.LTV.tooltip", {
          threshold: isThresholdLoading ? "-" : ltv_threshold,
          usdcThreshold: isThresholdLoading ? "-" : negative_usdc_threshold,
        })}
      </Text>
      {marginMode === MarginMode.ISOLATED ? (
        <Text className="oui-pb-2" intensity={54} size="2xs">
          {t("transfer.LTV.isolatedMarginOrderHint")}
        </Text>
      ) : null}
      <Button
        fullWidth
        size={"md"}
        variant={"outlined"}
        color={"secondary"}
        className="oui-ltvRiskTooltip-convert-btn"
        onClick={onConvert}
      >
        {t("transfer.convert.convertAssets")}
      </Button>
    </Flex>
  );
};
