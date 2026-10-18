import React from "react";
import { useTranslation } from "@paxeer-network/i18n";
import type { API } from "@paxeer-network/types";
import { Flex, TokenIcon, Text, cn } from "@paxeer-network/ui";
import type { Column } from "@paxeer-network/ui";
import { capitalizeString } from "@paxeer-network/utils";

export const useColumns = () => {
  const { t } = useTranslation();
  const columns = React.useMemo<Column<API.StrategyVaultHistoryRow>[]>(() => {
    return [
      {
        title: t("common.type"),
        dataIndex: "type",
        className: (record) => {
          if (record?.type === "deposit") {
            return "oui-text-success";
          }
          if (record?.type === "withdrawal") {
            return "oui-text-danger";
          }
          return "";
        },
        render(val: string) {
          if (val === "deposit") {
            return t("common.deposit");
          }
          if (val === "withdrawal") {
            return t("common.withdraw");
          }
          return null;
        },
      },
      {
        title: t("portfolio.overview.vaultName"),
        dataIndex: "vaultName",
      },
      {
        title: t("common.token"),
        dataIndex: "token",
        render(val: string) {
          return (
            <Flex justify="start" itemAlign="center" gap={2}>
              <TokenIcon name={val} />
              {val}
            </Flex>
          );
        },
      },
      {
        title: t("common.time"),
        dataIndex: "created_time",
        rule: "date",
      },
      {
        title: t("common.status"),
        dataIndex: "status",
        render(val: string) {
          return capitalizeString(val);
        },
      },
      {
        title: t("common.amount"),
        dataIndex: "amount_change",
        render(val: number) {
          return (
            <Text.numeral
              showIdentifier
              className={cn(
                "oui-select-none",
                val >= 0 ? "oui-text-success" : "oui-text-danger",
              )}
            >
              {val}
            </Text.numeral>
          );
        },
      },
    ];
  }, [t]);
  return columns;
};
