import { useEffect, useMemo, useState } from "react";
import { MarketsType, useMarkets } from "@paxeer-network/hooks";
import { usePagination } from "@paxeer-network/ui";
import { searchBySymbol, useSort } from "../../utils";
import { useMarketsContext } from "../marketsProvider";
import { useLowVolumeMarketFilter } from "../shared/lowVolumeMarketFilter";
import { filterMarketsByLowVolume } from "../shared/marketVolumeFilter";

export type UseFavoritesListFullReturn = ReturnType<
  typeof useFavoritesListFullScript
>;

export const useFavoritesListFullScript = () => {
  const { pageSize, setPage, pagination } = usePagination({
    pageSize: 10,
  });
  const [data, favorite] = useMarkets(MarketsType.FAVORITES);
  const [loading, setLoading] = useState(true);

  const { favorites, selectedFavoriteTab } = favorite;

  const { searchValue } = useMarketsContext();
  const { showLowVolume } = useLowVolumeMarketFilter();

  const { onSort, getSortedList } = useSort();

  const filterData = useMemo(() => {
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

    return searchBySymbol(filterList, searchValue, "base-type");
  }, [data, selectedFavoriteTab, favorites, searchValue]);

  const { dataSource, isHiddenByLowVolumeFilter } = useMemo(() => {
    const { visibleMarkets, allHidden } = filterMarketsByLowVolume(
      filterData,
      showLowVolume,
    );

    return {
      dataSource: getSortedList(visibleMarkets),
      isHiddenByLowVolumeFilter: allHidden,
    };
  }, [filterData, getSortedList, showLowVolume]);

  useEffect(() => {
    setLoading(false);
  }, [favorites]);

  useEffect(() => {
    setPage(1);
  }, [searchValue, showLowVolume]);

  return {
    loading,
    dataSource,
    isHiddenByLowVolumeFilter,
    favorite,
    onSort,
    pagination,
  };
};
