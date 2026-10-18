import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useAccount, useQuery } from "@paxeer-network/hooks";
import { API } from "@paxeer-network/types";
import { TableSort, usePagination, useScreen } from "@paxeer-network/ui";
import { useEndReached } from "../../../../hooks/useEndReached";
import { DateRange } from "../../../type";
import { getDateRange } from "../../../utils";
import { CampaignStatistics } from "../../campaigns/type";
import { useTradingLeaderboardContext } from "../../provider";
import { getCurrentAddressRowKey, isSameAddress } from "../shared/util";

const CAMPAIGN_IDS = ["129", "136", "131", "132", "133", "134"];

export type GeneralRankingData = {
  account_id: string;
  address: string;
  broker_fee: number;
  date: string;
  perp_maker_volume: number;
  perp_taker_volume: number;
  perp_volume: number;
  realized_pnl: number;
  total_fee: number;

  // custom field
  key?: string;
  rank?: number | string;
  volume?: number;
  pnl?: number;
};

export type GeneralRankingResponse = {
  meta: API.RecordsMeta;
  rows: GeneralRankingData[];
};
export type GeneralRankingScriptReturn = ReturnType<
  typeof useGeneralRankingScript
>;

export type GeneralRankingScriptOptions = {
  dateRange?: DateRange | null;
  address?: string;
  sortKey?: "perp_volume" | "realized_pnl";
  weekOneAddresses?: string[];
};

export function useGeneralRankingScript(options?: GeneralRankingScriptOptions) {
  const {
    dateRange = getDateRange(90),
    address: searchValue,
    sortKey = "perp_volume",
    weekOneAddresses,
  } = options || {};

  const [initialSort] = useState<TableSort>({
    sortKey,
    sort: "desc",
  });

  const [sort, setSort] = useState<TableSort | undefined>(initialSort);

  const { state } = useAccount();

  const { setStatistics } = useTradingLeaderboardContext();

  const { isMobile } = useScreen();

  const { page, pageSize, setPage, parsePagination } = usePagination({
    pageSize: 500,
  });

  const { campaignRankingList, filteredCampaignData, isLoading } =
    useCampaignRankingList({
      dateRange,
      sort,
      searchValue,
      weekOneAddresses,
      setStatistics,
    });

  const getAddressRank = useCallback(
    (address: string) => {
      const index = filteredCampaignData?.findIndex((item) =>
        isSameAddress(item.address, address!),
      );
      return index !== -1 ? index! + 1 : "-";
    },
    [filteredCampaignData],
  );

  const userDataList = useMemo(() => {
    if (!state.address) {
      return [];
    }

    const targetAddress = filteredCampaignData?.find((item) =>
      isSameAddress(item.address, state.address!),
    );

    if (!targetAddress) {
      return [];
    }

    return [
      {
        ...targetAddress,
        rank: getAddressRank(targetAddress!.address!),
        key: getCurrentAddressRowKey(targetAddress!.address!),
      },
    ];
  }, [state.address, getAddressRank, filteredCampaignData]);

  const addRankForList = useCallback(
    (list: GeneralRankingData[], total: number) => {
      return list?.map((item, index) => {
        let rank: string | number = index + 1;

        if (searchValue) {
          rank = getAddressRank(item.address);
        } else {
          if (sort?.sort === "asc") {
            rank = total - index;
          } else if (sort?.sort === "desc") {
            rank = index + 1;
          }
        }

        return {
          ...item,
          rank,
        };
      });
    },
    [page, pageSize, sort, searchValue, getAddressRank],
  );

  const dataSource = useMemo(() => {
    let list: GeneralRankingData[] =
      filteredCampaignData as GeneralRankingData[];
    // hardcode for 128 campaign

    if (page === 1) {
      list = list.slice(0, pageSize);
    }
    const total = list.length;

    const rankList = addRankForList(list, total);

    if (page === 1 && !searchValue) {
      return formatData([...userDataList, ...rankList]);
    }
    return formatData(rankList);
  }, [
    page,
    pageSize,
    userDataList,
    searchValue,
    addRankForList,
    campaignRankingList,
    filteredCampaignData,
  ]);

  const dataList = useMemo(() => {
    let total = filteredCampaignData?.length || 0;
    let flatList = filteredCampaignData as GeneralRankingData[];
    if (campaignRankingList && campaignRankingList.length > 0) {
      if (filteredCampaignData) {
        flatList = filteredCampaignData as GeneralRankingData[];
        total = filteredCampaignData.length;
      } else {
        flatList = flatList.filter((item) =>
          campaignRankingList.includes(item.address),
        );
        total = flatList.length;
      }
    }
    const rankList = addRankForList(flatList, total);

    if (!searchValue) {
      return formatData([...userDataList, ...rankList]);
    }
    return formatData(rankList);
  }, [
    userDataList,
    searchValue,
    addRankForList,
    campaignRankingList,
    filteredCampaignData,
  ]);

  const sentinelRef = useRef<HTMLDivElement | null>(null);

  const pagination = useMemo(
    () =>
      parsePagination({
        total: dataSource?.length,
        current_page: 1,
        records_per_page: pageSize,
      }),
    [pageSize, dataSource],
  );

  useEndReached(sentinelRef, () => {
    if (isMobile) {
      setPage(page + 1);
    }
  });

  const onSort = useCallback(
    (sort?: TableSort) => {
      // befause table column dataIndex is not the same as the api sort, so we need to map the sortKey
      if (sort?.sortKey === "volume") {
        sort.sortKey = "perp_volume";
      } else if (sort?.sortKey === "pnl") {
        sort.sortKey = "realized_pnl";
      }
      // fix for mobile sort
      setSort((_sort) => {
        if (sort) return sort;
        return isMobile
          ? { sortKey: _sort?.sortKey || "", sort: "desc" }
          : initialSort;
      });
    },
    [initialSort, isMobile],
  );

  useEffect(() => {
    if (searchValue) {
      setPage(1);
    }
  }, [searchValue]);

  useEffect(() => {
    setPage(1);
  }, [state.address]);

  useEffect(() => {
    if (dateRange?.to && dateRange?.from) {
      setPage(1);
    }
  }, [dateRange]);

  useEffect(() => {
    setSort({ sortKey, sort: "desc" });
  }, [sortKey]);

  return {
    pagination,
    initialSort,
    onSort,
    dataSource,
    isLoading,
    isMobile,
    sentinelRef,
    dataList,
    address: state.address,
  };
}

