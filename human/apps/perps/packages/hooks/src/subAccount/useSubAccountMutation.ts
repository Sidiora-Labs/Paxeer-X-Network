import useSWRMutation, { type SWRMutationConfiguration } from "swr/mutation";
import {
  type MessageFactor,
  type SignedMessagePayload,
} from "@paxeer-network/core";
import { mutate } from "@paxeer-network/net";
import { getTimestamp } from "@paxeer-network/utils";
import { useMemoizedFn } from "..";
import { useAccountInstance } from "../useAccountInstance";
import { useConfig } from "../useConfig";

type HTTP_METHOD = "POST" | "PUT" | "DELETE" | "GET";

const fetcher = (
  url: string,
  options: {
    arg: {
      data?: any;
      params?: any;
      method: HTTP_METHOD;
      signature: SignedMessagePayload;
    };
  },
) => {
  const init: RequestInit = {
    method: options.arg.method,
    headers: {
      ...options.arg.signature,
    },
  };

  if (options.arg.data) {
    init.body = JSON.stringify(options.arg.data);
  }

  if (
    typeof options.arg.params === "object" &&
    Object.keys(options.arg.params).length
  ) {
    const search = new URLSearchParams(options.arg.params);
    url = `${url}?${search.toString()}`;
  }

  return mutate(url, init);
};

type SubAccountMutationOptions<T, E> = SWRMutationConfiguration<T, E> & {
  /** sub account id */
  accountId?: string;
};

/**
 * This hook is used to execute API requests for data mutation, such as POST, DELETE, PUT, etc.
 */
export const useSubAccountMutation = <T, E>(
  /**
   * The URL to send the request to. If the URL does not start with "http",
   * it will be prefixed with the API base URL.
   */
  url: string,
  /**
   * The HTTP method to use for the request. Defaults to "POST".
   */
  method: HTTP_METHOD = "POST",
  /**
   * The configuration object for the mutation.
   * @see [useSWRMutation](https://swr.vercel.app/docs/mutation#api)
   *
   * @link https://swr.vercel.app/docs/mutation#api
   */
  options?: SubAccountMutationOptions<T, E>,
) => {
  const { accountId, ...restOptions } = options || ({} as any);
  const apiBaseUrl = useConfig("apiBaseUrl");

  let fullUrl = url;
  if (!url.startsWith("http")) {
    fullUrl = `${apiBaseUrl}${url}`;
  }

  const account = useAccountInstance();

  const { trigger, reset, data, error, isMutating } = useSWRMutation(
    fullUrl,
    // method === "POST" ? fetcher : deleteFetcher,
    fetcher,
    restOptions,
  );

  const mutation = async (
    /**
     * The data to send with the request.
     */
    data: Record<string, any> | null,
    /**
     * The query parameters to send with the request.
     */
    params?: Record<string, any>,
    options?: SubAccountMutationOptions<T, E>,
  ): Promise<any> => {
    const { accountId: _accountId, ...restOptions } = options || ({} as any);
    let newUrl = url;
    if (typeof params === "object" && Object.keys(params).length) {
      const search = new URLSearchParams(params);
      newUrl = `${url}?${search.toString()}`;
    }

    const payload: MessageFactor = {
      method,
      url: newUrl,
      data,
    };

    const signer = account.signer;
    const signature = await signer.sign(payload, getTimestamp());

    return trigger(
      {
        data,
        params,
        method,
        signature: {
          ...signature,
          "paxeer-network-account-id": accountId || _accountId || account.accountId,
        },
      },
      restOptions,
    );
  };

  return [useMemoizedFn(mutation), { data, error, reset, isMutating }] as const;
};
