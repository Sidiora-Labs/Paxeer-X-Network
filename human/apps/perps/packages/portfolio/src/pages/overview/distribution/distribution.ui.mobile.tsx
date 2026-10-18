import { FC, useMemo } from "react";
import { useTranslation } from "@paxeer-network/i18n";
import { DataFilter, Flex, Text, TokenIcon } from "@paxeer-network/ui";
import { ListView } from "@paxeer-network/ui";
import { type useDistributionHistoryHookReturn } from "./useDataSource.script";

type FundingHistoryProps = {} & useDistributionHistoryHookReturn;

export const DistributionHistoryMobile: FC<FundingHistoryProps> = (props) => {
  const { dataSource, queryParameter, onFilter, isLoading, pagination } = props;
  const { type, dateRange } = queryParameter;
  const { t } = useTranslation();

  const TYPES = useMemo(() => {
    return [
      { label: t("common.all"), value: "All" },
      {
        label: t("portfolio.overview.distribution.type.referralCommission"),
        value: "REFERRER_REBATE",
      },
      {
        label: t("portfolio.overview.distribution.type.refereeRebate"),
        value: "REFEREE_REBATE",
      },
      {
        label: t("portfolio.overview.distribution.type.brokerFee"),
        value: "BROKER_FEE",
      },
    ];
  }, [t]);

  const getStatusText = (status: string) => {
    switch (status) {
      case "CREATED":
      case "SPLIT":
        return t("assetHistory.status.processing") as string;
      case "COMPLETED":
      default:
        return t("assetHistory.status.completed") as string;
    }
  };

  const getTypeText = (type: string) => {
    switch (type) {
      case "REFERRER_REBATE":
        return t("portfolio.overview.distribution.type.referralCommission");
      case "REFEREE_REBATE":
        return t("portfolio.overview.distribution.type.refereeRebate");
      case "BROKER_FEE":
        return t("portfolio.overview.distribution.type.brokerFee");
      default:
        return "-";
    }
  };

  const renderItem = (item: any) => {
    return (
      <Flex
        p={2}
        direction="column"
        gapY={2}
        className="oui-rounded-xl oui-bg-base-9 oui-font-semibold"
      >
        <Flex direction="row" justify="between" width="100%" height="20px">
          <Text.formatted
            rule="date"
            className="oui-text-2xs oui-text-base-contrast-36"
          >
            {item.created_time}
          </Text.formatted>
          <Text className="oui-text-sm oui-text-base-contrast-80">
            {getStatusText(item.status)}
          </Text>
        </Flex>
        <div className="oui-h-px oui-w-full oui-bg-base-6" />
        <Flex direction="row" justify="between" width="100%">
          <Flex direction="column">
            <Text className="oui-text-2xs oui-text-base-contrast-36">
              {t("common.token")}
            </Text>
            <Flex gapX={2}>
              <TokenIcon name={item.token} size="xs" />
              <span className="oui-text-xs oui-text-base-contrast-80">
                {item.token}
              </span>
            </Flex>
          </Flex>
          <Flex direction="column">
            <Text className="oui-text-2xs oui-text-base-contrast-36">
              {t("common.type")}
            </Text>
            <Text className="oui-text-xs oui-text-base-contrast-80">
              {getTypeText(item.type)}
            </Text>
          </Flex>
          <Flex direction="column">
            <Text className="oui-text-2xs oui-text-base-contrast-36">
              {t("common.amount")}
            </Text>
            <Text className="oui-text-xs oui-text-base-contrast-80">
              {item.amount}
            </Text>
          </Flex>
        </Flex>
      </Flex>
    );
  };

  const loadMore = () => {
    if (dataSource.length < (pagination?.count || 0)) {
      pagination?.onPageSizeChange &&
        pagination.onPageSizeChange(pagination?.pageSize + 50);
    }
  };

  return (
    <>
      <DataFilter
        items={[
          {
            type: "picker",
            name: "type",
            options: TYPES,
            value: type,
            size: "md",
          },
          {
            type: "range",
            name: "dateRange",
            value: {
              from: dateRange[0],
              to: dateRange[1],
            },
          },
        ]}
        onFilter={(value) => {
          onFilter(value);
        }}
        className="oui-px-3 oui-py-2 oui-sticky oui-top-[44px] oui-z-10 oui-bg-base-10"
      />
      <ListView
        dataSource={dataSource}
        renderItem={renderItem}
        contentClassName="oui-space-y-1"
        loadMore={loadMore}
        isLoading={isLoading}
        className="oui-px-1"
      />
    </>
  );
};
