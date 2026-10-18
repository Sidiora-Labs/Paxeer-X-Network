import { FC } from "react";
import { VolBarChart } from "@paxeer-network/chart";
import { useTranslation } from "@paxeer-network/i18n";
import { Flex, Select, Text } from "@paxeer-network/ui";
import { Decimal } from "@paxeer-network/utils";
import { TitleStatisticReturns } from "./titleStatistic.script";

export const TitleStatistic: FC<TitleStatisticReturns> = (props) => {
  return (
    <Flex
      id="oui-affiliate-trader-titleStatistic"
      r={"2xl"}
      p={6}
      width={"100%"}
      gap={4}
      direction={"column"}
      className="oui-bg-base-9 xl:oui-flex-1"
    >
      <Title {...props} />
      <Flex className="oui-min-h-[170px] oui-h-full oui-w-full oui-flex oui-flex-row oui-items-stretch">
        <VolBarChart
          data={props.dataSource}
          colors={{ fill: "rgb(var(--oui-color-success))" }}
          className="oui-w-full oui-flex-1"
          tooltip={{
            rm: Decimal.ROUND_DOWN,
            dp: props.volType === "volume" ? 2 : 6,
          }}
        />
      </Flex>
    </Flex>
  );
};

const Title: FC<TitleStatisticReturns> = (props) => {
  const { t } = useTranslation();
  return (
    <Flex direction={"row"} justify={"between"} width={"100%"}>
      <Text className="oui-text-lg">{t("affiliate.statistics")}</Text>
      <Flex direction={"row"} gap={2} className={"oui-min-w-14"}>
        <Select.options
          size={"xs"}
          value={props.period}
          onValueChange={props.onPeriodChange}
          options={props.periodTypes}
        />
        <Select.options
          size={"xs"}
          value={props.volType}
          onValueChange={props.onVolTypeChange}
          options={props.volTypes}
        />
      </Flex>
    </Flex>
  );
};
