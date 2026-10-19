import type { PageRequest, PageResponse } from "../../base/query/v1beta1/pagination";
import type { Deposit, DepositParams, Proposal, ProposalStatus, TallyParams, TallyResult, Vote, VotingParams } from "./gov";
export interface QueryProposalRequest {
    /** proposal_id defines the unique id of the proposal. */
    proposal_id: number;
}
export interface QueryProposalResponse {
    proposal?: Proposal;
}
export interface QueryProposalsRequest {
    /** proposal_status defines the status of the proposals. */
    proposal_status: ProposalStatus;
    /** voter defines the voter address for the proposals. */
    voter: string;
    /** depositor defines the deposit addresses from the proposals. */
    depositor: string;
    /** pagination defines an optional pagination for the request. */
    pagination?: PageRequest;
}
export interface QueryProposalsResponse {
    proposals: Proposal[];
    /** pagination defines the pagination in the response. */
    pagination?: PageResponse;
}
export interface QueryVoteRequest {
    /** proposal_id defines the unique id of the proposal. */
    proposal_id: number;
    /** voter defines the oter address for the proposals. */
    voter: string;
}
export interface QueryVoteResponse {
    /** vote defined the queried vote. */
    vote?: Vote;
}
export interface QueryVotesRequest {
    /** proposal_id defines the unique id of the proposal. */
    proposal_id: number;
    /** pagination defines an optional pagination for the request. */
    pagination?: PageRequest;
}
export interface QueryVotesResponse {
    /** votes defined the queried votes. */
    votes: Vote[];
    /** pagination defines the pagination in the response. */
    pagination?: PageResponse;
}
export interface QueryParamsRequest {
    /**
     * params_type defines which parameters to query for, can be one of "voting",
     * "tallying" or "deposit".
     */
    params_type: string;
}
export interface QueryParamsResponse {
    /** voting_params defines the parameters related to voting. */
    voting_params?: VotingParams;
    /** deposit_params defines the parameters related to deposit. */
    deposit_params?: DepositParams;
    /** tally_params defines the parameters related to tally. */
    tally_params?: TallyParams;
}
export interface QueryDepositRequest {
    /** proposal_id defines the unique id of the proposal. */
    proposal_id: number;
    /** depositor defines the deposit addresses from the proposals. */
    depositor: string;
}
export interface QueryDepositResponse {
    /** deposit defines the requested deposit. */
    deposit?: Deposit;
}
export interface QueryDepositsRequest {
    /** proposal_id defines the unique id of the proposal. */
    proposal_id: number;
    /** pagination defines an optional pagination for the request. */
    pagination?: PageRequest;
}
export interface QueryDepositsResponse {
    deposits: Deposit[];
    /** pagination defines the pagination in the response. */
    pagination?: PageResponse;
}
export interface QueryTallyResultRequest {
    /** proposal_id defines the unique id of the proposal. */
    proposal_id: number;
}
export interface QueryTallyResultResponse {
    /** tally defines the requested tally. */
    tally?: TallyResult;
}
