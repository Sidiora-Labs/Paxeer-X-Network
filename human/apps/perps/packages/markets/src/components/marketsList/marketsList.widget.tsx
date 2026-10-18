import { ReactNode } from "react";
import { Favorite, FavoriteTab } from "@paxeer-network/hooks";
import { DataTableClassNames } from "@paxeer-network/ui";
import {
  FavoriteInstance,
  GetColumns,
  SortType,
  MarketsTabName,
} from "../../type";
import { useMarketsListScript } from "./marketsList.script";
import { MarketsList } from "./marketsList.ui";

export type MarketsListWidgetProps = {
  type: MarketsTabName;
  getColumns?: GetColumns;
  panelSize?: "small" | "middle" | "large";
  tableClassNames?: DataTableClassNames;
  rowClassName?: string;
  initialSort?: SortType;
  onSort?: (sort?: SortType) => void;
  renderHeader?: (favorite: FavoriteInstance) => ReactNode;
  dataFilter?: (
    data: any[],
    options: { favorites: Favorite[]; selectedFavoriteTab: FavoriteTab },
  ) => any[];
  emptyView?: ReactNode;
};

export const MarketsListWidget: React.FC<MarketsListWidgetProps> = (props) => {
  const state = useMarketsListScript(props);
  return (
    <MarketsList
      {...state}
      initialSort={props.initialSort}
      getColumns={props.getColumns}
      panelSize={props.panelSize}
      tableClassNames={props.tableClassNames}
      rowClassName={props.rowClassName}
      renderHeader={props.renderHeader}
      emptyView={props.emptyView}
    />
  );
};
