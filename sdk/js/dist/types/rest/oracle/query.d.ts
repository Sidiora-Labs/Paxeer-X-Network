import * as fm from "../fetch";
import type { QueryActivesRequest, QueryActivesResponse, QueryExchangeRateRequest, QueryExchangeRateResponse, QueryExchangeRatesRequest, QueryExchangeRatesResponse, QueryFeederDelegationRequest, QueryFeederDelegationResponse, QueryParamsRequest, QueryParamsResponse, QueryPriceSnapshotHistoryRequest, QueryPriceSnapshotHistoryResponse, QuerySlashWindowRequest, QuerySlashWindowResponse, QueryTwapsRequest, QueryTwapsResponse, QueryVotePenaltyCounterRequest, QueryVotePenaltyCounterResponse, QueryVoteTargetsRequest, QueryVoteTargetsResponse } from "../../types/oracle/query.ts";
export declare class Query {
    static ExchangeRate(req: QueryExchangeRateRequest, initReq?: fm.InitReq): Promise<QueryExchangeRateResponse>;
    static ExchangeRates(req: QueryExchangeRatesRequest, initReq?: fm.InitReq): Promise<QueryExchangeRatesResponse>;
    static Actives(req: QueryActivesRequest, initReq?: fm.InitReq): Promise<QueryActivesResponse>;
    static VoteTargets(req: QueryVoteTargetsRequest, initReq?: fm.InitReq): Promise<QueryVoteTargetsResponse>;
    static PriceSnapshotHistory(req: QueryPriceSnapshotHistoryRequest, initReq?: fm.InitReq): Promise<QueryPriceSnapshotHistoryResponse>;
    static Twaps(req: QueryTwapsRequest, initReq?: fm.InitReq): Promise<QueryTwapsResponse>;
    static FeederDelegation(req: QueryFeederDelegationRequest, initReq?: fm.InitReq): Promise<QueryFeederDelegationResponse>;
    static VotePenaltyCounter(req: QueryVotePenaltyCounterRequest, initReq?: fm.InitReq): Promise<QueryVotePenaltyCounterResponse>;
    static SlashWindow(req: QuerySlashWindowRequest, initReq?: fm.InitReq): Promise<QuerySlashWindowResponse>;
    static Params(req: QueryParamsRequest, initReq?: fm.InitReq): Promise<QueryParamsResponse>;
}
