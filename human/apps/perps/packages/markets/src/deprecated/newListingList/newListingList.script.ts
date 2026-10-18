import { useEffect, useMemo, useState } from "react";
import { MarketsType, useMarkets } from "@paxeer-network/hooks";
import { useMarketsContext } from "../../components/marketsProvider";
import { searchBySymbol, useSort } from "../../utils";

export type UseNewListingListReturn = ReturnType<
  typeof useNewListingListScript
>;

export const useNewListingListScript = () => {
  const [data, favorite] = useMarkets(MarketsType.NEW_LISTING);
  const [loading, setLoading] = useState(true);

  const { searchValue } = useMarketsContext();

  const { onSort, getSortedList } = useSort();

  const dataSource = useMemo(() => {
    const searchResults = searchBySymbol(data, searchValue, "base");
    return getSortedList(searchResults);
  }, [data, searchValue, getSortedList]);

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
