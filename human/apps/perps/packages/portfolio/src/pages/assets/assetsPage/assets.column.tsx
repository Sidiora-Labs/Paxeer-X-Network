import React from "react";
import { useAppStore } from "@paxeer-network/hooks";
import { useTranslation } from "@paxeer-network/i18n";
import { Button, Flex, Text, TokenIcon } from "@paxeer-network/ui";
import type { Column } from "@paxeer-network/ui";

export interface ColumnsOptions {
  onTransfer?: (accountId: string, token: string) => void;
  onConvert?: (accountId: string, token: string) => void;
}

// Define the enhanced holding interface with calculated fields
interface EnhancedHolding {
  token: string;
  holding: number;
  indexPrice: number;
  assetValue: number;
  collateralRatio: number;
  collateralContribution: number;
  account_id: string;
}

export const useAssetsColumns = (options: ColumnsOptions) => {
  const { t } = useTranslation();
  const tokensInfo = useAppStore((state) => state.tokensInfo);
  const { onTransfer, onConvert } = options;
  const columns = React.useMemo<Column[]>(() => {
    return [
      {
        title: t("portfolio.overview.column.token"),
        dataIndex: "token",
        align: "left",
        width: 150,
        render(val: string) {
          return (
            <Flex itemAlign="center" gap={2}>
              <TokenIcon name={val} />
              {val}
            </Flex>
          );
        },
      },
      {
        title: t("portfolio.overview.column.qty"),
        dataIndex: "holding",
        align: "left",
        width: 140,
        render(val: number, record) {
          const findToken = tokensInfo?.find(
            ({ token }) => token === record.token,
          );
          return (
            <Text.numeral dp={findToken?.decimals ?? 6} padding={false}>
              {val}
            </Text.numeral>
          );
        },
      },
      {
        title: t("portfolio.overview.column.indexPrice"),
        dataIndex: "indexPrice",
        align: "left",
        width: 140,
        render(val: number) {
          return (
            <Text.numeral rule="price" dp={6} currency="$" padding={false}>
              {val}
            </Text.numeral>
          );
        },
      },
      {
        title: t("portfolio.overview.column.assetValue"),
        dataIndex: "assetValue",
        align: "left",
        width: 140,
        render(val: number) {
          return (
            <Text.assetValue rule="price" dp={6} currency="$" padding={false}>
              {val}
            </Text.assetValue>
          );
        },
      },
      {
        title: t("portfolio.overview.column.collateralRatio"),
        dataIndex: "collateralRatio",
        align: "left",
        width: 140,
        render(val: number) {
          return (
            <Text.numeral dp={2} suffix="%">
              {val * 100}
            </Text.numeral>
          );
        },
      },
      {
        title: t("transfer.deposit.collateralContribution"),
        dataIndex: "collateralContribution",
        align: "left",
        width: 140,
        render(val: number) {
          return (
            <Text.collateral rule="price" dp={6} currency="$" padding={false}>
              {val}
            </Text.collateral>
          );
        },
      },
      {
        title: null,
        dataIndex: "account_id",
        align: "center",
        width: 180,
        render(id: string, record: EnhancedHolding) {
          const { token } = record;
          return (
            <Flex itemAlign="center" justify="end" gap={3}>
              {token !== "USDC" && typeof onConvert === "function" && (
                <Button
                  size={"sm"}
                  variant={"outlined"}
                  color={"secondary"}
                  onClick={() => onConvert?.(id, token)}
                >
                  {t("transfer.convert")}
                </Button>
              )}
              {typeof onTransfer === "function" && (
                <Button
                  size={"sm"}
                  variant={"outlined"}
                  color={"secondary"}
                  onClick={() => onTransfer?.(id, token)}
                >
                  {t("common.transfer")}
                </Button>
              )}
            </Flex>
          );
        },
      },
    ];
  }, [t, tokensInfo, onTransfer, onConvert]);
  return columns;
};
