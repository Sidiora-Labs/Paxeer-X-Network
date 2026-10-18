import { FC } from "react";
import { SortingState } from "@tanstack/react-table";
import { cnBase } from "tailwind-variants";
import { useMultiSort } from "./hooks/useMultiSort";
import { AscendingIcon, DescendingIcon, SortingIcon } from "./icons";
import { Column, SortOrder } from "./type";

export type MultiSortHeaderProps = {
  column: Column<any>;
  className?: string;
  sorting?: SortingState;
};

export const MultiSortHeader: FC<MultiSortHeaderProps> = ({
  column,
  className,
  sorting,
}) => {
  const { multiSort, title } = column;

  // Extract current sorting state
  const currentSorting = sorting?.[0]
    ? {
        sortKey: sorting[0].id,
        sort: sorting[0].desc ? ("desc" as SortOrder) : ("asc" as SortOrder),
      }
    : undefined;

  const { handleFieldSort, getFieldSort } = useMultiSort({
    columnKey: column.dataIndex,
    onSort: multiSort?.onSort,
    initialSort: multiSort?.initialSort,
    currentSorting,
  });

  if (!multiSort) {
    return <span>{title}</span>;
  }

  return (
    <div
      className={cnBase(
        "oui-inline-flex oui-items-center oui-gap-x-1",
        className,
      )}
    >
      {multiSort.fields.map((field, index) => {
        const fieldSort = getFieldSort(field.sortKey);

        return (
          <div
            key={field.sortKey}
            className={cnBase(
              "oui-inline-flex oui-items-center oui-gap-x-1",
              "oui-cursor-pointer oui-select-none hover:oui-text-base-contrast-80",
              field.className,
            )}
            onClick={
              multiSort.onSort
                ? () => handleFieldSort(field.sortKey)
                : undefined
            }
          >
            <span className="oui-whitespace-nowrap">
              {field.label || field.sortKey}
            </span>
            <SortIndicator sortDirection={fieldSort?.sort} />
            {index < multiSort.fields.length - 1 && (
              <span className="oui-text-base-contrast-36">/</span>
            )}
          </div>
        );
      })}
    </div>
  );
};

const SortIndicator: FC<{ sortDirection?: "asc" | "desc" }> = ({
  sortDirection,
}) => {
  if (sortDirection === "asc") {
    return <AscendingIcon />;
  }
  if (sortDirection === "desc") {
    return <DescendingIcon />;
  }
  return <SortingIcon />;
};
