import type { Coin, DecCoin } from "../../base/v1beta1/coin";
export interface Params {
    community_tax: string;
    base_proposer_reward: string;
    bonus_proposer_reward: string;
    withdraw_addr_enabled: boolean;
}
export interface ValidatorHistoricalRewards {
    cumulative_reward_ratio: DecCoin[];
    reference_count: number;
}
export interface ValidatorCurrentRewards {
    rewards: DecCoin[];
    period: number;
}
export interface ValidatorAccumulatedCommission {
    commission: DecCoin[];
}
export interface ValidatorOutstandingRewards {
    rewards: DecCoin[];
}
export interface ValidatorSlashEvent {
    validator_period: number;
    fraction: string;
}
export interface ValidatorSlashEvents {
    validator_slash_events: ValidatorSlashEvent[];
}
export interface FeePool {
    community_pool: DecCoin[];
}
export interface CommunityPoolSpendProposal {
    title: string;
    description: string;
    recipient: string;
    amount: Coin[];
}
export interface DelegatorStartingInfo {
    previous_period: number;
    stake: string;
    height: number;
}
export interface DelegationDelegatorReward {
    validator_address: string;
    reward: DecCoin[];
}
export interface CommunityPoolSpendProposalWithDeposit {
    title: string;
    description: string;
    recipient: string;
    amount: string;
    deposit: string;
}
