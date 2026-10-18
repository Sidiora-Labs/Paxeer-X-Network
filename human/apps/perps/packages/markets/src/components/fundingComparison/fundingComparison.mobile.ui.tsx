import type { FC } from "react";
import { cn } from "@paxeer-network/ui";
import { DataTable } from "@paxeer-network/ui";
import { useFundingColumns } from "./columns";
import { FundingComparisonReturn } from "./fundingComparison.script";

export const MobileFundingComparison: FC<FundingComparisonReturn> = (props) => {
  const columns = useFundingColumns();

  return (
    <DataTable
      columns={columns}
      dataSource={props.dataSource}
      loading={props.isLoading}
      onRow={() => {
        return {
          className: cn("oui-h-[34px] oui-cursor-pointer"),
        };
      }}
      onSort={props.onSort}
      classNames={{
        header: "oui-h-9",
        body: "oui-text-2xs",
      }}
      manualSorting
    />
  );
};
