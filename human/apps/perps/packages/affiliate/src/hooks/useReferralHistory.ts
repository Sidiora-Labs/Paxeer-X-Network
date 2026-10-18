import { useMemo } from "react";
import { usePrivateQuery } from "@paxeer-network/hooks";

type Params = {
  /** default is 10 */
  size?: number;
  /** YYYY-MM-dd */
  startDate?: string;
  /** YYYY-MM-dd */
  endDate?: string;
  initialSize?: number;
  page?: number;
  fetchAll?: boolean;
  /** disable network request when false */
  enabled?: boolean;
};

type ReferralHistoryItem = {
  account_id: string;
  user_address: string;
  referral_code: string;
  volume: number;
  referral_rebate: number;
  direct_bonus_rebate: number;
  date: string;
};

type ReferralHistoryPageResponse = {
  rows: ReferralHistoryItem[];
  meta?: {
    total: number;
    records_per_page: number;
    current_page: number;
  };
};

export const useReferralHistory = (params: Params) => {
  const { size, startDate, endDate, page, fetchAll, enabled = true } = params;

  const query = useMemo(() => {
    if (!enabled) return null;
    const search = new URLSearchParams({
      ...(startDate ? { start_date: startDate } : {}),
      ...(endDate ? { end_date: endDate } : {}),
      ...(!fetchAll && page !== undefined ? { page: String(page) } : {}),
      ...(!fetchAll && size !== undefined ? { size: String(size) } : {}),
    });

    const qs = search.toString();
    return qs
      ? `/v1/referral/referral_history?${qs}`
      : "/v1/referral/referral_history";
  }, [enabled, endDate, fetchAll, page, size, startDate]);

  const response = usePrivateQuery<
    ReferralHistoryPageResponse | { data?: ReferralHistoryPageResponse }
  >(query, {
    formatter: (data) => data,
    revalidateOnFocus: false,
  });

  const loadMore = () => {};

  const meta = useMemo(():
    | {
        total: number;
        records_per_page: number;
        current_page: number;
      }
    | undefined => {
    const payload =
      (response.data as { data?: ReferralHistoryPageResponse })?.data ??
      (response.data as ReferralHistoryPageResponse | undefined);
    if (payload?.meta) return payload.meta;
    if (!payload?.rows) return undefined;
    return {
      total: payload.rows.length,
      records_per_page: payload.rows.length,
      current_page: 1,
    };
  }, [response.data]);

  const total = useMemo(() => {
    if (typeof meta?.total === "number") return meta.total;
    return 0;
  }, [meta]);

  const flattenRows = useMemo((): ReferralHistoryItem[] | null => {
    if (!response.data) return null;
    const payload =
      (response.data as { data?: ReferralHistoryPageResponse })?.data ??
      (response.data as ReferralHistoryPageResponse | undefined);
    return payload?.rows ?? null;
  }, [response.data]);

  return [
    flattenRows,
    {
      total,
      isLoading: response.isLoading,
      refresh: response.mutate,
      loadMore,
      meta,
    },
  ] as const;
};
