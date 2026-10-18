import { useEffect, useMemo, useState } from "react";
import { MarketsType, useMarkets } from "@paxeer-network/hooks";
import { MarketsTabName } from "../../type";
import { searchBySymbol, useSort } from "../../utils";
import { useMarketsContext } from "../marketsProvider";
import { useLowVolumeMarketFilter } from "../shared/lowVolumeMarketFilter";
import { filterMarketsByLowVolume } from "../shared/marketVolumeFilter";
import { type MarketsListWidgetProps } from "./marketsList.widget";

export type MarketsListScriptOptions = MarketsListWidgetProps;

export type MarketsListScriiptReturn = ReturnType<typeof useMarketsListScript>;

const MarketsTypeMap = {
  [MarketsTabName.All]: MarketsType.ALL,
  [MarketsTabName.Crypto]: MarketsType.CRYPTO,
  [MarketsTabName.Community]: MarketsType.COMMUNITY,
  [MarketsTabName.Rwa]: MarketsType.RWA,
  [MarketsTabName.PreTge]: MarketsType.PRE_TGE,
  [MarketsTabName.Favorites]: MarketsType.FAVORITES,
  [MarketsTabName.Recent]: MarketsType.RECENT,
  [MarketsTabName.NewListing]: MarketsType.NEW_LISTING,
};

export const useMarketsListScript = (options: MarketsListScriptOptions) => {
  const [loading, setLoading] = useState(true);

  const [data, favorite] = useMarkets(
    MarketsTypeMap[options.type] || MarketsType.ALL,
  );

  const { favorites, selectedFavoriteTab } = favorite;

  const { searchValue } = useMarketsContext();
  const { showLowVolume } = useLowVolumeMarketFilter();

  const { onSort, getSortedList } = useSort(
    options.initialSort,
    options.onSort,
  );

  const { dataSource, isHiddenByLowVolumeFilter } = useMemo(() => {
    const filterList =
      typeof options.dataFilter === "function"
        ? options.dataFilter(data, { favorites, selectedFavoriteTab })
        : data;
    const searchResults = searchBySymbol(filterList, searchValue, "base");
    const { visibleMarkets, allHidden } = filterMarketsByLowVolume(
      searchResults,
      showLowVolume,
    );

    return {
      dataSource: getSortedList(visibleMarkets),
      isHiddenByLowVolumeFilter: allHidden,
    };
  }, [
    data,
    getSortedList,
    searchValue,
    options.dataFilter,
    favorites,
    selectedFavoriteTab,
    showLowVolume,
  ]);

  useEffect(() => {
    setLoading(false);
  }, [data]);

  const isFavoritesList = options.type === MarketsTabName.Favorites;

  return {
    loading,
    dataSource,
    isHiddenByLowVolumeFilter,
    favorite,
    onSort,
    isFavoritesList,
  };
};
