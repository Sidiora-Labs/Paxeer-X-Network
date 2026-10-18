import { useMemo } from "react";
import { useTranslation, i18n } from "@paxeer-network/i18n";
import { API } from "@paxeer-network/types";
import { Flex, type Column, TokenIcon } from "@paxeer-network/ui";

export const useColumns = () => {
  const { t } = useTranslation();

  const columns = useMemo(() => {
    return [
      {
        title: t("common.token"),
        dataIndex: "token",
        width: 80,
        // rule: "symbol",
        render: (value, record) => {
          return (
            <Flex gapX={2}>
              <TokenIcon name={value} size="xs" />
              <span>{value}</span>
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
        title: t("common.status"),
        dataIndex: "status",
        width: 120,
        formatter(value, record, index) {
          switch (value) {
            case "CREATED":
            case "SPLIT":
              return t("assetHistory.status.processing");
            case "COMPLETED":
            default:
              return t("assetHistory.status.completed");
          }
        },
      },

      {
        title: t("common.type"),
        dataIndex: "type",
        width: 80,
        formatter: (value: any) => {
          switch (value) {
            case "REFERRER_REBATE":
              return t(
                "portfolio.overview.distribution.type.referralCommission",
              );
            case "REFEREE_REBATE":
              return t("portfolio.overview.distribution.type.refereeRebate");
            case "BROKER_FEE":
              return t("portfolio.overview.distribution.type.brokerFee");
            default:
              return "-";
          }
        },
      },
      {
        title: t("common.amount"),
        dataIndex: "amount",
        width: 80,
        // rule: "price",
      },
    ] as Column<API.FundingFeeRow & { annual_rate: number }>[];
  }, [t]);

  return columns;
};
