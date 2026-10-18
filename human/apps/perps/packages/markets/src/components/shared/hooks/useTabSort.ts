import { useCallback } from "react";
import { useSessionStorage } from "@paxeer-network/hooks";
import { SortType, MarketsTabName } from "../../../type";

export function useTabSort(options: { storageKey: string }) {
  const [tabSort, setTabSort] = useSessionStorage(options.storageKey, {
    [MarketsTabName.All]: {
      sortKey: "24h_amount",
      sortOrder: "desc",
    },
  } as Record<string, SortType>);

  const onTabSort = useCallback(
    (type: MarketsTabName | string) => (sort?: SortType) => {
      setTabSort({ ...tabSort, [type]: sort });
    },
    [tabSort],
  );

  return {
    tabSort,
    onTabSort,
  };
}
