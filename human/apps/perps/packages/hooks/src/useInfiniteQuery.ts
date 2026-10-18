import useSWRInfinite, {
  type SWRInfiniteKeyLoader,
  type SWRInfiniteConfiguration,
} from "swr/infinite";
import { useConfig } from "./useConfig";
import { fetcher } from "./utils/fetcher";

export const useInfiniteQuery = <T>(
  getKey: SWRInfiniteKeyLoader,
  options?: SWRInfiniteConfiguration & {
    formatter?: (data: any) => any;
  },
) => {
  const { formatter, ...swrOptions } = options || {};
  const apiBaseUrl = useConfig("apiBaseUrl");

  const result = useSWRInfinite<T>(
    (pageIndex: number, previousPageData) => {
      return getKey(pageIndex, previousPageData);
    },
    (url: string, init: RequestInit) => {
      const _url = url.startsWith("http") ? url : `${apiBaseUrl}${url}`;
      return fetcher(_url, init, { formatter });
    },
    swrOptions,
  );

  return result;
};
