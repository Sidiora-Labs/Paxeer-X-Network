import { useCallback } from "react";
import { Favorite, FavoriteTab } from "@paxeer-network/hooks";
import { Box } from "@paxeer-network/ui";
import { FavoriteInstance, MarketsTabName } from "../../../type";
import { FavoritesEmpty } from "../../favoritesEmpty";
import { FavoritesTabWidget } from "../../favoritesTabs";
import { useMarketsContext } from "../../marketsProvider";

export function useFavoritesProps() {
  const { searchValue } = useMarketsContext();
  const renderHeader = useCallback((favorite: FavoriteInstance) => {
    return (
      <Box px={3} className="oui-my-[6px]">
        <FavoritesTabWidget favorite={favorite} size="sm" />
      </Box>
    );
  }, []);

  const dataFilter = useCallback(
    (
      data: any[],
      options: { favorites: Favorite[]; selectedFavoriteTab: FavoriteTab },
    ) => {
      const { favorites, selectedFavoriteTab } = options;

      return favorites
        ?.filter(
          (item) =>
            item.tabs?.findIndex((tab) => tab.id === selectedFavoriteTab.id) !==
            -1,
        )
        ?.map((fav) => {
          const index = data?.findIndex((item) => item.symbol === fav.name);
          if (index !== -1) {
            return data[index];
          }
          return null;
        })
        ?.filter((item) => item);
    },
    [],
  );

  const getFavoritesProps = useCallback(
    (type: MarketsTabName) => {
      if (type === MarketsTabName.Favorites) {
        return { renderHeader, dataFilter };
      }
      return {};
    },
    [renderHeader, dataFilter],
  );

  const renderEmptyView = useCallback(
    (options: {
      type: MarketsTabName;
      onClick: () => void;
      className?: string;
    }) => {
      if (options.type === MarketsTabName.Favorites && !searchValue) {
        return (
          <FavoritesEmpty
            onClick={options.onClick}
            className={options.className}
          />
        );
      }

      return undefined;
    },
    [searchValue],
  );

  return {
    renderHeader,
    dataFilter,
    getFavoritesProps,
    renderEmptyView,
  };
}
