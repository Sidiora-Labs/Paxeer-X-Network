import type { DecCoin } from "../../base/v1beta1/coin";
import type { DelegatorStartingInfo, FeePool, Params, ValidatorAccumulatedCommission, ValidatorCurrentRewards, ValidatorHistoricalRewards, ValidatorSlashEvent } from "./distribution";
export interface DelegatorWithdrawInfo {
    /** delegator_address is the address of the delegator. */
    delegator_address: string;
    /** withdraw_address is the address to withdraw the delegation rewards to. */
    withdraw_address: string;
}
export interface ValidatorOutstandingRewardsRecord {
    /** validator_address is the address of the validator. */
    validator_address: string;
    /** outstanding_rewards represents the oustanding rewards of a validator. */
    outstanding_rewards: DecCoin[];
}
export interface ValidatorAccumulatedCommissionRecord {
    /** validator_address is the address of the validator. */
    validator_address: string;
    /** accumulated is the accumulated commission of a validator. */
    accumulated?: ValidatorAccumulatedCommission;
}
export interface ValidatorHistoricalRewardsRecord {
    /** validator_address is the address of the validator. */
    validator_address: string;
    /** period defines the period the historical rewards apply to. */
    period: number;
    /** rewards defines the historical rewards of a validator. */
    rewards?: ValidatorHistoricalRewards;
}
export interface ValidatorCurrentRewardsRecord {
    /** validator_address is the address of the validator. */
    validator_address: string;
    /** rewards defines the current rewards of a validator. */
    rewards?: ValidatorCurrentRewards;
}
export interface DelegatorStartingInfoRecord {
    /** delegator_address is the address of the delegator. */
    delegator_address: string;
    /** validator_address is the address of the validator. */
    validator_address: string;
    /** starting_info defines the starting info of a delegator. */
    starting_info?: DelegatorStartingInfo;
}
export interface ValidatorSlashEventRecord {
    /** validator_address is the address of the validator. */
    validator_address: string;
    /** height defines the block height at which the slash event occured. */
    height: number;
    /** period is the period of the slash event. */
    period: number;
    /** validator_slash_event describes the slash event. */
    validator_slash_event?: ValidatorSlashEvent;
}
export interface GenesisState {
    /** params defines all the paramaters of the module. */
    params?: Params;
    /** fee_pool defines the fee pool at genesis. */
    fee_pool?: FeePool;
    /** fee_pool defines the delegator withdraw infos at genesis. */
    delegator_withdraw_infos: DelegatorWithdrawInfo[];
    /** fee_pool defines the previous proposer at genesis. */
    previous_proposer: string;
    /** fee_pool defines the outstanding rewards of all validators at genesis. */
    outstanding_rewards: ValidatorOutstandingRewardsRecord[];
    /** fee_pool defines the accumulated commisions of all validators at genesis. */
    validator_accumulated_commissions: ValidatorAccumulatedCommissionRecord[];
    /** fee_pool defines the historical rewards of all validators at genesis. */
    validator_historical_rewards: ValidatorHistoricalRewardsRecord[];
    /** fee_pool defines the current rewards of all validators at genesis. */
    validator_current_rewards: ValidatorCurrentRewardsRecord[];
    /** fee_pool defines the delegator starting infos at genesis. */
    delegator_starting_infos: DelegatorStartingInfoRecord[];
    /** fee_pool defines the validator slash events at genesis. */
    validator_slash_events: ValidatorSlashEventRecord[];
}
