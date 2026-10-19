import type { GeneratedType } from "@cosmjs/proto-signing";
import type { DenomOracleExchangeRatePair as DenomOracleExchangeRatePair_type, QueryActivesRequest as QueryActivesRequest_type, QueryActivesResponse as QueryActivesResponse_type, QueryExchangeRateRequest as QueryExchangeRateRequest_type, QueryExchangeRateResponse as QueryExchangeRateResponse_type, QueryExchangeRatesRequest as QueryExchangeRatesRequest_type, QueryExchangeRatesResponse as QueryExchangeRatesResponse_type, QueryFeederDelegationRequest as QueryFeederDelegationRequest_type, QueryFeederDelegationResponse as QueryFeederDelegationResponse_type, QueryParamsRequest as QueryParamsRequest_type, QueryParamsResponse as QueryParamsResponse_type, QueryPriceSnapshotHistoryRequest as QueryPriceSnapshotHistoryRequest_type, QueryPriceSnapshotHistoryResponse as QueryPriceSnapshotHistoryResponse_type, QuerySlashWindowRequest as QuerySlashWindowRequest_type, QuerySlashWindowResponse as QuerySlashWindowResponse_type, QueryTwapsRequest as QueryTwapsRequest_type, QueryTwapsResponse as QueryTwapsResponse_type, QueryVotePenaltyCounterRequest as QueryVotePenaltyCounterRequest_type, QueryVotePenaltyCounterResponse as QueryVotePenaltyCounterResponse_type, QueryVoteTargetsRequest as QueryVoteTargetsRequest_type, QueryVoteTargetsResponse as QueryVoteTargetsResponse_type } from "../../types/oracle";
import type { MessageFns } from "../common";
export interface QueryExchangeRateRequest extends QueryExchangeRateRequest_type {
}
export interface QueryExchangeRateResponse extends QueryExchangeRateResponse_type {
}
export interface QueryExchangeRatesRequest extends QueryExchangeRatesRequest_type {
}
export interface DenomOracleExchangeRatePair extends DenomOracleExchangeRatePair_type {
}
export interface QueryExchangeRatesResponse extends QueryExchangeRatesResponse_type {
}
export interface QueryActivesRequest extends QueryActivesRequest_type {
}
export interface QueryActivesResponse extends QueryActivesResponse_type {
}
export interface QueryVoteTargetsRequest extends QueryVoteTargetsRequest_type {
}
export interface QueryVoteTargetsResponse extends QueryVoteTargetsResponse_type {
}
export interface QueryPriceSnapshotHistoryRequest extends QueryPriceSnapshotHistoryRequest_type {
}
export interface QueryPriceSnapshotHistoryResponse extends QueryPriceSnapshotHistoryResponse_type {
}
export interface QueryTwapsRequest extends QueryTwapsRequest_type {
}
export interface QueryTwapsResponse extends QueryTwapsResponse_type {
}
export interface QueryFeederDelegationRequest extends QueryFeederDelegationRequest_type {
}
export interface QueryFeederDelegationResponse extends QueryFeederDelegationResponse_type {
}
export interface QueryVotePenaltyCounterRequest extends QueryVotePenaltyCounterRequest_type {
}
export interface QueryVotePenaltyCounterResponse extends QueryVotePenaltyCounterResponse_type {
}
export interface QuerySlashWindowRequest extends QuerySlashWindowRequest_type {
}
export interface QuerySlashWindowResponse extends QuerySlashWindowResponse_type {
}
export interface QueryParamsRequest extends QueryParamsRequest_type {
}
export interface QueryParamsResponse extends QueryParamsResponse_type {
}
export declare const QueryExchangeRateRequest: MessageFns<QueryExchangeRateRequest, "paxprotocol.paxchain.oracle.QueryExchangeRateRequest">;
export declare const QueryExchangeRateResponse: MessageFns<QueryExchangeRateResponse, "paxprotocol.paxchain.oracle.QueryExchangeRateResponse">;
export declare const QueryExchangeRatesRequest: MessageFns<QueryExchangeRatesRequest, "paxprotocol.paxchain.oracle.QueryExchangeRatesRequest">;
export declare const DenomOracleExchangeRatePair: MessageFns<DenomOracleExchangeRatePair, "paxprotocol.paxchain.oracle.DenomOracleExchangeRatePair">;
export declare const QueryExchangeRatesResponse: MessageFns<QueryExchangeRatesResponse, "paxprotocol.paxchain.oracle.QueryExchangeRatesResponse">;
export declare const QueryActivesRequest: MessageFns<QueryActivesRequest, "paxprotocol.paxchain.oracle.QueryActivesRequest">;
export declare const QueryActivesResponse: MessageFns<QueryActivesResponse, "paxprotocol.paxchain.oracle.QueryActivesResponse">;
export declare const QueryVoteTargetsRequest: MessageFns<QueryVoteTargetsRequest, "paxprotocol.paxchain.oracle.QueryVoteTargetsRequest">;
export declare const QueryVoteTargetsResponse: MessageFns<QueryVoteTargetsResponse, "paxprotocol.paxchain.oracle.QueryVoteTargetsResponse">;
export declare const QueryPriceSnapshotHistoryRequest: MessageFns<QueryPriceSnapshotHistoryRequest, "paxprotocol.paxchain.oracle.QueryPriceSnapshotHistoryRequest">;
export declare const QueryPriceSnapshotHistoryResponse: MessageFns<QueryPriceSnapshotHistoryResponse, "paxprotocol.paxchain.oracle.QueryPriceSnapshotHistoryResponse">;
export declare const QueryTwapsRequest: MessageFns<QueryTwapsRequest, "paxprotocol.paxchain.oracle.QueryTwapsRequest">;
export declare const QueryTwapsResponse: MessageFns<QueryTwapsResponse, "paxprotocol.paxchain.oracle.QueryTwapsResponse">;
export declare const QueryFeederDelegationRequest: MessageFns<QueryFeederDelegationRequest, "paxprotocol.paxchain.oracle.QueryFeederDelegationRequest">;
export declare const QueryFeederDelegationResponse: MessageFns<QueryFeederDelegationResponse, "paxprotocol.paxchain.oracle.QueryFeederDelegationResponse">;
export declare const QueryVotePenaltyCounterRequest: MessageFns<QueryVotePenaltyCounterRequest, "paxprotocol.paxchain.oracle.QueryVotePenaltyCounterRequest">;
export declare const QueryVotePenaltyCounterResponse: MessageFns<QueryVotePenaltyCounterResponse, "paxprotocol.paxchain.oracle.QueryVotePenaltyCounterResponse">;
export declare const QuerySlashWindowRequest: MessageFns<QuerySlashWindowRequest, "paxprotocol.paxchain.oracle.QuerySlashWindowRequest">;
export declare const QuerySlashWindowResponse: MessageFns<QuerySlashWindowResponse, "paxprotocol.paxchain.oracle.QuerySlashWindowResponse">;
export declare const QueryParamsRequest: MessageFns<QueryParamsRequest, "paxprotocol.paxchain.oracle.QueryParamsRequest">;
export declare const QueryParamsResponse: MessageFns<QueryParamsResponse, "paxprotocol.paxchain.oracle.QueryParamsResponse">;
export declare const registry: Array<[string, GeneratedType]>;
export declare const aminoConverters: {
    "/paxprotocol.paxchain.oracle.QueryExchangeRateRequest": {
        aminoType: string;
        toAmino: (message: QueryExchangeRateRequest) => {
            denom: string;
        };
        fromAmino: (object: QueryExchangeRateRequest) => {
            denom: string;
        };
    };
    "/paxprotocol.paxchain.oracle.QueryExchangeRateResponse": {
        aminoType: string;
        toAmino: (message: QueryExchangeRateResponse) => {
            oracle_exchange_rate?: import("../../types/oracle").OracleExchangeRate;
        };
        fromAmino: (object: QueryExchangeRateResponse) => {
            oracle_exchange_rate?: import("../../types/oracle").OracleExchangeRate;
        };
    };
    "/paxprotocol.paxchain.oracle.QueryExchangeRatesRequest": {
        aminoType: string;
        toAmino: (message: QueryExchangeRatesRequest) => {};
        fromAmino: (object: QueryExchangeRatesRequest) => {};
    };
    "/paxprotocol.paxchain.oracle.DenomOracleExchangeRatePair": {
        aminoType: string;
        toAmino: (message: DenomOracleExchangeRatePair) => {
            denom: string;
            oracle_exchange_rate?: import("../../types/oracle").OracleExchangeRate;
        };
        fromAmino: (object: DenomOracleExchangeRatePair) => {
            denom: string;
            oracle_exchange_rate?: import("../../types/oracle").OracleExchangeRate;
        };
    };
    "/paxprotocol.paxchain.oracle.QueryExchangeRatesResponse": {
        aminoType: string;
        toAmino: (message: QueryExchangeRatesResponse) => {
            denom_oracle_exchange_rate_pairs: DenomOracleExchangeRatePair_type[];
        };
        fromAmino: (object: QueryExchangeRatesResponse) => {
            denom_oracle_exchange_rate_pairs: DenomOracleExchangeRatePair_type[];
        };
    };
    "/paxprotocol.paxchain.oracle.QueryActivesRequest": {
        aminoType: string;
        toAmino: (message: QueryActivesRequest) => {};
        fromAmino: (object: QueryActivesRequest) => {};
    };
    "/paxprotocol.paxchain.oracle.QueryActivesResponse": {
        aminoType: string;
        toAmino: (message: QueryActivesResponse) => {
            actives: string[];
        };
        fromAmino: (object: QueryActivesResponse) => {
            actives: string[];
        };
    };
    "/paxprotocol.paxchain.oracle.QueryVoteTargetsRequest": {
        aminoType: string;
        toAmino: (message: QueryVoteTargetsRequest) => {};
        fromAmino: (object: QueryVoteTargetsRequest) => {};
    };
    "/paxprotocol.paxchain.oracle.QueryVoteTargetsResponse": {
        aminoType: string;
        toAmino: (message: QueryVoteTargetsResponse) => {
            vote_targets: string[];
        };
        fromAmino: (object: QueryVoteTargetsResponse) => {
            vote_targets: string[];
        };
    };
    "/paxprotocol.paxchain.oracle.QueryPriceSnapshotHistoryRequest": {
        aminoType: string;
        toAmino: (message: QueryPriceSnapshotHistoryRequest) => {};
        fromAmino: (object: QueryPriceSnapshotHistoryRequest) => {};
    };
    "/paxprotocol.paxchain.oracle.QueryPriceSnapshotHistoryResponse": {
        aminoType: string;
        toAmino: (message: QueryPriceSnapshotHistoryResponse) => {
            price_snapshots: import("../../types/oracle").PriceSnapshot[];
        };
        fromAmino: (object: QueryPriceSnapshotHistoryResponse) => {
            price_snapshots: import("../../types/oracle").PriceSnapshot[];
        };
    };
    "/paxprotocol.paxchain.oracle.QueryTwapsRequest": {
        aminoType: string;
        toAmino: (message: QueryTwapsRequest) => {
            lookback_seconds: number;
        };
        fromAmino: (object: QueryTwapsRequest) => {
            lookback_seconds: number;
        };
    };
    "/paxprotocol.paxchain.oracle.QueryTwapsResponse": {
        aminoType: string;
        toAmino: (message: QueryTwapsResponse) => {
            oracle_twaps: import("../../types/oracle").OracleTwap[];
        };
        fromAmino: (object: QueryTwapsResponse) => {
            oracle_twaps: import("../../types/oracle").OracleTwap[];
        };
    };
    "/paxprotocol.paxchain.oracle.QueryFeederDelegationRequest": {
        aminoType: string;
        toAmino: (message: QueryFeederDelegationRequest) => {
            validator_addr: string;
        };
        fromAmino: (object: QueryFeederDelegationRequest) => {
            validator_addr: string;
        };
    };
    "/paxprotocol.paxchain.oracle.QueryFeederDelegationResponse": {
        aminoType: string;
        toAmino: (message: QueryFeederDelegationResponse) => {
            feeder_addr: string;
        };
        fromAmino: (object: QueryFeederDelegationResponse) => {
            feeder_addr: string;
        };
    };
    "/paxprotocol.paxchain.oracle.QueryVotePenaltyCounterRequest": {
        aminoType: string;
        toAmino: (message: QueryVotePenaltyCounterRequest) => {
            validator_addr: string;
        };
        fromAmino: (object: QueryVotePenaltyCounterRequest) => {
            validator_addr: string;
        };
    };
    "/paxprotocol.paxchain.oracle.QueryVotePenaltyCounterResponse": {
        aminoType: string;
        toAmino: (message: QueryVotePenaltyCounterResponse) => {
            vote_penalty_counter?: import("../../types/oracle").VotePenaltyCounter;
        };
        fromAmino: (object: QueryVotePenaltyCounterResponse) => {
            vote_penalty_counter?: import("../../types/oracle").VotePenaltyCounter;
        };
    };
    "/paxprotocol.paxchain.oracle.QuerySlashWindowRequest": {
        aminoType: string;
        toAmino: (message: QuerySlashWindowRequest) => {};
        fromAmino: (object: QuerySlashWindowRequest) => {};
    };
    "/paxprotocol.paxchain.oracle.QuerySlashWindowResponse": {
        aminoType: string;
        toAmino: (message: QuerySlashWindowResponse) => {
            window_progress: number;
        };
        fromAmino: (object: QuerySlashWindowResponse) => {
            window_progress: number;
        };
    };
    "/paxprotocol.paxchain.oracle.QueryParamsRequest": {
        aminoType: string;
        toAmino: (message: QueryParamsRequest) => {};
        fromAmino: (object: QueryParamsRequest) => {};
    };
    "/paxprotocol.paxchain.oracle.QueryParamsResponse": {
        aminoType: string;
        toAmino: (message: QueryParamsResponse) => {
            params?: import("../../types/oracle").Params;
        };
        fromAmino: (object: QueryParamsResponse) => {
            params?: import("../../types/oracle").Params;
        };
    };
};
