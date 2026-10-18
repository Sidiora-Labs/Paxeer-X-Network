import { ReactNode } from "react";
import { useFavoritesListFullScript } from "./favoritesListFull.script";
import { FavoritesListFull } from "./favoritesListFull.ui";

export type FavoritesListFullWidgetProps = {
  emptyView?: ReactNode;
};

export const FavoritesListFullWidget: React.FC<FavoritesListFullWidgetProps> = (
  props,
) => {
  const state = useFavoritesListFullScript();
  return <FavoritesListFull {...state} emptyView={props.emptyView} />;
};
