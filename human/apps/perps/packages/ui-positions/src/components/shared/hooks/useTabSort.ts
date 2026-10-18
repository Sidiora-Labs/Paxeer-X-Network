import { useCallback } from "react";
import { useSessionStorage } from "@paxeer-network/hooks";
import { SortType } from "../../../types/types";

export enum PositionsTabName {
  Positions = "positions",
  PositionHistory = "positionHistory",
}

export function useTabSort(options: { storageKey: string }) {
  const [tabSort, setTabSort] = useSessionStorage(options.storageKey, {
    [PositionsTabName.Positions]: {
      sortKey: "unrealized_pnl",
      sortOrder: "desc",
    },
    [PositionsTabName.PositionHistory]: {
      sortKey: "close_timestamp",
      sortOrder: "desc",
    },
  } as Record<PositionsTabName, SortType>);

  const onTabSort = useCallback(
    (type: PositionsTabName) => (sort?: SortType) => {
      setTabSort({ ...tabSort, [type]: sort });
    },
    [tabSort, setTabSort],
  );

  return {
    tabSort,
    onTabSort,
  };
}
