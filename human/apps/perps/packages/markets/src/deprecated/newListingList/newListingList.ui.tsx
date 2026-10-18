import { FC } from "react";
import { cn, DataTable } from "@paxeer-network/ui";
import { CollapseMarkets } from "../../components/collapseMarkets";
import { useMarketsContext } from "../../components/marketsProvider";
import { useSideMarketsColumns } from "../../components/sideMarkets/column";
import { UseNewListingListReturn } from "./newListingList.script";
import { NewListingListWidgetProps } from "./widget";

export type NewListingListProps = UseNewListingListReturn &
  NewListingListWidgetProps;

export const NewListingList: FC<NewListingListProps> = (props) => {
  const { dataSource, favorite, onSort, loading, getColumns, collapsed } =
    props;

  const { symbol, onSymbolChange } = useMarketsContext();

  const sideColumns = useSideMarketsColumns(favorite, false);

  const columns =
    typeof getColumns === "function"
      ? getColumns(favorite, false)
      : sideColumns;

  if (collapsed) {
    return <CollapseMarkets dataSource={dataSource} />;
  }

  return (
    <DataTable
      classNames={{
        root: props.tableClassNames?.root,
        body: props.tableClassNames?.body,
        header: cn("oui-h-9", props.tableClassNames?.header),
        scroll: props.tableClassNames?.scroll,
      }}
      columns={columns}
      dataSource={dataSource}
      loading={loading}
      onRow={(record, index) => {
        return {
          className: cn("oui-h-[53px]", props.rowClassName),
          onClick: () => {
            onSymbolChange?.(record);
            favorite.addToHistory(record);
          },
        };
      }}
      generatedRowKey={(record) => record.symbol}
      rowSelection={{ [symbol!]: true }}
      onSort={onSort}
      manualSorting
    />
  );
};
