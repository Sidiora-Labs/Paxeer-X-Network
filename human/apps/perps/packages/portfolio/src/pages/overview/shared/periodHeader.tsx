import React, { useMemo } from "react";
import { useTranslation } from "@paxeer-network/i18n";
import { CardTitle, Flex, Select } from "@paxeer-network/ui";
import { PeriodType } from "./useAssetHistory";

export const PeriodTitle: React.FC<{
  onPeriodChange: (period: PeriodType) => void;
  periodTypes: string[];
  period: PeriodType;
  title: string;
}> = (props) => {
  const { t } = useTranslation();

  const periodLabel = useMemo(() => {
    return {
      [PeriodType.WEEK]: t("common.select.7d"),
      [PeriodType.MONTH]: t("common.select.30d"),
      [PeriodType.QUARTER]: t("common.select.90d"),
    };
  }, [t]);

  return (
    <Flex itemAlign={"center"} justify={"between"}>
      <CardTitle>{props.title}</CardTitle>
      <div className={"oui-min-w-14"}>
        <Select.options
          size={"xs"}
          value={props.period}
          onValueChange={props.onPeriodChange}
          options={props.periodTypes.map((item) => ({
            value: item,
            label: periodLabel[item as PeriodType],
          }))}
        />
      </div>
    </Flex>
  );
};
