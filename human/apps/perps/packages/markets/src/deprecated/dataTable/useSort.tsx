import { useState } from "react";
import { SortOrder } from "@paxeer-network/ui";
import type { TableProps } from ".";
import { AscendingIcon, DescendingIcon, SortingIcon } from "../../icons";

export function useSort(options: Pick<TableProps, "onSort" | "initialSort">) {
  const { initialSort } = options;

  const [sortKey, setSortKey] = useState<[string, SortOrder] | undefined>(
    initialSort ? [initialSort.sortKey, initialSort.sort] : undefined,
  );

  const onSort = (key: string) => {
    let _key: string, _order: SortOrder;
    const [prevKey, prevOrder] = sortKey || [];

    if (prevKey === key) {
      if (prevOrder === "desc") {
        _key = key;
        _order = "asc";
      }
    } else {
      _key = key;
      _order = "desc";
    }

    // @ts-ignore
    setSortKey(typeof _key === "undefined" ? undefined : [_key!, _order!]);

    if (typeof options.onSort === "function") {
      // @ts-ignore
      if (!!_key && !!_order) {
        options.onSort!({
          sortKey: key,
          sort: _order,
        });
      } else {
        options.onSort!();
      }
    }
  };

  const renderSortIndicator = (dataIndex: string) => {
    const [key, order] = sortKey || [];

    if (key === dataIndex) {
      return order === "asc" ? <AscendingIcon /> : <DescendingIcon />;
    }

    return <SortingIcon />;
  };

  return { sortKey, onSort, renderSortIndicator };
}
