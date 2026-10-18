import { FC } from "react";
import { useTranslation } from "@paxeer-network/i18n";
import { Divider, Flex, Text } from "@paxeer-network/ui";
import { FundingRateHintState } from "./fundingRateHint.script";

export type FundingRateHintProps = FundingRateHintState & {
  className?: string;
  style?: React.CSSProperties;
};

export const FundingRateHint: FC<FundingRateHintProps> = (props) => {
  const { t } = useTranslation();
  const {
    fundingPeriod,
    capFunding,
    floorFunding,
    lastFundingRate,
    estFundingFee,
    lastFundingRateAnnualized,
    estFundingRateByTimeframe,
  } = props;

  const renderRow = (
    label: string,
    value?: string | number,
    annualizedValue?: string,
  ) => {
    if (!value) {
      return null;
    }
    return (
      <Flex justify="between" itemAlign={"center"} width={"100%"}>
        <Text intensity={54}>
          {annualizedValue
            ? `${label}/ ${t("trading.fundingRate.annualized")}`
            : label}
        </Text>
        <Flex itemAlign="end" gap={1} className="oui-text-base-contrast-80">
          <Text intensity={80}>{value}</Text>
          {annualizedValue && " / "}
          {annualizedValue && annualizedValue}
        </Flex>
      </Flex>
    );
  };

  return (
    <Flex width={"100%"} itemAlign={"center"} direction="column" gap={1}>
      {renderRow(
        t("trading.fundingRate.predFundingRate.interval"),
        fundingPeriod,
      )}
      {renderRow(
        t("trading.fundingRate.predFundingRate.cap") +
          " / " +
          t("trading.fundingRate.predFundingRate.floor"),
        capFunding + " / " + floorFunding,
      )}
      {renderRow(
        t("trading.fundingRate.lastFundingRate"),
        lastFundingRate,
        lastFundingRateAnnualized,
      )}
      {estFundingRateByTimeframe.map(({ timeframe, value }) =>
        renderRow(
          t("trading.fundingRate.estFundingRateWithTimeframe", {
            timeframe: timeframe as string,
          }),
          value,
        ),
      )}
      {renderRow(t("trading.fundingRate.estimatedFundingFee"), estFundingFee)}
      <Divider className="oui-w-full" intensity={8} />
      {t("markets.symbolInfoBar.predFundingRate.tooltip")}
    </Flex>
  );
};
