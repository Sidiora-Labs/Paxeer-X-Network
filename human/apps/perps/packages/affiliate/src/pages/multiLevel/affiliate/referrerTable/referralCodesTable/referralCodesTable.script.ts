import { useMemo, useState } from "react";
import { TableSort } from "@paxeer-network/ui";
import {
  ReferralCodesRow,
  useMultiLevelReferralCodes,
} from "../../../../../hooks/useMultiLevelReferralCodes";

type ReferralCodesSortableKey =
  | "total_invites"
  | "total_volume"
  | "total_rebate";
type ReferralCodesSortableRow = ReferralCodesRow &
  Record<ReferralCodesSortableKey, number | undefined>;

export type UseReferralCodesTableScriptProps = {
  enabled?: boolean;
};

export const useReferralCodesTableScript = (
  props: UseReferralCodesTableScriptProps = {},
) => {
  const { enabled = true } = props;
  const { codes, copyCode } = useMultiLevelReferralCodes();
  const [sort, setSort] = useState<{
    key: ReferralCodesSortableKey;
    order: "asc" | "desc";
  } | null>(null);

  const sortedCodes = useMemo(() => {
    if (!enabled) return undefined;
    if (!codes || !sort) return codes;

    const sortableCodes = codes.map((code, index) => ({
      code,
      index,
    }));

    sortableCodes.sort((a, b) => {
      const valA = Number((a.code as ReferralCodesSortableRow)[sort.key] ?? 0);
      const valB = Number((b.code as ReferralCodesSortableRow)[sort.key] ?? 0);
      if (valA < valB) return sort.order === "asc" ? -1 : 1;
      if (valA > valB) return sort.order === "asc" ? 1 : -1;
      return a.index - b.index;
    });

    return sortableCodes.map((item) => item.code) as ReferralCodesRow[];
  }, [codes, enabled, sort]);

  const onSort = (sort?: TableSort) => {
    if (!sort) {
      setSort(null);
      return;
    }
    const sortKey = sort.sortKey as ReferralCodesSortableKey;
    if (
      sortKey &&
      (sortKey === "total_invites" ||
        sortKey === "total_volume" ||
        sortKey === "total_rebate")
    ) {
      setSort({
        key: sortKey,
        order: sort.sort === "asc" ? "asc" : "desc",
      });
    }
  };

  return {
    codes,
    sortedCodes,
    copyCode,
    onSort,
    isLoading: enabled && !codes,
  };
};

export type ReferralCodesTableScriptReturns = ReturnType<
  typeof useReferralCodesTableScript
>;
