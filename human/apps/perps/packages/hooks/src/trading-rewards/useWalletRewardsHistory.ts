import { TWType } from "./types";
import { useAccount } from "../useAccount";
import { useQuery } from "../useQuery";

export type WalletRewardsItem = {
  epoch_id: number;
  wallet_epoch_avg_staked: number;
  epoch_token: string;
  max_reward_amount: number;
  reward_status: string;
  r_wallet: number;
};

export type WalletRewards = {
  wallet_lifetime_trading_rewards_order: string;
  wallet_lifetime_trading_rewards_escrow: string;
  wallet_pending_trading_rewards_order: string;
  wallet_pending_trading_rewards_escrow: string;
  rows: WalletRewardsItem[];
};

export type WalletRewardsHistoryReturns = [
  WalletRewards | undefined,
  {
    refresh: () => void;
    error?: any;
  }
];

export const useWalletRewardsHistory = (
  type: TWType
): WalletRewardsHistoryReturns => {
  const { account } = useAccount();

  const address = account.address;

  const path =
    type === TWType.normal
      ? `/v1/public/trading_rewards/wallet_rewards_history?address=${address}`
      : `/v1/public/market_making_rewards/group_rewards_history?address=${address}`;

  const {
    data,
    mutate: refresh,
    error,
  } = useQuery(address !== undefined ? path : "", {
    formatter: (res) => {
      return {
        wallet_lifetime_trading_rewards_order:
          res?.wallet_lifetime_trading_rewards_order ?? 0,
        wallet_lifetime_trading_rewards_escrow:
          res?.wallet_lifetime_trading_rewards_escrow ?? 0,
        wallet_pending_trading_rewards_order:
          res.wallet_pending_trading_rewards_order ?? 0,
        wallet_pending_trading_rewards_escrow:
          res.wallet_pending_trading_rewards_escrow ?? 0,
        rows: (res?.rows || []).map((item: any) => ({
          ...item,
          r_wallet: item?.total_reward || item?.r_wallet,
        })),
      } as WalletRewards;
    },
  });
  return [data, { refresh, error }];
};
