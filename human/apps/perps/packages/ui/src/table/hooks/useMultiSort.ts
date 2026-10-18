import { useCallback } from "react";
import { SortOrder, MultiFieldSort } from "../type";

export function useMultiSort(props: {
  columnKey: string;
  onSort?: (fieldKey: string, sortOrder?: SortOrder) => void;
  initialSort?: MultiFieldSort[];
  currentSorting?: { sortKey: string; sort: SortOrder };
}) {
  const { onSort, currentSorting } = props;

  const handleFieldSort = useCallback(
    (fieldKey: string) => {
      // Check if this field is currently sorted
      const isCurrentField = currentSorting?.sortKey === fieldKey;
      let newOrder: SortOrder | undefined;

      if (!isCurrentField) {
        // Not currently sorted, start with asc
        newOrder = "asc";
      } else if (currentSorting.sort === "asc") {
        // Currently asc, change to desc
        newOrder = "desc";
      } else {
        // Currently desc, clear sorting
        newOrder = undefined;
      }

      // Call external handler
      onSort?.(fieldKey, newOrder);
    },
    [currentSorting, onSort],
  );

  const getFieldSort = useCallback(
    (fieldKey: string) => {
      // Only return sort info if this field is currently being sorted
      if (currentSorting?.sortKey === fieldKey) {
        return { sortKey: fieldKey, sort: currentSorting.sort };
      }
      return undefined;
    },
    [currentSorting],
  );

  return {
    handleFieldSort,
    getFieldSort,
  };
}
