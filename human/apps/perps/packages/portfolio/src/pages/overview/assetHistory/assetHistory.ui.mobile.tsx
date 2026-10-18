import { FC, useMemo } from "react";
import { useTranslation } from "@paxeer-network/i18n";
import {
  AssetHistorySideEnum,
  AssetHistoryStatusEnum,
} from "@paxeer-network/types";
import {
  Badge,
  DataFilter,
  Flex,
  ListView,
  Text,
  capitalizeFirstLetter,
  toast,
  ArrowRightShortIcon,
  EmptyDataState,
} from "@paxeer-network/ui";
import {
  AssetTarget,
  type AssetHistoryScriptReturn,
} from "./assetHistory.script";

type AssetHistoryMobileProps = AssetHistoryScriptReturn;

export const AssetHistoryMobile: FC<AssetHistoryMobileProps> = (props) => {
  const {
    dataSource,
    queryParameter,
    onFilter,
    isLoading,
    pagination,
    onDeposit,
    chainsInfo,
    isDeposit,
    isWeb3Wallet,
  } = props;
  const { dateRange, target } = queryParameter;
  const { t } = useTranslation();

  const options = useMemo(() => {
    return [
      { label: t("common.web3Wallet"), value: AssetTarget.Web3Wallet },
      { label: t("common.accountId"), value: AssetTarget.AccountId },
    ];
  }, [t]);

  const loadMore = () => {
    if (dataSource.length < (pagination?.count || 0)) {
      pagination?.onPageSizeChange?.(pagination?.pageSize + 50);
    }
  };

  const getStatusText = (item: any) => {
    const status = props.isWeb3Wallet ? item.trans_status : item.status;
    const statusMap = {
      [AssetHistoryStatusEnum.NEW]: t("assetHistory.status.pending"),
      [AssetHistoryStatusEnum.PENDING]: t("assetHistory.status.pending"),
      [AssetHistoryStatusEnum.CONFIRM]: t("assetHistory.status.confirm"),
      [AssetHistoryStatusEnum.PROCESSING]: t("assetHistory.status.processing"),
      [AssetHistoryStatusEnum.COMPLETED]: t("assetHistory.status.completed"),
      [AssetHistoryStatusEnum.FAILED]: t("assetHistory.status.failed"),
    };
    return (
      statusMap[status as keyof typeof statusMap] ||
      capitalizeFirstLetter(status?.toLowerCase())
    );
  };

  const renderHistoryItem = (item: any) => {
    const rowIsDeposit = item.side
      ? item.side !== AssetHistorySideEnum.WITHDRAW
      : isDeposit;
    // Amount formatting
    const formattedAmount = rowIsDeposit
      ? `+${item.amount - (item.fee ?? 0)}`
      : -(item.amount - (item.fee ?? 0));

    // Get tx link
    const getTxLink = () => {
      if (!item.tx_id) {
        return undefined;
      }

      const chainInfo =
        chainsInfo && Array.isArray(chainsInfo)
          ? chainsInfo.find(
              (chain: any) =>
                parseInt(item.chain_id) === parseInt(chain.chain_id),
            )
          : undefined;

      if (chainInfo?.explorer_base_url) {
        return `${chainInfo.explorer_base_url}/tx/${item.tx_id}`;
      }

      return undefined;
    };

    const onCopy = (e: any) => {
      e.preventDefault();
      e.stopPropagation();
      toast.success(t("common.copy.copied"));
    };

    const itemColor = rowIsDeposit ? "buy" : "sell";

    const amountView = (
      <Flex className="oui-text-xs">
        <Text color={itemColor}>{formattedAmount}</Text>
        <Text className="oui-ms-1" intensity={80}>
          {item.token}
        </Text>
        {isWeb3Wallet && (
          <Badge color={itemColor} className="oui-ms-2">
            {rowIsDeposit ? t("common.deposit") : t("common.withdraw")}
          </Badge>
        )}
      </Flex>
    );

    const statusView = (
      <Text intensity={isWeb3Wallet ? 80 : 36} size="xs">
        {getStatusText(item)}
      </Text>
    );

    const timeView = (
      <Text.formatted rule="date" intensity={36} size="2xs">
        {item.created_time}
      </Text.formatted>
    );

    const txIdView = item.tx_id ? (
      <a href={getTxLink()} target="_blank" rel="noreferrer">
        <Text.formatted
          copyable={!!item.tx_id}
          className="oui-underline oui-decoration-line-16 oui-decoration-dashed oui-underline-offset-4"
          rule="txId"
          onCopy={onCopy}
          intensity={36}
          size="xs"
        >
          {item.tx_id}
        </Text.formatted>
      </a>
    ) : (
      <Text intensity={54}>-</Text>
    );

    const accountId = isDeposit ? item.from_account_id : item.to_account_id;

    const accountIdView = (
      <Flex gapX={1}>
        <Text intensity={36} size="xs">
          {isDeposit
            ? t("transfer.internalTransfer.from")
            : t("transfer.internalTransfer.to")}
        </Text>
        <Text.formatted
          rule="address"
          copyable={!!accountId}
          onCopy={onCopy}
          intensity={80}
          size="xs"
        >
          {accountId}
        </Text.formatted>
      </Flex>
    );

    return (
      <Flex
        p={2}
        direction="column"
        gapY={1}
        intensity={900}
        r="xl"
        className="oui-font-semibold"
      >
        <Flex justify="between" width="100%" height="18px">
          {isWeb3Wallet ? (
            <>
              {amountView}
              {statusView}
            </>
          ) : (
            <>
              {accountIdView}
              {amountView}
            </>
          )}
        </Flex>

        <Flex justify="between" width="100%" height="18px">
          {timeView}
          {isWeb3Wallet ? txIdView : statusView}
        </Flex>
      </Flex>
    );
  };

  return (
    <>
      <DataFilter
        items={[
          {
            type: "picker",
            name: "target",
            options: options,
            value: target,
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
        className="oui-sticky oui-top-[44px] oui-z-10 oui-border-none oui-bg-base-10 oui-px-3 oui-py-2"
      />

      <ListView
        dataSource={dataSource}
        renderItem={renderHistoryItem}
        contentClassName="!oui-space-y-1"
        loadMore={loadMore}
        isLoading={isLoading}
        className="oui-px-1"
        emptyView={
          <Flex
            direction={"column"}
            height={"100%"}
            itemAlign={"center"}
            justify={"center"}
            mt={3}
          >
            <EmptyDataState />
            {dataSource?.length == 0 && (
              <Flex
                direction="row"
                itemAlign="center"
                justify="center"
                onClick={onDeposit}
                className="oui-mt-2 oui-h-4 oui-w-full oui-text-secondary"
              >
                <Text color="primary" size="2xs">
                  {t("common.deposit")}
                </Text>
                <ArrowRightShortIcon
                  className="oui-ms-0.5 oui-size-4 oui-text-primary oui-opacity-100"
                  color="primary"
                  size={16}
                  opacity={100}
                />
              </Flex>
            )}
          </Flex>
        }
      />
    </>
  );
};
