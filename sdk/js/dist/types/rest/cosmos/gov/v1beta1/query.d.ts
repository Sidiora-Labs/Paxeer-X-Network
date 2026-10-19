import * as fm from "../../../fetch";
import type { QueryDepositRequest, QueryDepositResponse, QueryDepositsRequest, QueryDepositsResponse, QueryParamsRequest, QueryParamsResponse, QueryProposalRequest, QueryProposalResponse, QueryProposalsRequest, QueryProposalsResponse, QueryTallyResultRequest, QueryTallyResultResponse, QueryVoteRequest, QueryVoteResponse, QueryVotesRequest, QueryVotesResponse } from "../../../../types/cosmos/gov/v1beta1/query.ts";
export declare class Query {
    static Proposal(req: QueryProposalRequest, initReq?: fm.InitReq): Promise<QueryProposalResponse>;
    static Proposals(req: QueryProposalsRequest, initReq?: fm.InitReq): Promise<QueryProposalsResponse>;
    static Vote(req: QueryVoteRequest, initReq?: fm.InitReq): Promise<QueryVoteResponse>;
    static Votes(req: QueryVotesRequest, initReq?: fm.InitReq): Promise<QueryVotesResponse>;
    static Params(req: QueryParamsRequest, initReq?: fm.InitReq): Promise<QueryParamsResponse>;
    static Deposit(req: QueryDepositRequest, initReq?: fm.InitReq): Promise<QueryDepositResponse>;
    static Deposits(req: QueryDepositsRequest, initReq?: fm.InitReq): Promise<QueryDepositsResponse>;
    static TallyResult(req: QueryTallyResultRequest, initReq?: fm.InitReq): Promise<QueryTallyResultResponse>;
}
