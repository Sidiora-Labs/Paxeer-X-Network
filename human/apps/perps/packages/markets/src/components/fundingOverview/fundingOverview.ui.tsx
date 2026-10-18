import { FC, useState } from "react";
import { DataTable, cn } from "@paxeer-network/ui";
import { useFundingOverviewColumns } from "./columns";
import { FundingOverviewReturn } from "./fundingOverview.script";

export type FundingOverviewProps = FundingOverviewReturn;

export const FundingOverview: FC<FundingOverviewProps> = (props) => {
  const { dataSource, isLoading, pagination, onSort } = props;
  const [selectedPeriod, setSelectedPeriod] = useState("1dPositive");

  const columns = useFundingOverviewColumns(selectedPeriod, setSelectedPeriod);

  return (
    <DataTable
      columns={columns}
      dataSource={dataSource}
      loading={isLoading}
      bordered
      onRow={() => {
        return {
          className: cn("oui-h-[48px] oui-cursor-pointer"),
        };
      }}
      classNames={{
        header: "oui-h-12",
      }}
      pagination={pagination}
      onSort={onSort}
      manualSorting
      generatedRowKey={(record) => record.symbol}
    />
  );
};
