import { FC, useMemo } from "react";
import { useTranslation } from "@paxeer-network/i18n";
import { DataFilter } from "@paxeer-network/ui";
import { AuthGuardDataTable } from "@paxeer-network/ui-connector";
import { useColumns } from "./column";
import { type useDistributionHistoryHookReturn } from "./useDataSource.script";

type FundingHistoryProps = {} & useDistributionHistoryHookReturn;

export const DistributionHistoryDesktop: FC<FundingHistoryProps> = (props) => {
  const { dataSource, queryParameter, onFilter, isLoading, isValidating } =
    props;
  const columns = useColumns();
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

  return (
    <>
      <DataFilter
        items={[
          {
            type: "select",
            name: "type",
            options: TYPES,
            value: type,
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
      />
      <AuthGuardDataTable
        bordered
        columns={columns}
        dataSource={dataSource}
        loading={isLoading}
        // isValidating={isValidating}
        className="oui-font-semibold"
        classNames={{
          root: "oui-h-[calc(100%_-_49px)]",
        }}
        pagination={props.pagination}
      />
    </>
  );
};
