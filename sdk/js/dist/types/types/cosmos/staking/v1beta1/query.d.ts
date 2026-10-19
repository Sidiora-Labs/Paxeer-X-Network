import type { PageRequest, PageResponse } from "../../base/query/v1beta1/pagination";
import type { DelegationResponse, HistoricalInfo, Params, Pool, RedelegationResponse, UnbondingDelegation, Validator } from "./staking";
export interface QueryValidatorsRequest {
    /** status enables to query for validators matching a given status. */
    status: string;
    /** pagination defines an optional pagination for the request. */
    pagination?: PageRequest;
}
export interface QueryValidatorsResponse {
    /** validators contains all the queried validators. */
    validators: Validator[];
    /** pagination defines the pagination in the response. */
    pagination?: PageResponse;
}
export interface QueryValidatorRequest {
    /** validator_addr defines the validator address to query for. */
    validator_addr: string;
}
export interface QueryValidatorResponse {
    /** validator defines the the validator info. */
    validator?: Validator;
}
export interface QueryValidatorDelegationsRequest {
    /** validator_addr defines the validator address to query for. */
    validator_addr: string;
    /** pagination defines an optional pagination for the request. */
    pagination?: PageRequest;
}
export interface QueryValidatorDelegationsResponse {
    delegation_responses: DelegationResponse[];
    /** pagination defines the pagination in the response. */
    pagination?: PageResponse;
}
export interface QueryValidatorUnbondingDelegationsRequest {
    /** validator_addr defines the validator address to query for. */
    validator_addr: string;
    /** pagination defines an optional pagination for the request. */
    pagination?: PageRequest;
}
export interface QueryValidatorUnbondingDelegationsResponse {
    unbonding_responses: UnbondingDelegation[];
    /** pagination defines the pagination in the response. */
    pagination?: PageResponse;
}
export interface QueryDelegationRequest {
    /** delegator_addr defines the delegator address to query for. */
    delegator_addr: string;
    /** validator_addr defines the validator address to query for. */
    validator_addr: string;
}
export interface QueryDelegationResponse {
    /** delegation_responses defines the delegation info of a delegation. */
    delegation_response?: DelegationResponse;
}
export interface QueryUnbondingDelegationRequest {
    /** delegator_addr defines the delegator address to query for. */
    delegator_addr: string;
    /** validator_addr defines the validator address to query for. */
    validator_addr: string;
}
export interface QueryUnbondingDelegationResponse {
    /** unbond defines the unbonding information of a delegation. */
    unbond?: UnbondingDelegation;
}
export interface QueryDelegatorDelegationsRequest {
    /** delegator_addr defines the delegator address to query for. */
    delegator_addr: string;
    /** pagination defines an optional pagination for the request. */
    pagination?: PageRequest;
}
export interface QueryDelegatorDelegationsResponse {
    /** delegation_responses defines all the delegations' info of a delegator. */
    delegation_responses: DelegationResponse[];
    /** pagination defines the pagination in the response. */
    pagination?: PageResponse;
}
export interface QueryDelegatorUnbondingDelegationsRequest {
    /** delegator_addr defines the delegator address to query for. */
    delegator_addr: string;
    /** pagination defines an optional pagination for the request. */
    pagination?: PageRequest;
}
export interface QueryDelegatorUnbondingDelegationsResponse {
    unbonding_responses: UnbondingDelegation[];
    /** pagination defines the pagination in the response. */
    pagination?: PageResponse;
}
export interface QueryRedelegationsRequest {
    /** delegator_addr defines the delegator address to query for. */
    delegator_addr: string;
    /** src_validator_addr defines the validator address to redelegate from. */
    src_validator_addr: string;
    /** dst_validator_addr defines the validator address to redelegate to. */
    dst_validator_addr: string;
    /** pagination defines an optional pagination for the request. */
    pagination?: PageRequest;
}
export interface QueryRedelegationsResponse {
    redelegation_responses: RedelegationResponse[];
    /** pagination defines the pagination in the response. */
    pagination?: PageResponse;
}
export interface QueryDelegatorValidatorsRequest {
    /** delegator_addr defines the delegator address to query for. */
    delegator_addr: string;
    /** pagination defines an optional pagination for the request. */
    pagination?: PageRequest;
}
export interface QueryDelegatorValidatorsResponse {
    /** validators defines the the validators' info of a delegator. */
    validators: Validator[];
    /** pagination defines the pagination in the response. */
    pagination?: PageResponse;
}
export interface QueryDelegatorValidatorRequest {
    /** delegator_addr defines the delegator address to query for. */
    delegator_addr: string;
    /** validator_addr defines the validator address to query for. */
    validator_addr: string;
}
export interface QueryDelegatorValidatorResponse {
    /** validator defines the the validator info. */
    validator?: Validator;
}
export interface QueryHistoricalInfoRequest {
    /** height defines at which height to query the historical info. */
    height: number;
}
export interface QueryHistoricalInfoResponse {
    /** hist defines the historical info at the given height. */
    hist?: HistoricalInfo;
}
export type QueryPoolRequest = {};
export interface QueryPoolResponse {
    /** pool defines the pool info. */
    pool?: Pool;
}
export type QueryParamsRequest = {};
export interface QueryParamsResponse {
    /** params holds all the parameters of this module. */
    params?: Params;
}
