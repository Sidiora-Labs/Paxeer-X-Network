import { useCallback } from "react";
import { useAccount } from "../useAccount";
import { useMutation } from "../useMutation";
import { usePrivateQuery } from "../usePrivateQuery";

export type APIKeyItem = {
  paxeerNetwork_key: string;
  key_status: string;
  ip_restriction_list: string[];
  ip_restricion_status: string;
  expiration: number;
  tag?: any;
  scope?: string;
};

export enum ScopeType {
  trade = "trade",
  trading = "trading",
  tradeAndTrading = "trade,trading",
}

export const useApiKeyManager = (queryParams?: {
  keyInfo?: {
    page?: number;
    size?: number;
    key_status?: string;
  };
}) => {
  const { account, state, isSubAccount } = useAccount();
  const { keyInfo } = queryParams || {};

  const keyInfoPrams = getQueryParamsFromObject(keyInfo);

  const { data, mutate, error, isLoading } = usePrivateQuery<APIKeyItem[]>(
    `/v1/client/key_info${keyInfoPrams.length > 0 ? `?${keyInfoPrams}` : ""}`,
    {
      formatter: (data) => data?.rows,
    },
  );

  const [doIPRestriction] = useMutation(
    "/v1/client/set_paxeerNetwork_key_ip_restriction",
    "POST",
  );

  const [doRemoveOrderKey] = useMutation(
    "/v1/client/remove_paxeerNetwork_key",
    "POST",
  );

  const [doResetIPRestriction] = useMutation(
    "/v1/client/reset_paxeerNetwork_key_ip_restriction",
    "POST",
  );

  /**  ip_restriction_list is ["192.168.1.1", "192.168.1.2"].join(",") */
  const setIPRestriction = useCallback(
    (paxeerNetwork_key: string, ip_restriction_list: string): Promise<any> => {
      return doIPRestriction({
        paxeerNetwork_key,
        ip_restriction_list,
      });
    },
    [],
  );

  const removePaxeerNetworkKey = useCallback((paxeerNetwork_key: string): Promise<any> => {
    return doRemoveOrderKey({
      paxeerNetwork_key,
    });
  }, []);

  const generatePaxeerNetworkKey = (
    scope?: ScopeType,
  ): Promise<{
    key: string;
    secretKey: string;
  }> => {
    if (isSubAccount) {
      return account?.createSubAccountApiKey(365, {
        tag: "manualCreated",
        scope,
        subAccountId: state.accountId!,
      });
    }

    return account?.createApiKey(365, {
      tag: "manualCreated",
      scope,
    });
  };

  const resetPaxeerNetworkKeyIPRestriction = (
    paxeerNetworkKey: string,
    mode: "ALLOW_ALL_IPS" | "DISALLOW_ALL_IPS",
  ) => {
    return doResetIPRestriction({
      paxeerNetwork_key: paxeerNetworkKey,
      reset_mode: mode,
    });
  };
  return [
    data,
    {
      refresh: mutate,
      error,
      isLoading,
      generatePaxeerNetworkKey,
      setIPRestriction,
      removePaxeerNetworkKey,
      resetPaxeerNetworkKeyIPRestriction,
    },
  ] as const;
};

function getQueryParamsFromObject(obj?: Record<string, any>): string {
  if (typeof obj === "undefined") return "";
  const queryParams = new URLSearchParams();

  for (const [key, value] of Object.entries(obj)) {
    if (value !== undefined) {
      if (Array.isArray(value)) {
        value.forEach((item) => {
          queryParams.append(key, item.toString());
        });
      } else {
        queryParams.set(key, value.toString());
      }
    }
  }

  return queryParams.toString();
}
