import React from "react";
import { useAccount } from "@paxeer-network/hooks";
import { useTranslation } from "@paxeer-network/i18n";
import { API, EMPTY_LIST } from "@paxeer-network/types";
import { Flex, TokenIcon, Text, toast, Badge } from "@paxeer-network/ui";
import type { Column } from "@paxeer-network/ui";
import { capitalizeString } from "@paxeer-network/utils";

export const useColumns = () => {
  const { t } = useTranslation();
  const onCopy = () => {
    toast.success(t("common.copy.copied"));
  };
  const { state } = useAccount();
  const sub = state.subAccounts ?? [];
  const columns = React.useMemo<Column<API.TransferHistoryRow>[]>(() => {
    return [
      {
        title: t("common.token"),
        dataIndex: "token",
        width: 80,
        render(val: string) {
          return (
            <Flex gapX={2}>
              <TokenIcon name={val} size="xs" />
              <span>{val}</span>
            </Flex>
          );
        },
      },
      {
        title: t("common.time"),
        dataIndex: "updated_time",
        width: 120,
        rule: "date",
      },
      {
        title: `${t("transfer.internalTransfer.from")} (${t("common.accountId")})`,
        dataIndex: "from_account_id",
        render(val: string) {
          const isMainAccount = val === state.mainAccountId;
          const subAccount = sub.find((item) => item.id === val);
          return (
            <Flex itemAlign="start" py={2} gap={1} direction="column">
              <Text.formatted onCopy={onCopy} copyable rule="address">
                {val}
              </Text.formatted>
              <Badge className="oui-select-none" color="neutral" size="xs">
                {isMainAccount
                  ? t("common.mainAccount")
                  : subAccount?.description || t("common.subAccount")}
              </Badge>
            </Flex>
          );
        },
      },
      {
        title: `${t("transfer.internalTransfer.to")} (${t("common.accountId")})`,
        dataIndex: "to_account_id",
        render(val: string) {
          const isMainAccount = val === state.mainAccountId;
          const subAccount = sub.find((item) => item.id === val);
          return (
            <Flex itemAlign="start" py={2} gap={1} direction="column">
              <Text.formatted onCopy={onCopy} copyable rule="address">
                {val}
              </Text.formatted>
              <Badge className="oui-select-none" color="neutral" size="xs">
                {isMainAccount
                  ? t("common.mainAccount")
                  : subAccount?.description || t("common.subAccount")}
              </Badge>
            </Flex>
          );
        },
      },
      {
        title: t("common.status"),
        dataIndex: "status",
        width: 120,
        render(val: string) {
          return capitalizeString(val);
        },
      },
      {
        title: t("common.amount"),
        dataIndex: "amount",
        width: 80,
      },
    ];
  }, [t, state.mainAccountId, sub]);
  return columns;
};
