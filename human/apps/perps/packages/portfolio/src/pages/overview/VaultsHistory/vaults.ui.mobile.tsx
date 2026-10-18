import { useTranslation } from "@paxeer-network/i18n";
import { Divider } from "@paxeer-network/ui";
import { DataFilter, TokenIcon, ListView, Text } from "@paxeer-network/ui";
import { useVaultsHistoryHookReturn } from "./useDataSource.script";

export const VaultsHistoryMobile = (props: useVaultsHistoryHookReturn) => {
  const { dateRange, onFilter, dataSource, isLoading, pagination } = props;
  const { t } = useTranslation();

  const loadMore = () => {
    if (dataSource.length < (pagination?.count || 0)) {
      pagination?.onPageSizeChange?.(pagination?.pageSize + 50);
    }
  };

  const renderHistoryItem = (item: any) => {
    const typeColor = item.type === "deposit" ? "buy" : "sell";
    const typeText =
      item.type === "deposit" ? t("common.deposit") : t("common.withdraw");
    const amountText = item.amount_change ? Math.abs(item.amount_change) : "-";
    return (
      <div className="oui-flex oui-flex-col oui-rounded-xl oui-bg-base-9 oui-p-2 oui-font-semibold">
        <div className="oui-flex oui-items-center oui-justify-between oui-text-sm oui-text-base-contrast-80">
          <div>{item.vaultName}</div>
          <div>
            {item.status?.slice(0, 1).toUpperCase() + item.status?.slice(1)}
          </div>
        </div>
        <Text.formatted rule="date" intensity={36} size="2xs">
          {item.created_time}
        </Text.formatted>
        <Divider className="oui-my-2" />
        <div className="oui-flex oui-items-center [&>div]:oui-flex-1 [&>div]:oui-text-2xs [&>div]:oui-text-base-contrast-36">
          <div>
            <div>{t("common.token")}</div>
            <div className="oui-flex oui-items-center oui-gap-1 oui-text-xs oui-text-base-contrast-80">
              <TokenIcon name={"USDC"} size="2xs" />
              <Text>USDC</Text>
            </div>
          </div>
          <div>
            <div>{t("common.type")}</div>
            <Text color={typeColor} className="oui-text-xs">
              {typeText}
            </Text>
          </div>
          <div className="oui-text-end">
            <div>{t("common.amount")}</div>
            <div className="oui-text-xs oui-text-base-contrast-80">
              {amountText}
            </div>
          </div>
        </div>
      </div>
    );
  };

  return (
    <>
      <DataFilter
        items={[
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
        className="oui-sticky oui-top-[44px] oui-z-10 oui-border-none oui-bg-base-10 oui-px-3 oui-py-2"
      />
      <ListView
        dataSource={dataSource}
        renderItem={renderHistoryItem}
        contentClassName="!oui-space-y-1"
        loadMore={loadMore}
        isLoading={isLoading}
        className="oui-px-1"
      />
    </>
  );
};
