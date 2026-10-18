import { FC } from "react";
import { useTranslation } from "@paxeer-network/i18n";
import {
  DatePicker,
  Divider,
  Flex,
  ListView,
  Statistic,
  Column,
  Text,
  useScreen,
} from "@paxeer-network/ui";
import { AuthGuardDataTable } from "@paxeer-network/ui-connector";
import { commifyOptional } from "@paxeer-network/utils";
import { RebatesItem, RebatesReturns } from "./rebates.script";

export const Rebates: FC<RebatesReturns> = (props) => {
  return (
    <Flex
      id="oui-affiliate-trader-rebates"
      r={"2xl"}
      p={6}
      width={"100%"}
      gap={4}
      direction={"column"}
      className="oui-bg-base-9 oui-tabular-nums"
    >
      <Title {...props} />
      <Flex
        width={"100%"}
        className="oui-border-y-2 oui-border-line-6"
        height={49}
        direction={"row"}
        itemAlign={"center"}
      >
        <div>
          <DatePicker.range
            size="xs"
            value={props.dateRange}
            onChange={(range) => {
              props.setDateRange(range);
            }}
            max={90}
            disabled={{
              after: new Date(),
            }}
          />
        </div>
      </Flex>
      <List {...props} />
    </Flex>
  );
};

const Title: FC<RebatesReturns> = (props) => {
  const { t } = useTranslation();

  return (
    <Flex direction={"row"} justify={"between"} width={"100%"}>
      <Text className="oui-text-lg">{t("affiliate.trader.myRebates")}</Text>
      <Text intensity={36} className="oui-text-2xs">
        {props.displayDate}
      </Text>
    </Flex>
  );
};

const List: FC<RebatesReturns> = (props) => {
  const { t } = useTranslation();

  const { isMobile } = useScreen();

  const columns: Column<RebatesItem>[] = [
    {
      title: `${t("affiliate.trader.rebates")} (USDC)`,
      dataIndex: "referee_rebate",
      render: (value) => (
        <Text>
          {commifyOptional(value, {
            fix: 6,
            fallback: "0",
            padEnd: true,
            prefix: "$",
          })}
        </Text>
      ),
      width: 127,
    },
    {
      title: `${t("affiliate.trader.tradingVol")} (USDC)`,
      dataIndex: "vol",
      render: (value) =>
        commifyOptional(value, {
          fix: 2,
          fallback: "0",
          padEnd: true,
          prefix: "$",
        }),
      width: 127,
    },
    {
      title: t("common.date"),
      dataIndex: "date",
      render: (value) => (
        <Text.formatted formatString="yyyy-MM-dd">{value}</Text.formatted>
      ),
      width: 127,
    },
  ];

  if (isMobile) {
    return (
      <ListView<RebatesItem, RebatesItem[]>
        dataSource={props.dataSource}
        className="oui-h-[197px] oui-w-full"
        renderItem={(item, index) => {
          return <Cell item={item} />;
        }}
      />
    );
  }

  return (
    <AuthGuardDataTable
      bordered
      loading={props.isLoading}
      columns={columns}
      dataSource={props.dataSource}
      ignoreLoadingCheck={true}
      onRow={(record) => {
        return {
          className: "oui-h-[41px]",
        };
      }}
      pagination={props.pagination}
      manualPagination={false}
    />
  );
};

const Cell = (props: { item: RebatesItem }) => {
  const { t } = useTranslation();

  return (
    <Flex direction={"column"} width={"100%"}>
      <Flex width={"100%"} gap={1}>
        <Statistic
          label={`${t("affiliate.trader.rebates")} (USDC)`}
          className="oui-text-xs oui-text-base-contrast-54 oui-flex-1 oui-min-w-[105px]"
        >
          <Text size="sm" intensity={80}>
            {commifyOptional(props.item.referee_rebate, {
              fix: 6,
              fallback: "0",
              padEnd: true,
              prefix: "$",
            })}
          </Text>
        </Statistic>
        <Statistic
          label={`${t("affiliate.trader.tradingVol")} (USDC)`}
          className="oui-text-xs oui-text-base-contrast-54 oui-flex-shrink"
        >
          <Text size="sm" intensity={80}>
            {commifyOptional(props.item.vol, {
              fix: 2,
              fallback: "0",
              padEnd: true,
              prefix: "$",
            })}
          </Text>
        </Statistic>
        <Statistic
          label={t("common.date")}
          className="oui-text-xs oui-text-base-contrast-54 oui-flex-1"
          align="end"
        >
          <Text.formatted size="sm" intensity={80} formatString="yyyy-MM-dd">
            {props.item.date}
          </Text.formatted>
        </Statistic>
      </Flex>
      <Divider className="oui-w-full oui-mt-3" />
    </Flex>
  );
};
