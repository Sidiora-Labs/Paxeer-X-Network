import type { PageRequest, PageResponse } from "../../base/query/v1beta1/pagination";
import type { DecCoin } from "../../base/v1beta1/coin";
import type { DelegationDelegatorReward, Params, ValidatorAccumulatedCommission, ValidatorOutstandingRewards, ValidatorSlashEvent } from "./distribution";
export type QueryParamsRequest = {};
export interface QueryParamsResponse {
    /** params defines the parameters of the module. */
    params?: Params;
}
export interface QueryValidatorOutstandingRewardsRequest {
    /** validator_address defines the validator address to query for. */
    validator_address: string;
}
export interface QueryValidatorOutstandingRewardsResponse {
    rewards?: ValidatorOutstandingRewards;
}
export interface QueryValidatorCommissionRequest {
    /** validator_address defines the validator address to query for. */
    validator_address: string;
}
export interface QueryValidatorCommissionResponse {
    /** commission defines the commision the validator received. */
    commission?: ValidatorAccumulatedCommission;
}
export interface QueryValidatorSlashesRequest {
    /** validator_address defines the validator address to query for. */
    validator_address: string;
    /** starting_height defines the optional starting height to query the slashes. */
    starting_height: number;
    /** starting_height defines the optional ending height to query the slashes. */
    ending_height: number;
    /** pagination defines an optional pagination for the request. */
    pagination?: PageRequest;
}
export interface QueryValidatorSlashesResponse {
    /** slashes defines the slashes the validator received. */
    slashes: ValidatorSlashEvent[];
    /** pagination defines the pagination in the response. */
    pagination?: PageResponse;
}
export interface QueryDelegationRewardsRequest {
    /** delegator_address defines the delegator address to query for. */
    delegator_address: string;
    /** validator_address defines the validator address to query for. */
    validator_address: string;
}
export interface QueryDelegationRewardsResponse {
    /** rewards defines the rewards accrued by a delegation. */
    rewards: DecCoin[];
}
export interface QueryDelegationTotalRewardsRequest {
    /** delegator_address defines the delegator address to query for. */
    delegator_address: string;
}
export interface QueryDelegationTotalRewardsResponse {
    /** rewards defines all the rewards accrued by a delegator. */
    rewards: DelegationDelegatorReward[];
    /** total defines the sum of all the rewards. */
    total: DecCoin[];
}
export interface QueryDelegatorValidatorsRequest {
    /** delegator_address defines the delegator address to query for. */
    delegator_address: string;
}
export interface QueryDelegatorValidatorsResponse {
    /** validators defines the validators a delegator is delegating for. */
    validators: string[];
}
export interface QueryDelegatorWithdrawAddressRequest {
    /** delegator_address defines the delegator address to query for. */
    delegator_address: string;
}
export interface QueryDelegatorWithdrawAddressResponse {
    /** withdraw_address defines the delegator address to query for. */
    withdraw_address: string;
}
export type QueryCommunityPoolRequest = {};
export interface QueryCommunityPoolResponse {
    /** pool defines community pool's coins. */
    pool: DecCoin[];
}
