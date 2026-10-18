import useSWR from "swr";
import type { SWRHook, SWRResponse } from "swr";
import { AccountStatusEnum } from "@paxeer-network/types";
import { signatureMiddleware } from "./middleware/signatureMiddleware";
import { useAccount } from "./useAccount";
import { fetcher, useQueryOptions } from "./utils/fetcher";

/**
 * usePrivateQuery
 * @description for private api
 * @param query
 * @param options
 */
export const usePrivateQuery = <T>(
  query: Parameters<SWRHook>[0],
  options?: useQueryOptions<T>,
): SWRResponse<T> => {
  const { formatter, ...swrOptions } = options || {};
  const { state } = useAccount();
  const middleware = Array.isArray(options?.use) ? (options?.use ?? []) : [];

  return useSWR<T>(
    () =>
      query &&
      (state.status >= AccountStatusEnum.EnableTrading ||
        state.status === AccountStatusEnum.EnableTradingWithoutConnected)
        ? [query, state.accountId]
        : null,
    (url: string, init: RequestInit) => {
      return fetcher(url, init, { formatter });
    },
    {
      ...swrOptions,
      use: [signatureMiddleware, ...middleware],
      onError: () => {},
    },
  );
};
