import React, { useMemo } from "react";
import { produce } from "immer";
import { useAccount } from "@paxeer-network/hooks";
import { useTranslation } from "@paxeer-network/i18n";
import type { API } from "@paxeer-network/types";
import {
  DataFilter,
  formatAddress,
  ListView,
  Flex,
  Text,
  Badge,
  TokenIcon,
  toast,
  Statistic,
} from "@paxeer-network/ui";
import type { SelectOption } from "@paxeer-network/ui/src/select/withOptions";
import { capitalizeString } from "@paxeer-network/utils";
import type { useTransferHistoryHookReturn } from "./useDataSource.script";

type TransferHistoryMobileProps = useTransferHistoryHookReturn;

export enum AccountType {
  ALL = "All accounts",
  MAIN = "Main accounts",
}

export const TransferHistoryMobileUI: React.FC<
  Readonly<TransferHistoryMobileProps & ReturnType<typeof useAccount>>
> = (props) => {
  const {
    dataSource,
    queryParameter,
    state,
    isMainAccount,
    isLoading,
    selectedAccount,
    onFilter,
    pagination,
  } = props;
  const { side, dateRange } = queryParameter;

  const { t } = useTranslation();

  const ALL_ACCOUNTS: SelectOption = {
    label: t("common.allAccount"),
    value: AccountType.ALL,
  };

  const MAIN_ACCOUNT: SelectOption = {
    label: t("common.mainAccount"),
    value: AccountType.MAIN,
  };

  const subAccounts = state.subAccounts ?? [];

  const memoizedOptions = useMemo(() => {
    if (Array.isArray(subAccounts) && subAccounts.length) {
      return [
        ALL_ACCOUNTS,
        MAIN_ACCOUNT,
        ...subAccounts.map<SelectOption>((value) => ({
          value: value.id,
          label: value?.description || formatAddress(value?.id),
        })),
      ];
    }
    return [ALL_ACCOUNTS, MAIN_ACCOUNT];
  }, [subAccounts]);

  const loadMore = () => {
    if (dataSource && dataSource.length < (pagination?.count || 0)) {
      pagination?.onPageSizeChange?.(pagination?.pageSize + 50);
    }
  };

  const onCopy = () => {
    toast.success(t("common.copy.copied"));
  };

  const renderHistoryItem = (item: API.TransferHistoryRow) => {
    const isFromMainAccount = item.from_account_id === state.mainAccountId;
    const isToMainAccount = item.to_account_id === state.mainAccountId;
    const fromSubAccount = subAccounts.find(
      (acc) => acc.id === item.from_account_id,
    );
    const toSubAccount = subAccounts.find(
      (acc) => acc.id === item.to_account_id,
    );

    return (
      <Flex
        p={2}
        direction="column"
        gapY={2}
        className="oui-rounded-xl oui-bg-base-9 oui-font-semibold"
      >
        {/* Header: Date and Status */}
        <Flex direction="row" justify="between" width="100%">
          <Text.formatted
            rule="date"
            className="oui-text-base-contrast-36 oui-text-2xs"
          >
            {item.updated_time}
          </Text.formatted>
          <Text size="sm" className="oui-text-base-contrast-80">
            {capitalizeString(item.status)}
          </Text>
        </Flex>

        {/* Divider */}
        <div className="oui-w-full oui-h-[1px] oui-bg-base-6" />

        {/* Token and Amount */}
        <Flex direction="row" justify="between" width="100%">
          <Flex direction="column" itemAlign={"start"}>
            <Text className="oui-text-base-contrast-36 oui-text-2xs">
              {t("common.token")}
            </Text>
            <Flex direction="row" itemAlign="center" gap={1}>
              <TokenIcon name={item.token} size="2xs" />
              <Text size="xs" className="oui-text-base-contrast-80">
                {item.token}
              </Text>
            </Flex>
          </Flex>
          <Flex direction="column" itemAlign="end">
            <Text className="oui-text-base-contrast-36 oui-text-2xs">
              {t("common.amount")}
            </Text>
            <Text.numeral
              dp={6}
              className="oui-text-base-contrast-80 oui-text-xs"
            >
              {item.amount}
            </Text.numeral>
          </Flex>
        </Flex>

        {/* From and To Account IDs */}
        <Flex direction="row" justify="between" width="100%">
          <Flex direction="column" gap={1}>
            <Text className="oui-text-base-contrast-36 oui-text-2xs">
              {t("transfer.internalTransfer.from")} ({t("common.accountId")})
            </Text>
            <Flex direction="column" gap={1} itemAlign={"start"}>
              <Flex direction="row" itemAlign="center" gap={1}>
                <Text.formatted
                  rule="address"
                  copyable
                  onCopy={onCopy}
                  className="oui-text-base-contrast-80 oui-text-xs"
                >
                  {item.from_account_id}
                </Text.formatted>
              </Flex>
              <Badge color="neutral" size="xs">
                {isFromMainAccount
                  ? t("common.mainAccount")
                  : fromSubAccount?.description || t("common.subAccount")}
              </Badge>
            </Flex>
          </Flex>
          <Flex direction="column" itemAlign="end" gap={1}>
            <Text className="oui-text-base-contrast-36 oui-text-2xs">
              {t("transfer.internalTransfer.to")} ({t("common.accountId")})
            </Text>
            <Flex direction="column" itemAlign="end" gap={1}>
              <Flex direction="row" itemAlign="center" gap={1}>
                <Text.formatted
                  rule="address"
                  copyable
                  onCopy={onCopy}
                  className="oui-text-base-contrast-80 oui-text-xs"
                >
                  {item.to_account_id}
                </Text.formatted>
              </Flex>
              <Badge color="neutral" size="xs">
                {isToMainAccount
                  ? t("common.mainAccount")
                  : toSubAccount?.description || t("common.subAccount")}
              </Badge>
            </Flex>
          </Flex>
        </Flex>
      </Flex>
    );
  };

  return (
    <>
      <div className="oui-px-3">
        <DataFilter
          onFilter={onFilter}
          items={produce(
            [
              {
                type: "select",
                name: "side",
                value: side,
                options: [
                  { value: "OUT", label: t("common.outflow") },
                  { value: "IN", label: t("common.inflow") },
                ],
              },
              {
                type: "range",
                name: "dateRange",
                value: {
                  from: dateRange[0],
                  to: dateRange[1],
                },
              },
            ],
            (draft) => {
              if (isMainAccount) {
                draft.unshift({
                  type: "select",
                  name: "account",
                  value: selectedAccount as any,
                  options: memoizedOptions,
                });
              }
            },
          )}
        />
      </div>
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
