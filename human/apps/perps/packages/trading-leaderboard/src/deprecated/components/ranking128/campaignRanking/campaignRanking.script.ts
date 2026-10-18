import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
  useAccount,
  useConfig,
  useInfiniteQuery,
  useQuery,
} from "@paxeer-network/hooks";
import { API } from "@paxeer-network/types";
import { TableSort, usePagination, useScreen } from "@paxeer-network/ui";
import { useEndReached } from "../../../../hooks/useEndReached";
import { CampaignConfig, UserData } from "../../campaigns/type";
import { useTradingLeaderboardContext } from "../../provider";
import { calculateUserPoolReward } from "../../rewards/utils";
import { getCurrentAddressRowKey, isSameAddress } from "../shared/util";

export type CampaignRankingData = {
  broker_id: string;
  account_id: string;
  address: string;
  volume: number;
  pnl: number;
  total_deposit_amount: number;
  total_withdrawal_amount: number;
  start_account_value: number;
  end_account_value: number;

  // custom field
  key?: string;
  rank?: number | string;
  rewards?: {
    amount: number;
    currency: string;
  };
};

type CampaignRankingResponse = {
  meta: API.RecordsMeta;
  rows: CampaignRankingData[];
  updated_time: number;
};
export type CampaignRankingScriptReturn = ReturnType<
  typeof useCampaignRankingScript
>;

export type CampaignRankingScriptOptions = {
  campaignId: number | string;
  sortKey?: "volume" | "pnl";
};

