import { useEffect, useMemo, useState } from "react";
import { format, subDays } from "date-fns";
import {
  RefferalAPI,
  useRefereeInfo,
  useReferralRebateSummary,
} from "@paxeer-network/hooks";
import { PaginationMeta, usePagination, useScreen } from "@paxeer-network/ui";
import { DateRange } from "../../../utils/types";

export type ListReturns<T> = {
  data: T;
  dateRange?: DateRange;
  setDateRange: React.Dispatch<React.SetStateAction<DateRange | undefined>>;
  isLoading?: boolean;
  loadMore?: () => void;
  pagination: PaginationMeta;
};

export type CommissionAndRefereesReturns = {
  commission: ListReturns<RefferalAPI.ReferralRebateSummary[] | undefined>;
  referees: ListReturns<RefferalAPI.RefereeInfoItem[] | undefined>;
};

export const useCommissionAndRefereesScript =
  (): CommissionAndRefereesReturns => {
    const commission = useCommissionDataScript();
    const referees = useRefereesDataScript();

    return {
      commission,
      referees,
    };
  };

const useCommissionDataScript = (): ListReturns<
  RefferalAPI.ReferralRebateSummary[] | undefined
> => {
  const [commissionRange, setCommissionRange] = useState<DateRange | undefined>(
    {
      from: subDays(new Date(), 90),
      to: subDays(new Date(), 1),
    },
  );

  const { isMobile } = useScreen();

  const { page, pageSize, setPage, parsePagination } = usePagination();

  const [commissionData, { refresh, isLoading, loadMore, meta }] =
    useReferralRebateSummary({
      startDate:
        commissionRange?.from !== undefined
          ? format(commissionRange.from, "yyyy-MM-dd")
          : undefined,
      endDate:
        commissionRange?.to !== undefined
          ? format(commissionRange.to, "yyyy-MM-dd")
          : undefined,
      size: pageSize,
      page: !isMobile ? page : undefined,
    });

  useEffect(() => {
    refresh();
  }, [commissionRange]);

  // const loadMore = () => {
  //   setPage(page + 1);
  // };

  const pagination = useMemo(
    () => parsePagination(meta),
    [parsePagination, meta],
  );

  useEffect(() => {
    setPage(1);
  }, [commissionRange]);

  return {
    data: commissionData || undefined,
    pagination,
    dateRange: commissionRange,
    setDateRange: setCommissionRange,
    isLoading,
    loadMore,
  };
};

const useRefereesDataScript = (): ListReturns<
  RefferalAPI.RefereeInfoItem[] | undefined
> => {
  const [commissionRange, setCommissionRange] = useState<DateRange | undefined>(
    {
      from: subDays(new Date(), 90),
      to: subDays(new Date(), 1),
    },
  );

  const { isMobile } = useScreen();

  const { page, pageSize, setPage, parsePagination } = usePagination();

  const [commissionData, { refresh, isLoading, loadMore, meta }] =
    useRefereeInfo({
      startDate:
        commissionRange?.from !== undefined
          ? format(commissionRange.from, "yyyy-MM-dd")
          : undefined,
      endDate:
        commissionRange?.to !== undefined
          ? format(commissionRange.to, "yyyy-MM-dd")
          : undefined,
      size: pageSize,
      page: !isMobile ? page : undefined,
      sort: "descending_code_binding_time",
    });

  useEffect(() => {
    refresh();
  }, [commissionRange]);

  // const loadMore = () => {
  //   setPage(page + 1);
  // };

  const pagination = useMemo(
    () => parsePagination(meta),
    [parsePagination, meta],
  );

  useEffect(() => {
    setPage(1);
  }, [commissionRange]);

  return {
    data: commissionData || undefined,
    pagination,
    dateRange: commissionRange,
    setDateRange: setCommissionRange,
    isLoading,
    loadMore,
  };
};
