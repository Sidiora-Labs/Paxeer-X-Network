import {
  ColumnSort,
  createColumnHelper,
  getPaginationRowModel,
  PaginationTableState,
  SortingState,
  SortingTableState,
  TableOptions,
  OnChangeFn,
} from "@tanstack/react-table";
import { PaginationMeta, Column, TableSort, SortOrder } from "./type";

function resolveFixedDirection(
  fixed: Column["fixed"],
  isRTL: boolean,
): "left" | "right" | null {
  if (!fixed) {
    return null;
  }

  if (fixed === "left" || fixed === "right") {
    return fixed;
  }

  if (fixed === "start") {
    return isRTL ? "right" : "left";
  }

  return isRTL ? "left" : "right";
}

/**
 * Compare two values intelligently for table sorting
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
    numeric: false, // Disable numeric sorting for pure string comparison
    caseFirst: "upper",
  });
};

/**
 * Custom sorting function for TanStack Table
 */
const customSortingFn = (rowA: any, rowB: any, columnId: string): number => {
  const aValue = rowA.getValue(columnId);
  const bValue = rowB.getValue(columnId);
  return compareValues(aValue, bValue);
};

/**
 * Create a sorting comparator for multiSort fields
 */
const createMultiSortComparator = (sortId: string, isDesc: boolean) => {
  return (a: any, b: any) => {
    // Support nested field access (e.g., "user.name" or "data.price.value")
    const getNestedValue = (obj: any, path: string) => {
      return path.split(".").reduce((current, key) => {
        return current && current[key] !== undefined ? current[key] : undefined;
      }, obj);
    };

    const aValue = getNestedValue(a, sortId) ?? a[sortId];
    const bValue = getNestedValue(b, sortId) ?? b[sortId];

    // Keep missing and invalid values at the bottom in both directions.
    const aMissing =
      aValue == null ||
      (typeof aValue === "number" && !Number.isFinite(aValue));
    const bMissing =
      bValue == null ||
      (typeof bValue === "number" && !Number.isFinite(bValue));
    if (aMissing && bMissing) return 0;
    if (aMissing) return 1;
    if (bMissing) return -1;

    // Numeric comparison
    const aNum = Number(aValue);
    const bNum = Number(bValue);
    if (Number.isFinite(aNum) && Number.isFinite(bNum)) {
      return isDesc ? bNum - aNum : aNum - bNum;
    }

    // String comparison with enhanced support for complex strings
    const aStr = String(aValue);
    const bStr = String(bValue);
    const comparison = aStr.localeCompare(bStr, undefined, {
      sensitivity: "base",
      numeric: true, // Handle numeric sequences in strings like "PERP_1000PEPE_USDC"
    });

    return isDesc ? -comparison : comparison;
  };
};

export const Transform = {
  columns: (
    columns: Column[],
    sorting?: SortingState,
    setSorting?: OnChangeFn<SortingState>,
  ) => {
    const columnHelper = createColumnHelper<any>();

    return columns?.map((column, index) => {
      // Create a copy of column to avoid mutating the original
      let columnCopy = { ...column };

      const { dataIndex, onSort, multiSort } = columnCopy;

      // For multi-field sorting, provide default onSort if not provided
      const isMultiSort = Boolean(multiSort);

      // If multiSort is enabled but no onSort is provided, create default handler
      if (isMultiSort && multiSort && !multiSort.onSort && setSorting) {
        columnCopy = {
          ...columnCopy,
          multiSort: {
            ...multiSort,
            onSort: (fieldKey: string, sortOrder?: SortOrder) => {
              if (!setSorting) return;

              if (!sortOrder) {
                // Clear sorting
                setSorting([]);
              } else {
                // Set sorting for the field with custom sorting function
                setSorting([
                  {
                    id: fieldKey,
                    desc: sortOrder === "desc",
                  },
                ]);
              }
            },
          },
        };
      }

      const commonProps = {
        meta: columnCopy,
        size: columnCopy.width || 100,
      };

      return columnHelper.accessor(dataIndex, {
        header: () => columnCopy.title,
        enableSorting: onSort && !isMultiSort ? true : false, // Disable sorting for multiSort columns
        sortingFn:
          typeof onSort === "function"
            ? (rowA, rowB, columnId) => onSort?.(rowA.original, rowB.original)
            : customSortingFn,
        ...commonProps,
      });
    });
  },

  dataSource: <T>(
    dataSource: ReadonlyArray<T> | T[],
    columns: Column[],
    sorting?: SortingState,
  ): ReadonlyArray<T> | T[] => {
    if (
      !dataSource ||
      dataSource.length === 0 ||
      !sorting ||
      sorting.length === 0
    ) {
      return dataSource || [];
    }

    const currentSort = sorting[0];
    const sortId = currentSort.id;

    // Check if this sort ID matches any multiSort field
    const multiSortColumn = columns.find((col) =>
      col.multiSort?.fields?.some((field) => field.sortKey === sortId),
    );

    if (!multiSortColumn) {
      return dataSource; // Not a multiSort field, return original data
    }

    // Create a copy and sort using the optimized comparator
    const sortedData = [...dataSource].sort(
      createMultiSortComparator(sortId, currentSort.desc),
    );

    return sortedData;
  },

  columnPinning: (columns: Column[], isRTL = false) => {
    const left: string[] = [];
    const right: string[] = [];
    columns?.map((column) => {
      const fixed = resolveFixedDirection(column.fixed, isRTL);
      if (fixed === "left") {
        left.push(column.dataIndex);
      } else if (fixed === "right") {
        right.push(column.dataIndex);
      }
    });

    return { left, right };
  },

  pagination: (pagination?: PaginationMeta) => {
    let state = {} as PaginationTableState;
    let config = {} as Pick<
      TableOptions<any>,
      "getPaginationRowModel" | "onPaginationChange"
    >;
    if (pagination) {
      state = {
        pagination: {
          pageIndex: pagination.page - 1,
          pageSize: pagination.pageSize,
        },
      };
      config = {
        // no need to set onPaginationChange
        // onPaginationChange,
        // load client-side pagination code
        getPaginationRowModel: getPaginationRowModel(),
      };
    }
    return { state, config };
  },
  // sorting: (onSort?: (sort?: TableSort) => void, initialSort?: TableSort) => {
  //   let state = {} as SortingTableState;
  //   let config = {} as Pick<TableOptions<any>, "onSortingChange">;

  //   if (typeof onSort === "function") {
  //     config = {
  //       onSortingChange: (updaterFn: any) => {
  //         const sorting = updaterFn() as ColumnSort[];
  //         const { id, desc } = sorting[0];
  //         onSort({ sortKey: id, sort: desc ? "desc" : "asc" });
  //       },
  //     };
  //   }

  //   if (initialSort) {
  //     state = {
  //       sorting: [
  //         {
  //           id: initialSort.sortKey,
  //           desc: initialSort.sort === "desc",
  //         },
  //       ],
  //     } as SortingTableState;
  //   }

  //   return { state, config };
  // },
};
