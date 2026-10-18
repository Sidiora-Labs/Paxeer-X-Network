export interface VaultSupportedChain {
  chain_id: string;
  chain_name: string;
}

export type VaultTimeRange = "24h" | "7d" | "30d" | "all_time";

export type VaultStatus = "pre_launch" | "live" | "closing" | "closed";

export interface VaultInfo {
  vault_id: string;
  vault_address: string;
  vault_type: "protocol" | "community" | "user";
  vault_name: string;
  description: string;
  sp_address: string;
  sp_name: string | null;
  asset: string;
  vault_age: number | null;
  status: VaultStatus;
  vault_start_time: number;
  performance_fee_rate: number;
  supported_chains: VaultSupportedChain[];
  tvl: number;
  valid_hpr: number;
  "30d_apy": number;
  recovery_30d_apy: number;
  lifetime_apy: number;
  vault_lifetime_net_pnl: number;
  lp_counts: number;
  total_main_shares: number;
  est_main_share_price: number;
  lock_duration: number;
  min_deposit_amount: number;
  min_withdrawal_amount: number;
  gate_threshold_pct: number;
  gate_triggered: boolean;
  broker_id: string;
}

export interface VaultLpPerformance {
  time_range: VaultTimeRange;
  tvl_max_drawdown: number;
  incremental_net_pnl: number;
  pnl_max_drawdown: number;
}

export interface VaultLpInfo {
  vault_id: string;
  lp_nav: number;
  lp_tvl: number;
  total_main_shares: number;
  available_main_shares: number;
  potential_pnl: number;
}

export interface VaultOverallInfo {
  strategy_vaults_tvl: number;
  strategy_vaults_lifetime_net_pnl: number;
  strategy_vaults_count: number;
  strategy_vaults_lp_count: number;
}

export interface VaultOperation {
  type: OperationType;
  vault_id: string;
  amount_change: number;
  created_time: string;
  status: string;
}

export enum RoleType {
  LP = "lp",
  SP = "sp",
}

export enum OperationType {
  DEPOSIT = "deposit",
  WITHDRAWAL = "withdrawal",
}

export type VaultsPageConfig = {
  headerImage?: React.ReactNode;
  /**
   * Custom broker_ids filter for overall vault info API.
   * If not provided, defaults to "paxeer-network,{current_broker_id}"
   * @example "paxeer-network,woofi_pro,aden"
   */
  overallInfoBrokerIds?: string;
};
