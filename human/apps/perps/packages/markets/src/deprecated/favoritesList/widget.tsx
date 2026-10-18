import { DataTableClassNames } from "@paxeer-network/ui";
import { GetColumns } from "../../type";
import { useFavoritesListScript } from "./favoritesList.script";
import { FavoritesList } from "./favoritesList.ui";

export type FavoritesListWidgetProps = {
  getColumns?: GetColumns;
  collapsed?: boolean;
  tableClassNames?: DataTableClassNames;
  rowClassName?: string;
};

export const FavoritesListWidget: React.FC<FavoritesListWidgetProps> = (
  props,
) => {
  const state = useFavoritesListScript();
  return <FavoritesList {...state} {...props} />;
};
