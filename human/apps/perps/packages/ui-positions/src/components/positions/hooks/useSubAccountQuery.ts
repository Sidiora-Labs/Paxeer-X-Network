/* eslint-disable @typescript-eslint/no-explicit-any */
import {
  type SWRConfiguration,
  type SWRHook,
  type SWRResponse,
  type Middleware,
  useAccount,
  useSWR,
  fetcher,
  useConfig,
  useAccountInstance,
} from "@paxeer-network/hooks";
import { AccountStatusEnum } from "@paxeer-network/types";
import { getTimestamp } from "@paxeer-network/utils";

export type QueryOptions<T> = SWRConfiguration & {
  formatter?: (data: any) => T;
  accountId?: string | string[];
};

const signatureMiddleware = (
  account: ReturnType<typeof useAccountInstance>,
  accountId?: string | string[],
): Middleware => {
  // TODO(hooks): Move this hook call into the parent hook and pass apiBaseUrl in.
  const apiBaseUrl = useConfig("apiBaseUrl");
  // @ts-ignore
  return (useSWRNext: SWRHook) => {
    return (key, fetcher, config) => {
      try {
        const extendedFetcher = async (args: any) => {
          const url = Array.isArray(args) ? args[0] : args;
          const fullUrl = `${apiBaseUrl}${url}`;
          const signer = account.signer;
          const payload: MessageFactor = { method: "GET", url };
          const signature = await signer.sign(payload, getTimestamp());
          const ids = Array.isArray(accountId) ? accountId : [accountId];
          return Promise.all(
            ids.map((id) => {
              // @ts-ignore
              return fetcher(fullUrl, {
                headers: {
                  ...signature,
                  "paxeer-network-account-id": id,
                },
              });
            }),
          );
        };
        // @ts-ignore
        return useSWRNext(key, extendedFetcher, config);
      } catch (e) {
        throw e;
      }
    };
  };
};

export const useSubAccountQuery = <T>(
  query: Parameters<SWRHook>[0],
  options?: QueryOptions<T>,
): SWRResponse<T> => {
  const { formatter, accountId, ...swrOptions } = options || {};
  const { state, account } = useAccount();
  const middleware = Array.isArray(options?.use) ? (options?.use ?? []) : [];

  const ids = Array.isArray(accountId) ? accountId : [accountId];

  const shouldFetch =
    ids.filter(Boolean).length &&
    (state.status >= AccountStatusEnum.EnableTrading ||
      state.status === AccountStatusEnum.EnableTradingWithoutConnected);

  return useSWR<T>(
    () => (shouldFetch ? [query, ids] : null),
    (url: string, init: RequestInit) => {
      return fetcher(url, init, { formatter });
    },
    {
      ...swrOptions,
      use: [...middleware, signatureMiddleware(account, ids as string[])],
      onError: () => {},
    },
  );
};

export type MessageFactor = {
  url: string;
  method: "GET" | "POST" | "PUT" | "DELETE";
  data?: any;
};
