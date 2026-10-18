import { useEffect, useMemo, useState } from "react";
import { MarketsType, useMarkets } from "@paxeer-network/hooks";
import { TableSort, usePagination } from "@paxeer-network/ui";
import { searchBySymbol, useSort } from "../../utils";
import { useMarketsContext } from "../marketsProvider";
import { useLowVolumeMarketFilter } from "../shared/lowVolumeMarketFilter";
import { filterMarketsByLowVolume } from "../shared/marketVolumeFilter";
import { MarketsListFullWidgetProps } from "./marketsListFull.widget";

export type UseMarketsListFullScriptOptions = MarketsListFullWidgetProps;

export type UseMarketsListFullReturn = ReturnType<
  typeof useMarketsListFullScript
>;

const MarketsTypeMap = {
  all: MarketsType.ALL,
  crypto: MarketsType.CRYPTO,
  new: MarketsType.NEW_LISTING,
  rwa: MarketsType.RWA,
  preTge: MarketsType.PRE_TGE,
};

export type MarketsListFullType = keyof typeof MarketsTypeMap;

export const useMarketsListFullScript = (
  options: UseMarketsListFullScriptOptions,
) => {
  const { type = "all", dataFilter } = options;
  const [loading, setLoading] = useState(true);
  const { setPage, pagination } = usePagination({
    pageSize: 100,
  });

  const [data, favorite] = useMarkets(MarketsTypeMap[type]);

  const { searchValue } = useMarketsContext();
  const { showLowVolume } = useLowVolumeMarketFilter();

  const { onSort, getSortedList, sort } = useSort(options.initialSort);

  const { dataSource, isHiddenByLowVolumeFilter } = useMemo(() => {
    const filtered = typeof dataFilter === "function" ? dataFilter(data) : data;
    const searchList = searchBySymbol(filtered, searchValue, "base-type");
    const { visibleMarkets, allHidden } = filterMarketsByLowVolume(
      searchList,
      showLowVolume,
    );

    return {
      dataSource: getSortedList(visibleMarkets),
      isHiddenByLowVolumeFilter: allHidden,
    };
  }, [data, dataFilter, getSortedList, searchValue, showLowVolume]);

  useEffect(() => {
    setLoading(false);
  }, [data]);

  useEffect(() => {
    setPage(1);
  }, [searchValue, showLowVolume]);

  useEffect(() => {
    // Only all markets store sort
    if (type === "all") {
      favorite.updateTabsSortState("all", sort?.sortKey!, sort?.sortOrder!);
    }
  }, [sort, type]);

  const initialSort = useMemo(() => {
    const sortStore = type === "all" ? favorite.tabSort?.all : undefined;

    return {
      sortKey: sortStore?.sortKey || options?.initialSort?.sortKey,
      sort: sortStore?.sortOrder || options?.initialSort?.sortOrder,
    } as TableSort;
  }, [favorite.tabSort, options.initialSort, type]);

  return {
    loading,
    dataSource,
    isHiddenByLowVolumeFilter,
    favorite,
    onSort,
    pagination,
    initialSort,
  };
};
