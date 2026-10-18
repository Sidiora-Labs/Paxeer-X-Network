import { useMemo } from "react";
import { useBadgeBySymbol } from "@paxeer-network/hooks";
import { useTranslation } from "@paxeer-network/i18n";
import { API } from "@paxeer-network/types";
import { Flex, Text, type Column } from "@paxeer-network/ui";

const SymbolBadge = (props: { symbol: string }) => {
  const { brokerId, brokerName, brokerNameRaw } = useBadgeBySymbol(
    props.symbol,
  );

  return (
    <Text.symbolBadge
      badge={brokerName ?? brokerId ?? undefined}
      fullName={brokerNameRaw}
      className="oui-cursor-pointer"
      showIcon
    >
      {props.symbol}
    </Text.symbolBadge>
  );
};

export const useFundingHistoryColumns = () => {
  const { t } = useTranslation();

  const columns = useMemo(() => {
    return [
      {
        title: t("common.symbol"),
        dataIndex: "symbol",
        width: 80,
        render: (value: string) => <SymbolBadge symbol={value} />,
      },
      {
        title: t("common.time"),
        dataIndex: "created_time",
        width: 120,
        rule: "date",
      },
      {
        title: `${t("funding.fundingRate")} / ${t("funding.annualRate")}`,
        dataIndex: "funding_rate",
        width: 80,
        render: (value: any, record) => {
          return (
            <Flex gap={1}>
              {/* <span>{`${record.funding_rate * 100}%`}</span> */}
              <Text.numeral rule={"percentages"} dp={6}>
                {record.funding_rate}
              </Text.numeral>
              <span>/</span>
              {/* <span>{`${record.annual_rate * 10}%`}</span> */}
              <Text.numeral rule={"percentages"} dp={6}>
                {record.annual_rate}
              </Text.numeral>
            </Flex>
          );
        },
      },
      {
        title: t("funding.paymentType"),
        dataIndex: "payment_type",
        width: 80,
        render: (value: any) => {
          switch (value) {
            case "Pay":
            case "Paid":
              return t("funding.paymentType.paid");
            case "Receive":
            case "Received":
              return t("funding.paymentType.received");
            default:
              return value;
          }
        },
      },
      {
        title: `${t("funding.fundingFee")} (USDC)`,
        dataIndex: "funding_fee",
        width: 80,
        rule: "price",
        formatter(value, record, index) {
          return Number(value) * -1;
        },
        numeralProps: {
          coloring: true,
          showIdentifier: true,
          ignoreDP: true,
        },
      },
    ] as Column<API.FundingFeeRow & { annual_rate: number }>[];
  }, [t]);

  return columns;
};
