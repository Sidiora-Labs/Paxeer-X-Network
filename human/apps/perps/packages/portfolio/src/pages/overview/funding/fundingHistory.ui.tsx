import { FC, useMemo } from "react";
import { useSymbolsInfo } from "@paxeer-network/hooks";
import { useTranslation } from "@paxeer-network/i18n";
import { DataFilter } from "@paxeer-network/ui";
import { AuthGuardDataTable } from "@paxeer-network/ui-connector";
import { useFundingHistoryColumns } from "./column";
import { type UseFundingHistoryReturn } from "./useDataSource.script";

type FundingHistoryProps = {} & UseFundingHistoryReturn;

export const FundingHistoryDesktop: FC<FundingHistoryProps> = (props) => {
  const { dataSource, queryParameter, onFilter, isLoading } = props;
  const columns = useFundingHistoryColumns();
  const symbols = useSymbolsInfo();
  const { symbol, dateRange } = queryParameter;
  const { t } = useTranslation();

  const options = useMemo(() => {
    return [
      {
        label: t("common.all"),
        value: "All",
      },
      ...Object.keys(symbols).map((symbol) => {
        const s = symbol.split("_")[1];
        return {
          label: s,
          value: symbol,
        };
      }),
    ];
  }, [t, symbols]);

  return (
    <>
      <DataFilter
        items={[
          {
            type: "select",
            name: "symbol",
            isCombine: true,
            options,
            value: symbol,
            valueFormatter: (value) => {
              const option = options.find((item) => item.value === value);
              return option?.label || value;
            },
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
        generatedRowKey={(record) => `${record.updated_time}`}
        classNames={{ root: "oui-h-[calc(100%_-_49px)]" }}
        pagination={props.pagination}
      />
    </>
  );
};
