import { useEffect, useMemo, useState } from "react";
import { MarketsType, useMarkets } from "@paxeer-network/hooks";
import { useMarketsContext } from "../../components/marketsProvider";
import { searchBySymbol, useSort } from "../../utils";

export type UseFavoritesListReturn = ReturnType<typeof useFavoritesListScript>;

export const useFavoritesListScript = () => {
  const [data, favorite] = useMarkets(MarketsType.FAVORITES);
  const [loading, setLoading] = useState(true);

  const { favorites, selectedFavoriteTab } = favorite;

  const { searchValue } = useMarketsContext();

  const { onSort, getSortedList } = useSort();

  const dataSource = useMemo(() => {
    const filterList = favorites
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
      ?.filter((item) => !!item);

    const searchResults = searchBySymbol(filterList, searchValue, "base");
    return getSortedList(searchResults);
  }, [data, selectedFavoriteTab, favorites, searchValue, getSortedList]);

  useEffect(() => {
    setLoading(false);
  }, [data]);

  return {
    loading,
    dataSource,
    favorite,
    onSort,
  };
};
