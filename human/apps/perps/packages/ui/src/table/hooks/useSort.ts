import { useEffect, useState } from "react";
import { SortingState } from "@tanstack/react-table";
import { TableSort } from "../type";

export function useSort(props: {
  onSort?: (sort?: TableSort) => void;
  initialSort?: TableSort;
}) {
  const { onSort, initialSort } = props;

  const [sorting, setSorting] = useState<SortingState>(
    initialSort
      ? [
          {
            id: initialSort.sortKey,
            desc: initialSort.sort === "desc",
          },
        ]
      : []
  );

  useEffect(() => {
    const { id, desc } = sorting[0] || {};
    onSort?.(id ? { sortKey: id, sort: desc ? "desc" : "asc" } : undefined);
  }, [sorting, onSort]);

  return [sorting, setSorting] as const;
}
