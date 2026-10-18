import { useMemo } from "react";
import { useAccount } from "@paxeer-network/hooks";
import { CampaignConfig } from "../type";
import { getTicketPrizePool } from "../utils";

export const usePricePoolScript = (campaign: CampaignConfig) => {
  const { state } = useAccount();
  const ticketPrizePool = useMemo(
    () => getTicketPrizePool(campaign),
    [campaign],
  );

  const highlightPool = useMemo(() => {
    if (!campaign?.highlight_pool_id) {
      return null;
    }
    return campaign?.prize_pools?.find(
      (pool) => pool.pool_id === campaign.highlight_pool_id,
    );
  }, [campaign]);

  return {
    ticketPrizePool,
    highlightPool,
    status: state.status,
  };
};

export type PricePoolScriptReturn = ReturnType<typeof usePricePoolScript>;
