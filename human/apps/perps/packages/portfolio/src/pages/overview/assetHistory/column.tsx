/* eslint-disable @typescript-eslint/no-explicit-any */
import { useMemo } from "react";
import { useTranslation } from "@paxeer-network/i18n";
import {
  AssetHistorySideEnum,
  AssetHistoryStatusEnum,
} from "@paxeer-network/types";
import {
  capitalizeFirstLetter,
  Text,
  Flex,
  TokenIcon,
  toast,
  type Column,
} from "@paxeer-network/ui";
import { Decimal } from "@paxeer-network/utils";

type Options = {
  chainsInfo: any[];
  isDeposit: boolean;
  isWeb3Wallet: boolean;
};

export const useAssetHistoryColumns = (options: Options) => {
  const { chainsInfo, isDeposit, isWeb3Wallet } = options;
  const { t } = useTranslation();

  const onCopy = (e: any) => {
    e.preventDefault();
    e.stopPropagation();
    toast.success(t("common.copy.copied"));
  };

  const columns = useMemo<Column[]>(() => {
    const txIdColumn: Column = {
      title: t("common.txId"),
      dataIndex: "tx_id",
      width: 120,
      render: (value, record) => {
        if (!value) {
          return <div className="oui-text-base-contrast-54">-</div>;
        }
        const chainInfo = chainsInfo?.find(
          (item) => parseInt(record.chain_id) === parseInt(item.chain_id),
        );
        const explorer_base_url = chainInfo?.explorer_base_url;
        const href = `${explorer_base_url}/tx/${value}`;
        return (
          <a href={href} target="_blank" rel="noreferrer">
            {/* <Tooltip content={value} delayDuration={0}> */}
            <Text.formatted
              copyable={!!value}
              rule="txId"
              className="oui-underline oui-decoration-line-16 oui-decoration-dashed oui-underline-offset-4"
              onCopy={onCopy}
            >
              {value}
            </Text.formatted>
            {/* </Tooltip> */}
          </a>
        );
      },
    };

    const accountIdColumn: Column = {
      title: t("common.accountId"),
      dataIndex: "account_id",
      width: 120,
      render: (value, record) => {
        const accountId = isDeposit
          ? record.from_account_id
          : record.to_account_id;

        return (
          <Text.formatted rule="address" copyable={!!accountId} onCopy={onCopy}>
            {accountId}
          </Text.formatted>
        );
      },
    };

    return [
      {
        title: t("common.token"),
        dataIndex: "token",
        width: 80,
        render: (value) => {
          return (
            <Flex gapX={1}>
              <TokenIcon name={value} size="xs" />
              <span>{value}</span>
            </Flex>
          );
        },
      },
      {
        title: t("common.time"),
        dataIndex: "created_time",
        width: 80,
        rule: "date",
      },
      isWeb3Wallet ? txIdColumn : accountIdColumn,
      {
        title: t("common.status"),
        dataIndex: "trans_status",
        width: 100,
        render: (value, recoed) => {
          value = isWeb3Wallet ? value : recoed.status;
          const statusMap = {
            [AssetHistoryStatusEnum.NEW]: t("assetHistory.status.pending"),
            [AssetHistoryStatusEnum.PENDING]: t("assetHistory.status.pending"),
            [AssetHistoryStatusEnum.CONFIRM]: t("assetHistory.status.confirm"),
            [AssetHistoryStatusEnum.PROCESSING]: t(
              "assetHistory.status.processing",
            ),
            [AssetHistoryStatusEnum.COMPLETED]: t(
              "assetHistory.status.completed",
            ),
            [AssetHistoryStatusEnum.FAILED]: t("assetHistory.status.failed"),
          };
          return (
            statusMap[value as keyof typeof statusMap] ||
            capitalizeFirstLetter(value?.toLowerCase())
          );
        },
      },
      {
        title: t("common.amount"),
        dataIndex: "amount",
        width: 100,
        rule: "price",
        render: (value, record) => {
          const netAmount = new Decimal(value).minus(record.fee ?? 0);
          const rowIsDeposit = record.side
            ? record.side !== AssetHistorySideEnum.WITHDRAW
            : isDeposit;
          return (
            <Text.numeral
              dp={record.decimals}
              rule="price"
              coloring
              showIdentifier
              padding={false}
            >
              {rowIsDeposit ? netAmount.toNumber() : netAmount.neg().toNumber()}
            </Text.numeral>
          );
        },
      },
    ];
  }, [t, chainsInfo, isDeposit, isWeb3Wallet]);

  return columns;
};
