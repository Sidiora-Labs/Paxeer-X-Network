/* eslint-disable @typescript-eslint/no-explicit-any */
import { useEffect, useMemo } from "react";
import { useDebouncedCallback } from "use-debounce";
import {
  API,
  AssetHistoryStatusEnum,
  EMPTY_LIST,
} from "@paxeer-network/types";
import { useEventEmitter } from "../../useEventEmitter";
import { usePrivateQuery } from "../../usePrivateQuery";

type AssetHistoryOptions = {
  /** token name you want to search */
  token?: string;
  /** DEPOSIT、WITHDRAW, all */
  side?: string;
  status?: AssetHistoryStatusEnum;
  /** start time in milliseconds */
  startTime?: number;
  /** end time in milliseconds */
  endTime?: number;
  page?: number;
  pageSize?: number;
};

/**
 * Get asset history, including token deposits/withdrawals.
 * https://paxeer-network.network/docs/build-on-omnichain/evm-api/restful-api/private/get-asset-history#get-asset-history
 */
export const useAssetsHistory = (
  options: AssetHistoryOptions,
  config?: {
    /**
     * should update when wallet changed, default is update
     */
    shouldUpdateOnWalletChanged?: (data: any) => boolean;
  },
) => {
  const ee = useEventEmitter();

  const getKey = () => {
    const {
      page = 1,
      pageSize = 10,
      token,
      side,
      status,
      startTime,
      endTime,
    } = options;
    const searchParams = new URLSearchParams();

    searchParams.set("page", page.toString());
    searchParams.set("size", pageSize.toString());

    if (token) {
      searchParams.set("token", token);
    }

    if (side && side !== "All") {
      searchParams.set("side", side);
    }

    if (status) {
      searchParams.set("status", status);
    }

    if (startTime) {
      searchParams.set("start_t", startTime.toString());
    }

    if (endTime) {
      searchParams.set("end_t", endTime.toString());
    }

    return `/v1/asset/history?${searchParams.toString()}`;
  };

  const { data, isLoading, mutate } = usePrivateQuery<API.AssetHistory>(
    getKey(),
    {
      formatter: (data) => data,
      revalidateOnFocus: false,
      errorRetryInterval: 60 * 1000,
    },
  );

  const updateList = useDebouncedCallback(
    (data: any) => {
      const isUpdate =
        typeof config?.shouldUpdateOnWalletChanged === "function"
          ? config.shouldUpdateOnWalletChanged(data)
          : true;

      if (isUpdate) {
        mutate();
      }
    },
    // delay in ms
    300,
  );

  useEffect(() => {
    ee.on("wallet:changed", updateList);

    return () => {
      ee.off("wallet:changed", updateList);
    };
  }, []);

  return useMemo(
    () => [data?.rows ?? EMPTY_LIST, { meta: data?.meta, isLoading }] as const,
    [data?.rows, data?.meta, isLoading],
  );
};
