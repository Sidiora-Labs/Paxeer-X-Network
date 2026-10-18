import { useMemo } from "react";
import { usePrivateQuery } from "../../usePrivateQuery";

type UserStatistics = {
  perp_trading_volume_last_24_hours?: number;
  perp_trading_volume_today?: number;
};

export const useUserStatistics = () => {
  const { data, isValidating } = usePrivateQuery<UserStatistics>(
    "/v1/client/statistics",
    {
      revalidateOnFocus: false,
    },
  );

  return useMemo(
    () => [data ?? null, { isValidating }] as const,
    [data, isValidating],
  );
};

export type UseUserStatisticsReturn = ReturnType<typeof useUserStatistics>;
