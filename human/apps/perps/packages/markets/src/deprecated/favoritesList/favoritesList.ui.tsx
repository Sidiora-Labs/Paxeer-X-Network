import { FC } from "react";
import { Box, cn, DataTable } from "@paxeer-network/ui";
import { CollapseMarkets } from "../../components/collapseMarkets";
import { FavoritesTabWidget } from "../../components/favoritesTabs";
import { useMarketsContext } from "../../components/marketsProvider";
import { useSideMarketsColumns } from "../../components/sideMarkets/column";
import { UseFavoritesListReturn } from "./favoritesList.script";
import type { FavoritesListWidgetProps } from "./widget";

export type FavoritesListProps = UseFavoritesListReturn &
  FavoritesListWidgetProps;

export const FavoritesList: FC<FavoritesListProps> = (props) => {
  const { dataSource, favorite, onSort, loading, getColumns, collapsed } =
    props;

  const { symbol, onSymbolChange } = useMarketsContext();

  const sideColumns = useSideMarketsColumns(favorite, true);

  const columns =
    typeof getColumns === "function" ? getColumns(favorite, true) : sideColumns;

  if (collapsed) {
    return <CollapseMarkets dataSource={dataSource} />;
  }

  return (
    <>
      <Box px={3} className="oui-my-[6px]">
        <FavoritesTabWidget favorite={favorite} size="sm" />
      </Box>

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
    </>
  );
};