export function useCampaignRankingScript(
  options: CampaignRankingScriptOptions,
) {
  const { campaignId, sortKey = "volume" } = options;
  const [initialSort] = useState<TableSort>({
    sortKey,
    sort: "desc",
  });
  const [sort, setSort] = useState<TableSort | undefined>(initialSort);

  const { currentCampaign, setUserData, setUpdatedTime, dataAdapter } =
    useTradingLeaderboardContext();

  const { state } = useAccount();
  const brokerId = useConfig("brokerId");

  const { isMobile } = useScreen();

  const { page, pageSize, setPage, parsePagination } = usePagination({
    pageSize: isMobile ? 100 : 20,
  });

  const isCustomData = typeof dataAdapter === "function";

  const customData = useMemo(() => {
    if (typeof dataAdapter === "function") {
      return dataAdapter({ page, pageSize });
    }
  }, [dataAdapter, page, pageSize]);

  const getUrl = (args: {
    page: number;
    pageSize: number;
    sort?: string | null;
  }) => {
    if (isCustomData) {
      return null;
    }
    const searchParams = new URLSearchParams();

    searchParams.set("page", args.page.toString());
    searchParams.set(
      "size",
      // if page is 1, we need to set page size to 100 to get the top 100 data to judge user rank
      args.page === 1 ? "100" : args.pageSize.toString(),
    );

    if (campaignId) {
      searchParams.set("campaign_id", campaignId.toString());
    }

    if (brokerId) {
      searchParams.set("broker_id", brokerId);
    }

    if (args.sort) {
      searchParams.set("sort_by", args.sort);
    } else if (args.sort !== null && sort) {
      searchParams.set("sort_by", sort.sortKey);
    }

    // https://paxeer-network.network/docs/build-on-omnichain/evm-api/restful-api/public/get-campaign-ranking
    // only get prod ranking data
    return `https://api.paxeer-network.org/v1/public/campaign/ranking?${searchParams.toString()}`;
  };

  const { data, isLoading } = useQuery<CampaignRankingResponse>(
    getUrl({ page, pageSize }),
    {
      formatter: (res) => res,
      revalidateOnFocus: false,
    },
  );

  const {
    data: infiniteData,
    size,
    setSize,
    isValidating,
  } = useInfiniteQuery<CampaignRankingResponse>(
    (pageIndex: number, previousPageData: any): string | null => {
      // reached the end
      if (previousPageData && !previousPageData.rows?.length) return null;

      if (!isMobile) {
        return null;
      }

      return getUrl({
        page: pageIndex + 1,
        pageSize,
      });
    },
    {
      initialSize: 1,
      formatter: (res) => res,
      revalidateOnFocus: false,
    },
  );

  // it will use first page data cache
  const { data: top100Data } = useQuery<CampaignRankingResponse>(
    state.address
      ? getUrl({
          page: 1,
          pageSize: 100,
          // sort: "desc",
        })
      : null,
    {
      formatter: (res) => res,
      revalidateOnFocus: false,
    },
  );

  const getUserUrl = () => {
    if (!state.address || !campaignId || isCustomData) {
      return null;
    }

    const searchParams = new URLSearchParams();

    if (brokerId) {
      searchParams.set("broker_id", brokerId);
    }

    searchParams.set("campaign_id", campaignId.toString());
    searchParams.set("address", state.address!);

    // https://paxeer-network.network/docs/build-on-omnichain/evm-api/restful-api/public/get-campaign-user-info
    // only get prod user data
    return `https://api.paxeer-network.org/v1/public/campaign/user?${searchParams.toString()}`;
  };

  const { data: userDataRes } = useQuery<CampaignRankingData[]>(getUserUrl(), {
    revalidateOnFocus: false,
  });

  const getAddressRank = useCallback(
    (address: string) => {
      if (top100Data?.rows?.length === 0) {
        return "-";
      }

      const index = top100Data?.rows.findIndex((item) =>
        isSameAddress(item.address, address!),
      );
      return index !== -1 ? index! + 1 : "100+";
    },
    [top100Data],
  );

  const userData = useMemo(() => {
    if (customData) {
      return customData?.userData;
    }

    if (!state.address || isLoading) {
      return undefined;
    }

    if (!userDataRes) {
      return {
        key: getCurrentAddressRowKey(state.address!),
        address: state.address,
        rank: "-",
      } as unknown as CampaignRankingData;
    }

    return {
      ...userDataRes,
      address: state.address,
      rank: getAddressRank(state.address!),
      key: getCurrentAddressRowKey(state.address!),
    };
  }, [state.address, userDataRes, isLoading, getAddressRank, customData]);

  const addRankForList = useCallback(
    (list: CampaignRankingData[], total: number) => {
      return list?.map((item, index) => {
        let rank: string | number = index + 1;

        if (sort?.sort === "asc") {
          rank = total - (page - 1) * pageSize - index;
        } else if (sort?.sort === "desc") {
          rank = (page - 1) * pageSize + index + 1;
        }

        return {
          ...item,
          rank,
        };
      });
    },
    [page, pageSize, sort],
  );

  const dataSource = useMemo(() => {
    if (customData) {
      return formatData(customData?.dataSource, currentCampaign, sortKey);
    }

    let list = data?.rows || [];
    if (page === 1) {
      list = list.slice(0, pageSize);
    }
    const total = data?.meta.total || 0;
    const rankList = addRankForList(list, total);

    const _data =
      page === 1 ? (userData ? [userData, ...rankList] : rankList) : rankList;

    return formatData(_data, currentCampaign, sortKey);
  }, [
    data,
    page,
    pageSize,
    userData,
    addRankForList,
    currentCampaign,
    sortKey,
    customData,
  ]);

  const dataList = useMemo(() => {
    if (customData) {
      return formatData(customData?.dataList, currentCampaign, sortKey);
    }

    if (!infiniteData?.length) {
      return [];
    }

    const total = infiniteData[0]?.meta.total || 0;
    const flatList = infiniteData?.map((item) => item.rows)?.flat();
    const rankList = addRankForList(flatList, total);

    const _data = userData ? [userData, ...rankList] : rankList;

    return formatData(_data, currentCampaign, sortKey);
  }, [
    infiniteData,
    userData,
    addRankForList,
    currentCampaign,
    sortKey,
    customData,
  ]);

  const sentinelRef = useRef<HTMLDivElement | null>(null);

  const pagination = useMemo(
    () =>
      parsePagination({
        total: customData?.meta?.total || data?.meta?.total || 0,
        current_page:
          customData?.meta?.current_page || data?.meta?.current_page || 1,
        records_per_page: pageSize,
      }),
    [
      data?.meta?.total,
      data?.meta?.current_page,
      pageSize,
      parsePagination,
      customData,
    ],
  );

  useEndReached(sentinelRef, () => {
    if (!isValidating && isMobile) {
      setSize(size + 1);
    }
  });

  useEffect(() => {
    setPage(1);
  }, [state.address]);

  useEffect(() => {
    if (userData) {
      setUserData?.(userData as any);
    }
  }, [userData]);

  useEffect(() => {
    const time = customData?.updatedTime || data?.updated_time || 0;
    setUpdatedTime?.(time);
    // when currentCampaign changed, we need to reset the campaign ranking list updated time
  }, [data, currentCampaign, customData]);

  useEffect(() => {
    setSort({ sortKey, sort: "desc" });
  }, [sortKey]);

  return {
    pagination,
    initialSort,
    dataSource,
    isLoading: isLoading || isValidating || customData?.loading,
    isMobile,
    sentinelRef,
    dataList,
    address: state.address,
  };
}

function formatData(
  data?: any[],
  currentCampaign?: CampaignConfig,
  metric?: "volume" | "pnl",
) {
  const pool = currentCampaign?.prize_pools?.find(
    (item) => item.metric === metric,
  );

  return data?.map((item) => {
    const rewards = pool ? calculateUserPoolReward(item as UserData, pool!) : 0;

    return {
      ...item,
      rewards: {
        amount: rewards,
        currency: pool?.currency,
      },
    };
  });
}
