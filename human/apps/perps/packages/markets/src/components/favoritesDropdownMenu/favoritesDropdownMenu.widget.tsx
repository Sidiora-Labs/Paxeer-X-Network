import { PropsWithChildren } from "react";
import {
  useFavoritesDropdownMenuScript,
  UseFavoritesDropdownMenuScriptOptions,
} from "./favoritesDropdownMenu.script";
import { FavoritesDropdownMenu } from "./favoritesDropdownMenu.ui";

export type FavoritesDropdownMenuWidgetProps =
  PropsWithChildren<UseFavoritesDropdownMenuScriptOptions>;

export const FavoritesDropdownMenuWidget: React.FC<
  FavoritesDropdownMenuWidgetProps
> = (props) => {
  const state = useFavoritesDropdownMenuScript(props);
  return (
    <FavoritesDropdownMenu {...state}>{props.children}</FavoritesDropdownMenu>
  );
};
