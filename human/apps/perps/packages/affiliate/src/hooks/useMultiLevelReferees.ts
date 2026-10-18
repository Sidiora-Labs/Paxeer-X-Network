import { useMemo } from "react";
import { useAccount, usePrivateQuery } from "@paxeer-network/hooks";
import { AccountStatusEnum } from "@paxeer-network/types";

export type RefereeDataType = {
  account_id: string;
  address: string;
  bind_code: string;
  bind_type: string;
  description?: string | null;
  code_binding_time: number;
  is_default_rate: boolean;
  referral_rebate_rate: number;
  referee_rebate_rate: number;
  direct_rebate: number;
  direct_bonus_rebate: number;
  indirect_rebate: number;
  network_size: number;
  volume: number;
  commission: number;
  direct_invites: number;
  indirect_invites: number;
  direct_volume: number;
  indirect_volume: number;
};

export type RefereePaginationMeta = {
  total: number;
  current_page: number;
  records_per_page: number;
};

type RefereesResponse = {
  rows?: RefereeDataType[];
  data?: { rows?: RefereeDataType[]; meta?: RefereePaginationMeta };
  success?: boolean;
  timestamp?: number;
  meta?: RefereePaginationMeta;
};

export type UseMultiLevelRefereesParams = {
  /** disable network request when false */
  enabled?: boolean;
  /** fetch without pagination params (used by mobile) */
  fetchAll?: boolean;
  page?: number;
  pageSize?: number;
};

export const useMultiLevelReferees = (
  params: UseMultiLevelRefereesParams = {},
) => {
  const { enabled = true, fetchAll = false, page, pageSize } = params;
  const { state } = useAccount();

  const canQuery = enabled && state.status >= AccountStatusEnum.EnableTrading;

  const queryKey = useMemo(() => {
    if (!canQuery) return null;
    if (fetchAll) return "/v1/referral/multi_level/referee_list";
    if (page !== undefined && pageSize !== undefined) {
      return `/v1/referral/multi_level/referee_list?page=${page}&size=${pageSize}`;
    }
    return "/v1/referral/multi_level/referee_list";
  }, [canQuery, fetchAll, page, pageSize]);

  const response = usePrivateQuery<RefereesResponse>(queryKey, {
    formatter: (payload) => {
      const rows = payload.rows ?? payload.data?.rows ?? [];
      const meta = payload.meta ?? payload.data?.meta;
      return {
        ...payload,
        meta,
        rows: rows.map((row: RefereeDataType) => ({
          ...row,
          network_size: row.direct_invites + row.indirect_invites,
          volume: row.direct_volume + row.indirect_volume,
          commission:
            row.direct_rebate + row.indirect_rebate + row.direct_bonus_rebate,
        })),
      };
    },
    revalidateOnFocus: false,
  });

  return {
    ...response,
    isLoading:
      state.status < AccountStatusEnum.EnableTrading ||
      !enabled ||
      response.isLoading,
    rows: response.data?.rows ?? [],
    meta: response.data?.meta,
  };
};

export type UseMultiLevelRefereesReturns = ReturnType<
  typeof useMultiLevelReferees
>;
