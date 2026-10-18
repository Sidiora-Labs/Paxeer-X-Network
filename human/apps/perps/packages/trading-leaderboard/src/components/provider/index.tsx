import React, {
  createContext,
  useContext,
  useMemo,
  useState,
  useEffect,
} from "react";
import { parseISO } from "date-fns";
import { sortWith, descend } from "ramda";
import {
  usePrivateQuery,
  RefferalAPI,
  useMemoizedFn,
  noCacheConfig,
  useConfig,
  useQuery,
} from "@paxeer-network/hooks";
import { getCurrentTierIndex } from "../campaigns/pricePool/utils";
import {
  CampaignConfig,
  UserData,
  CampaignStatsResponse,
  CampaignStatistics,
} from "../campaigns/type";

/**
 * Trading leaderboard provider state
 */
export type TradingLeaderboardState = {
  /** campaigns config, if not provided, will not show campaigns section */
  campaigns?: CampaignConfig[];
  /** background src, it can be a image resource or video resource */
  backgroundSrc?: string;
  href?: {
    /** default trading now button url */
    trading: string;
  };
  currentCampaignId: string | number;
  currentCampaign?: CampaignConfig;
  onCampaignChange: (campaignId: string | number) => void;
  /** leaderboard user data, it will be used to calculate the rewards */
  userData?: UserData;
  /** set leaderboard user data */
  setUserData?: (userdata: UserData) => void;
  /** campaign ranking list updated time */
  updatedTime?: number;
  /** set snapshot time */
  setUpdatedTime?: (updatedTime?: number) => void;
  /** custom data, if use this, you can full control the data */
  dataAdapter?: (info: { page: number; pageSize: number }) => {
    loading: boolean;
    dataSource?: any[];
    dataList?: any[];
    userData?: any;
    updatedTime?: number;
    meta?: {
      total: number;
      current_page: number;
      records_per_page: number;
    };
  };
  campaignDateRange?: {
    start_time: Date | string;
    end_time: Date | string;
  };
  statistics?: CampaignStatistics;
  setStatistics?: (statistics: CampaignStatistics) => void;
};

/**
 * Trading leaderboard context
 */
export const TradingLeaderboardContext = createContext<TradingLeaderboardState>(
  {} as TradingLeaderboardState,
);

export type TradingLeaderboardProviderProps = Pick<
  TradingLeaderboardState,
  "campaigns" | "href" | "backgroundSrc" | "dataAdapter"
> & {
  campaignId?: string | number;
  onCampaignChange?: (campaignId: string | number) => void;
};

export const TradingLeaderboardProvider: React.FC<
  React.PropsWithChildren<TradingLeaderboardProviderProps>
> = (props) => {
  const {
    campaignId,
    campaigns,
    backgroundSrc,
    href,
    children,
    dataAdapter,
    onCampaignChange,
  } = props;

  const [userData, setUserData] = useState<UserData>();
  const [updatedTime, setUpdatedTime] = useState<number>();
  const [statistics, setStatistics] = useState<CampaignStatistics>({
    total_participants: 0,
    total_volume: 0,
  });

  const { data: generateCode, mutate: generateCodeMutate } =
    usePrivateQuery<RefferalAPI.ReferralInfo>("/v1/referral/info", {
      revalidateOnFocus: true,
      errorRetryCount: 3,
      ...noCacheConfig,
    });

  const refererCode = generateCode?.referee_info?.referer_code ?? "";

  useEffect(() => {
    if (refererCode && userData?.referral_code != refererCode) {
      setUserData({ ...userData!, referral_code: refererCode });
      generateCodeMutate();
    }
  }, [userData, refererCode, generateCodeMutate]);

  const currentCampaign = useMemo(() => {
    return campaigns?.find((campaign) => campaign.campaign_id == campaignId);
  }, [campaigns, campaignId]);

  const filteredCampaigns = useMemo(() => {
    // Using date-fns to parse date strings and sort by end_time in descending order
    return campaigns
      ? sortWith(
          [descend((campaign: CampaignConfig) => parseISO(campaign.end_time))],
          campaigns,
        )
      : campaigns;
  }, [campaigns]);

  const memoCampaignChange = useMemoizedFn((id: string | number) => {
    onCampaignChange?.(id);
  });

  const symbols = Array.isArray(currentCampaign?.volume_scope)
    ? currentCampaign?.volume_scope.join(",")
    : currentCampaign?.volume_scope;

  const brokerId = useConfig("brokerId");

  const searchParams = useMemo(() => {
    return {
      campaign_id: campaignId?.toString() || "",
      symbols: symbols || "",
      broker_id: brokerId,
      group_by: "BROKER",
    };
  }, [campaignId, symbols, brokerId]);

  const { data: stats } = useQuery<CampaignStatsResponse>(
    campaignId !== "general"
      ? `https://api.paxeer-network.org/v1/public/campaign/stats?${new URLSearchParams(searchParams).toString()}`
      : null,
    { revalidateOnFocus: false },
  );

  useEffect(() => {
    setStatistics({
      total_participants: stats?.user_count,
      total_volume: stats?.volume,
    });
  }, [stats?.user_count, stats?.volume]);

  const tieredIndex = useMemo(() => {
    if (!currentCampaign?.tiered_prize_pools) return 0;
    return getCurrentTierIndex(
      statistics?.total_volume || 0,
      currentCampaign?.tiered_prize_pools,
    );
  }, [statistics?.total_volume, currentCampaign?.tiered_prize_pools]);

  const _currentCampaign = useMemo(() => {
    if (currentCampaign?.tiered_prize_pools) {
      return {
        ...currentCampaign,
        prize_pools: currentCampaign?.tiered_prize_pools[tieredIndex],
      };
    }
    return currentCampaign;
  }, [currentCampaign, tieredIndex]);

  const campaignDateRange = useMemo(() => {
    return currentCampaign?.start_time && currentCampaign?.end_time
      ? {
          start_time: currentCampaign.start_time,
          end_time: currentCampaign.end_time,
        }
      : undefined;
  }, [currentCampaign]);

  const memoizedValue = useMemo<TradingLeaderboardState>(() => {
    return {
      campaigns: filteredCampaigns,
      href: href,
      backgroundSrc: backgroundSrc,
      currentCampaignId: campaignId || "general",
      currentCampaign: _currentCampaign,
      updatedTime,
      userData,
      setUserData,
      onCampaignChange: memoCampaignChange,
      setUpdatedTime: setUpdatedTime,
      dataAdapter: dataAdapter,
      campaignDateRange,
      statistics,
      setStatistics,
    };
  }, [
    backgroundSrc,
    currentCampaign,
    campaignId,
    filteredCampaigns,
    href,
    updatedTime,
    userData,
    dataAdapter,
    memoCampaignChange,
    campaignDateRange,
    statistics,
    setStatistics,
  ]);

  return (
    <TradingLeaderboardContext.Provider value={memoizedValue}>
      {children}
    </TradingLeaderboardContext.Provider>
  );
};

export const useTradingLeaderboardContext = () => {
  const ctx = useContext<TradingLeaderboardState>(TradingLeaderboardContext);
  return ctx;
};
