import { usePrivateQuery } from "@paxeer-network/hooks";
import { RebateRow, resolveRebateRows, VolumeRow } from "./traderData";

export type QueryRange = { startDate: string; endDate: string };

function datedUrl(path: string, range: QueryRange) {
  const params = new URLSearchParams({
    start_date: range.startDate,
    end_date: range.endDate,
  });
  return `${path}?${params}`;
}

export function useTraderRebates(range: QueryRange) {
  const valid = range.startDate <= range.endDate;
  const multi = usePrivateQuery<RebateRow[]>(
    valid
      ? datedUrl("/v1/referral/multi_level/referee_rebate_summary", range)
      : null,
    { revalidateOnFocus: false, shouldRetryOnError: false },
  );
  const checkLegacy =
    valid &&
    (!!multi.error || (Array.isArray(multi.data) && multi.data.length === 0));
  const legacy = usePrivateQuery<RebateRow[]>(
    checkLegacy ? datedUrl("/v1/referral/referee_rebate_summary", range) : null,
    { revalidateOnFocus: false, shouldRetryOnError: false },
  );
  const waitingForLegacy = checkLegacy && !legacy.data && !legacy.error;
  const result = waitingForLegacy
    ? {}
    : resolveRebateRows(multi.data, multi.error, legacy.data, legacy.error);

  return {
    rows: result.rows,
    error: result.error,
    isLoading: valid && (multi.isLoading || waitingForLegacy),
  };
}

export function useTraderVolume(range: QueryRange) {
  return usePrivateQuery<VolumeRow[]>(
    range.startDate <= range.endDate
      ? datedUrl("/v1/volume/user/daily", range)
      : null,
    { revalidateOnFocus: false, shouldRetryOnError: false },
  );
}
