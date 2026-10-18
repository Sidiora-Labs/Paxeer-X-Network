import { useMemo, useCallback } from "react";
import { useQuery, useMutation, useAccount } from "@paxeer-network/hooks";
import { AccountStatusEnum } from "@paxeer-network/types";
import { toast } from "@paxeer-network/ui";
import { useTradingLeaderboardContext } from "../provider";
import { getCurrentTierIndex } from "./pricePool/utils";
import { UserCampaignsResponse } from "./type";
import { getTotalPrizePool } from "./utils";

/**
 * Hook for managing campaigns data and statistics
 * TODO: Replace mock data with real API calls when backend is ready
 */
export const useCampaignsScript = () => {
  const {
    campaigns = [],
    currentCampaignId,
    onCampaignChange,
    currentCampaign,
    userData,
    backgroundSrc,
    statistics,
  } = useTradingLeaderboardContext();

  const isCampaignEnded = useMemo(() => {
    return (
      currentCampaign?.end_time &&
      currentCampaign?.end_time < new Date().toISOString()
    );
  }, [currentCampaign]);

  const { state } = useAccount();

  const { data: userCampaigns, mutate: refreshUserCampaigns } =
    useQuery<UserCampaignsResponse>(
      currentCampaignId !== "general" && state.address
        ? `https://api.paxeer-network.org/v1/public/campaigns?address=${state.address}`
        : null,
      { revalidateOnFocus: false },
    );

  const isParticipated = useMemo(() => {
    const target = userCampaigns?.find((item) => item.id == currentCampaignId);
    return !!target;
  }, [userCampaigns, currentCampaignId]);

  const shouldShowJoinButton = useMemo(() => {
    // return false;
    return !!state.address && !isCampaignEnded && !isParticipated;
  }, [state.address, isCampaignEnded, isParticipated]);

  const [doJoinCampaign, { isMutating: isJoining, error: joinError }] =
    useMutation(`/v1/client/campaign/sign_up`, "POST");

  const joinCampaign = useCallback(
    async (data: { campaign_id: string | number }) => {
      try {
        if (state.status < AccountStatusEnum.EnableTrading) {
          toast.error("Please enable trading to proceed.");
          return;
        }
        const result = await doJoinCampaign(data);

        // hardcode for 128 campaign
        if (String(data.campaign_id) === "128") {
          Promise.all(
            ["130", "131", "132", "133", "134", "136"].map(
              async (campaignId) => {
                await doJoinCampaign({ campaign_id: campaignId });
              },
            ),
          );
        }

        if (result?.success !== false) {
          // Refresh user campaigns data to update participation status
          await refreshUserCampaigns();
          toast.success(result?.message || "Campaign joined successfully");
          return result;
        } else {
          toast.error(result?.message || "Failed to join campaign");
        }
      } catch (error) {
        console.error("Failed to join campaign:", error);
        throw error;
      }
    },
    [doJoinCampaign, refreshUserCampaigns, state.status],
  );

  const onLearnMore = () => {
    if (currentCampaign?.rule_url) {
      if (currentCampaign.rule_config?.action === "scroll") {
        document
          .getElementById(currentCampaign.rule_url)
          ?.scrollIntoView({ behavior: "smooth" });
      } else {
        window.open(currentCampaign.rule_url, "_blank");
      }
    }
  };

  const onTradeNow = () => {
    if (currentCampaign?.trading_url) {
      window.open(currentCampaign.trading_url, "_self");
    }
  };

  const canTrade = useMemo(() => {
    if (!currentCampaign) return false;
    return (
      currentCampaign?.start_time < new Date().toISOString() &&
      currentCampaign?.end_time > new Date().toISOString() &&
      state.status >= AccountStatusEnum.EnableTrading
    );
  }, [currentCampaign, state.status]);

  const tieredIndex = useMemo(() => {
    if (!currentCampaign?.tiered_prize_pools) return 0;
    return getCurrentTierIndex(
      statistics?.total_volume || 0,
      currentCampaign?.tiered_prize_pools,
    );
  }, [statistics?.total_volume, currentCampaign?.tiered_prize_pools]);

  const totalPrizePool = useMemo(() => {
    return getTotalPrizePool(currentCampaign, tieredIndex ?? undefined);
  }, [currentCampaign, tieredIndex]);

  return {
    campaigns,
    currentCampaignId,
    currentCampaign,
    onCampaignChange,
    statistics,
    userData,
    onLearnMore,
    onTradeNow,
    backgroundSrc,
    joinCampaign,
    isJoining,
    isParticipated,
    shouldShowJoinButton,
    joinError,
    canTrade,
    totalPrizePool,
    status: state.status,
  };
};

export type CampaignsScriptReturn = ReturnType<typeof useCampaignsScript>;
