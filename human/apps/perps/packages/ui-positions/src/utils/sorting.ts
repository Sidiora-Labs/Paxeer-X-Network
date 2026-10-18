import { useCallback, useEffect, useState } from "react";
import { TableSort } from "@paxeer-network/ui";
import { SortType } from "../types/types";

/**
 * Compare two values intelligently
 */
const compareValues = (aValue: any, bValue: any): number => {
  // Handle null/undefined values (always sort to bottom)
  if (aValue == null && bValue == null) return 0;
  if (aValue == null) return 1;
  if (bValue == null) return -1;

  // Convert to string first for type checking
  const aStr = String(aValue).trim();
  const bStr = String(bValue).trim();

  // More robust number detection - check if values can be converted to valid numbers
  const aNum = Number(aStr);
  const bNum = Number(bStr);
  const aIsNumber =
    !isNaN(aNum) && isFinite(aNum) && /^-?\d*\.?\d+([eE][+-]?\d+)?$/.test(aStr);
  const bIsNumber =
    !isNaN(bNum) && isFinite(bNum) && /^-?\d*\.?\d+([eE][+-]?\d+)?$/.test(bStr);

  if (aIsNumber && bIsNumber) {
    return aNum - bNum;
  }

  // Check if both are valid dates (ISO format or timestamp)
  const aIsDate = /^\d{4}-\d{2}-\d{2}/.test(aStr) || /^\d{13}$/.test(aStr);
  const bIsDate = /^\d{4}-\d{2}-\d{2}/.test(bStr) || /^\d{13}$/.test(bStr);

  if (aIsDate && bIsDate) {
    const aDate = new Date(aValue);
    const bDate = new Date(bValue);
    if (!isNaN(aDate.getTime()) && !isNaN(bDate.getTime())) {
      return aDate.getTime() - bDate.getTime();
    }
  }

  // String comparison - use localeCompare for proper string sorting
  return aStr.localeCompare(bStr, undefined, {
    sensitivity: "base",
    numeric: false,
  });
};

export function sortList(list: any[], sort?: SortType) {
  const { sortKey, sortOrder } = sort || {};
  const sortedList = [...(list || [])];

  if (sortKey && sortOrder) {
    // sort list
    sortedList.sort((a: any, b: any) => {
      const comparison = compareValues(a[sortKey], b[sortKey]);
      // Handle sort order: desc means reverse the comparison result
      return sortOrder === "desc" ? -comparison : comparison;
    });
  }
  return sortedList;
}

export function useSort(
  initialSort?: SortType,
  onSortChange?: (sort?: SortType) => void,
) {
  const [sort, setSort] = useState<SortType | undefined>(initialSort);

  const onSort = useCallback((options?: TableSort) => {
    const nextSort = options
      ? {
          sortKey: options.sortKey,
          sortOrder: options.sort,
        }
      : undefined;

    setSort(nextSort);
    onSortChange?.(nextSort);
    // initialSort, onSortChange is not needed to be in the dependency array, otherwise it will cause infinite loop
  }, []);

  const getSortedList = useCallback(
    (list: any[]) => sortList(list, sort),
    [sort],
  );

  return {
    sort,
    onSort,
    getSortedList,
  };
}
