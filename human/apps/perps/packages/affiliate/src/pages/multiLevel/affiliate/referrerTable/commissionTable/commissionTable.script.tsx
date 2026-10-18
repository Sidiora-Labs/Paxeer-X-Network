import { useMemo, useState } from "react";
import { format, subDays } from "date-fns";
import { useAccount } from "@paxeer-network/hooks";
import { AccountStatusEnum } from "@paxeer-network/types";
import { TableSort, usePagination, useScreen } from "@paxeer-network/ui";
import { useReferralHistory } from "../../../../../hooks/useReferralHistory";
import { DateRange } from "../../../../../utils/types";

export type CommissionDataType = {
  date: string;
  code: string;
  volume: number;
  commission: number;
  directBonusRebate: number;
  address: string;
};

export type UseCommissionTableScriptProps = {
  enabled?: boolean;
};

export const useCommissionTableScript = (
  props: UseCommissionTableScriptProps = {},
) => {
  const { enabled = true } = props;
  const { isMobile } = useScreen();
  const { state } = useAccount();

  const [dateRange, setDateRange] = useState<DateRange>({
    from: subDays(new Date(), 30),
    to: new Date(),
  });

  const { page, pageSize, parsePagination } = usePagination();

  const [sort, setSort] = useState<{
    key: keyof CommissionDataType;
    order: "asc" | "desc";
  } | null>(null);

  const [data, { meta, isLoading: isDataLoading }] = useReferralHistory({
    enabled: enabled && state.status >= AccountStatusEnum.EnableTrading,
    startDate: dateRange.from
      ? format(dateRange.from, "yyyy-MM-dd")
      : undefined,
    endDate: dateRange.to ? format(dateRange.to, "yyyy-MM-dd") : undefined,
    page: isMobile ? undefined : page,
    size: isMobile ? undefined : pageSize,
    fetchAll: isMobile,
  });

  const pagination = useMemo(() => {
    return parsePagination(meta);
  }, [meta, parsePagination]);

  const commissionData = useMemo(() => {
    if (!data) return [];

    const rows: CommissionDataType[] = data.map((row) => ({
      date: row.date,
      code: row.referral_code,
      volume: row.volume,
      commission: row.referral_rebate,
      directBonusRebate: row.direct_bonus_rebate,
      address: row.user_address,
    }));

    if (!sort) return rows;

    rows.sort((a, b) => {
      const valA = a[sort.key];
      const valB = b[sort.key];
      if (valA < valB) return sort.order === "asc" ? -1 : 1;
      if (valA > valB) return sort.order === "asc" ? 1 : -1;
      return 0;
    });

    return rows;
  }, [data, sort]);

  const onSort = (sort?: TableSort) => {
    if (!sort) {
      setSort(null);
      return;
    }
    const sortKey = sort.sortKey as keyof CommissionDataType;
    if (sortKey && (sortKey === "volume" || sortKey === "commission")) {
      setSort({
        key: sortKey,
        order: sort.sort === "asc" ? "asc" : "desc",
      });
    }
  };

  return {
    commissionData,
    dateRange,
    setDateRange,
    pagination,
    onSort,
    isLoading:
      state.status < AccountStatusEnum.EnableTrading ||
      !enabled ||
      isDataLoading,
  };
};

export type CommissionTableScriptReturns = ReturnType<
  typeof useCommissionTableScript
>;