function formatData(data: any[]) {
  return data.map((item) => ({
    ...item,
    volume: item.perp_volume,
    pnl: item.realized_pnl,
  }));
}

// for 128 campaign hardcode
const useCampaignRankingList = ({
  dateRange,
  sort,
  searchValue,
  weekOneAddresses,
  setStatistics,
}: {
  dateRange: DateRange | null;
  sort?: TableSort;
  searchValue?: string;
  weekOneAddresses?: string[];
  setStatistics: any;
}) => {
  const [allCampaignData, setAllCampaignData] = useState<any[]>([]);
  const getUrl = (campaignId: string) => {
    const searchParams = new URLSearchParams();
    searchParams.set("campaign_id", campaignId);
    searchParams.set("page", "1");
    searchParams.set("size", "2000");
    searchParams.set("aggregate_by", "address");

    return `https://api.paxeer-network.org/v1/public/campaign/ranking?${searchParams.toString()}`;
  };

  useEffect(() => {
    const urls = CAMPAIGN_IDS.map((campaignId) => getUrl(campaignId));
    Promise.all(urls.map((url) => fetch(url).then((res) => res.json()))).then(
      (res) => {
        setAllCampaignData(res.map(formatCampaignData));
      },
    );
  }, []);

  const { data: campaignData, isLoading: isCampaignDataLoading } = useQuery<
    GeneralRankingResponse["rows"]
  >(getUrl("128"), {
    revalidateOnFocus: false,
  });

  const { data, isLoading: isDataLoading } = useQuery<
    GeneralRankingResponse["rows"]
  >(
    dateRange?.label
      ? getUrl(CAMPAIGN_IDS[Number(dateRange.label?.split(" ")[1]) - 1])
      : null,
    {
      revalidateOnFocus: false,
    },
  );

  const campaignRankingList = useMemo(() => {
    if (!dateRange?.label) {
      return undefined;
    }
    return campaignData?.map((item) => item.address) || [];
  }, [campaignData, dateRange]);

  useEffect(() => {
    if (allCampaignData.length > 0 && campaignRankingList) {
      const list = weekOneAddresses ? weekOneAddresses : campaignRankingList;
      let totalVolume = 0;

      allCampaignData.forEach((item, index) => {
        if (index === 0) {
          const filteredList = item.filter((item: any) =>
            list.includes(item.address),
          );
          totalVolume += filteredList.reduce(
            (acc: number, curr: any) => acc + curr.perp_volume,
            0,
          );
        } else {
          totalVolume += item.reduce(
            (acc: number, curr: any) => acc + curr.perp_volume,
            0,
          );
        }
      });

      setStatistics((stas: CampaignStatistics) => ({
        ...stas,
        total_volume: totalVolume,
      }));
    }
  }, [allCampaignData, weekOneAddresses, campaignRankingList]);

  const isLoading = isCampaignDataLoading || isDataLoading;

  const filteredCampaignData = useMemo(() => {
    const list =
      data?.filter((item) => {
        if (dateRange?.label === "Week 1") {
          return (
            (weekOneAddresses
              ? weekOneAddresses?.includes(item.address)
              : campaignRankingList?.includes(item.address)) &&
            (searchValue ? isSameAddress(item.address, searchValue) : true)
          );
        }
        return searchValue ? isSameAddress(item.address, searchValue) : true;
      }) || [];
    const newList = list.map((item) => ({
      address: item.address,
      perp_volume: item.volume,
      realized_pnl: item.pnl,
    }));

    if (sort) {
      newList.sort((a: any, b: any) => {
        if (sort?.sort === "asc") {
          return a?.[sort?.sortKey] - b?.[sort?.sortKey];
        } else if (sort?.sort === "desc") {
          return b?.[sort?.sortKey] - a?.[sort?.sortKey];
        }
        return 0;
      });
    }
    return newList;
  }, [
    data,
    campaignRankingList,
    sort,
    dateRange,
    searchValue,
    weekOneAddresses,
  ]);

  return { campaignRankingList, filteredCampaignData, isLoading };
};

const formatCampaignData = (result: any) => {
  const data = result?.data?.rows || [];
  return data.map((item: any) => ({
    address: item.address,
    perp_volume: item.volume,
    realized_pnl: item.pnl,
  }));
};
