import type { Middleware } from "swr";
import { nativeRequest } from "@paxeer-network/paxeer-backend/nativeClient";
/** Private reads are Paxeer RPC/indexer reads gated by the connected-wallet state. */
export const signatureMiddleware: Middleware = useSWRNext => (key,fetcher,config) => {
  const nextFetcher = async (value:any) => {
    const url = Array.isArray(value) ? value[0] : value;
    return fetcher ? fetcher(url,{}) : (await nativeRequest(String(url))).data;
  };
  return useSWRNext(key,nextFetcher,config);
};
