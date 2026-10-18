import { FC } from "react";
import { VolBarChart } from "@paxeer-network/chart";
import { useTranslation } from "@paxeer-network/i18n";
import { Flex, Select, Text } from "@paxeer-network/ui";
import { Decimal } from "@paxeer-network/utils";
import { TitleStatisticReturns } from "./commissionChart.script";

export const CommissionChart: FC<TitleStatisticReturns> = (props) => {
  const { t } = useTranslation();

  return (
    <Flex
      r={"2xl"}
      p={6}
      width={"100%"}
      gap={4}
      direction={"column"}
      className="oui-affiliate-commissionChart oui-border oui-border-line-6 oui-bg-base-9"
    >
      <Flex
        justify={"between"}
        width={"100%"}
        className="oui-commissionChart-header"
      >
        <Text size="lg">{`${t("affiliate.commission")} (USDC)`}</Text>
        <div className="oui-commissionChart-period-select">
          <Select.options
            size={"xs"}
            value={props.period}
            onValueChange={props.onPeriodChange}
            options={props.periodTypes}
          />
        </div>
      </Flex>

      <Flex className="oui-commissionChart-body oui-flex oui-h-[170px] oui-w-full oui-flex-row oui-items-stretch 2xl:oui-h-[196px]">
        <VolBarChart
          data={props.dataSource!}
          colors={{ fill: "rgba(0, 180, 158, 1)" }}
          className="oui-w-full oui-flex-1"
          tooltip={{
            rm: Decimal.ROUND_DOWN,
            dp: 2,
          }}
        />
      </Flex>
    </Flex>
  );
};
