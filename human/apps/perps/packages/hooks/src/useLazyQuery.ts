/* eslint-disable @typescript-eslint/no-explicit-any */
import type { SWRHook } from "swr";
import type {
  SWRMutationResponse,
  SWRMutationConfiguration,
} from "swr/mutation";
import useSWRMutation from "swr/mutation";
import { SDKError } from "@paxeer-network/types";
import { useConfig } from "./useConfig";
import { fetcher } from "./utils/fetcher";

/**
 * useQuery
 * @description for public api
 * @param query
 * @param options
 */
export const useLazyQuery = <T, R = any>(
  query: Parameters<SWRHook>[0],
  options?: SWRMutationConfiguration<any, any> & {
    formatter?: (data: any) => R;
    init?: RequestInit;
  },
): SWRMutationResponse => {
  const apiBaseUrl = useConfig("apiBaseUrl");
  const { formatter, init, ...swrOptions } = options || {};
  // check the query is public api
  // if (typeof query === "string" && !query.startsWith("/v1/public")) {
  //   throw new SDKError("useQuery is only for public api");
  // }

  if (typeof apiBaseUrl === "undefined") {
    throw new SDKError("please add PaxeerNetworkConfigProvider to your app");
  }

  // @ts-ignore
  return useSWRMutation(
    query,
    (url: string, options: any) => {
      url = url.startsWith("http") ? url : `${apiBaseUrl}${url}`;
      if (options?.arg) {
        // const searchParams = new URLSearchParams(init.arg);
        // url = `${url}?${encodeURIComponent(searchParams.toString())}`;
        const queryString = Object.entries<string>(options.arg)
          .map(([key, value]) => `${key}=${encodeURIComponent(value)}`)
          .join("&");
        url = `${url}?${queryString}`;
      }
      return fetcher(url, init, { formatter });
    },
    swrOptions,
  );
};
