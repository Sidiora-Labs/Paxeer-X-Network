import type { GeneratedType } from "@cosmjs/proto-signing";
import type { QueryDepositRequest as QueryDepositRequest_type, QueryDepositResponse as QueryDepositResponse_type, QueryDepositsRequest as QueryDepositsRequest_type, QueryDepositsResponse as QueryDepositsResponse_type, QueryParamsRequest as QueryParamsRequest_type, QueryParamsResponse as QueryParamsResponse_type, QueryProposalRequest as QueryProposalRequest_type, QueryProposalResponse as QueryProposalResponse_type, QueryProposalsRequest as QueryProposalsRequest_type, QueryProposalsResponse as QueryProposalsResponse_type, QueryTallyResultRequest as QueryTallyResultRequest_type, QueryTallyResultResponse as QueryTallyResultResponse_type, QueryVoteRequest as QueryVoteRequest_type, QueryVoteResponse as QueryVoteResponse_type, QueryVotesRequest as QueryVotesRequest_type, QueryVotesResponse as QueryVotesResponse_type } from "../../../../types/cosmos/gov/v1beta1";
import type { MessageFns } from "../../../common";
export interface QueryProposalRequest extends QueryProposalRequest_type {
}
export interface QueryProposalResponse extends QueryProposalResponse_type {
}
export interface QueryProposalsRequest extends QueryProposalsRequest_type {
}
export interface QueryProposalsResponse extends QueryProposalsResponse_type {
}
export interface QueryVoteRequest extends QueryVoteRequest_type {
}
export interface QueryVoteResponse extends QueryVoteResponse_type {
}
export interface QueryVotesRequest extends QueryVotesRequest_type {
}
export interface QueryVotesResponse extends QueryVotesResponse_type {
}
export interface QueryParamsRequest extends QueryParamsRequest_type {
}
export interface QueryParamsResponse extends QueryParamsResponse_type {
}
export interface QueryDepositRequest extends QueryDepositRequest_type {
}
export interface QueryDepositResponse extends QueryDepositResponse_type {
}
export interface QueryDepositsRequest extends QueryDepositsRequest_type {
}
export interface QueryDepositsResponse extends QueryDepositsResponse_type {
}
export interface QueryTallyResultRequest extends QueryTallyResultRequest_type {
}
export interface QueryTallyResultResponse extends QueryTallyResultResponse_type {
}
export declare const QueryProposalRequest: MessageFns<QueryProposalRequest, "cosmos.gov.v1beta1.QueryProposalRequest">;
export declare const QueryProposalResponse: MessageFns<QueryProposalResponse, "cosmos.gov.v1beta1.QueryProposalResponse">;
export declare const QueryProposalsRequest: MessageFns<QueryProposalsRequest, "cosmos.gov.v1beta1.QueryProposalsRequest">;
export declare const QueryProposalsResponse: MessageFns<QueryProposalsResponse, "cosmos.gov.v1beta1.QueryProposalsResponse">;
export declare const QueryVoteRequest: MessageFns<QueryVoteRequest, "cosmos.gov.v1beta1.QueryVoteRequest">;
export declare const QueryVoteResponse: MessageFns<QueryVoteResponse, "cosmos.gov.v1beta1.QueryVoteResponse">;
export declare const QueryVotesRequest: MessageFns<QueryVotesRequest, "cosmos.gov.v1beta1.QueryVotesRequest">;
export declare const QueryVotesResponse: MessageFns<QueryVotesResponse, "cosmos.gov.v1beta1.QueryVotesResponse">;
export declare const QueryParamsRequest: MessageFns<QueryParamsRequest, "cosmos.gov.v1beta1.QueryParamsRequest">;
export declare const QueryParamsResponse: MessageFns<QueryParamsResponse, "cosmos.gov.v1beta1.QueryParamsResponse">;
export declare const QueryDepositRequest: MessageFns<QueryDepositRequest, "cosmos.gov.v1beta1.QueryDepositRequest">;
export declare const QueryDepositResponse: MessageFns<QueryDepositResponse, "cosmos.gov.v1beta1.QueryDepositResponse">;
export declare const QueryDepositsRequest: MessageFns<QueryDepositsRequest, "cosmos.gov.v1beta1.QueryDepositsRequest">;
export declare const QueryDepositsResponse: MessageFns<QueryDepositsResponse, "cosmos.gov.v1beta1.QueryDepositsResponse">;
export declare const QueryTallyResultRequest: MessageFns<QueryTallyResultRequest, "cosmos.gov.v1beta1.QueryTallyResultRequest">;
export declare const QueryTallyResultResponse: MessageFns<QueryTallyResultResponse, "cosmos.gov.v1beta1.QueryTallyResultResponse">;
export declare const registry: Array<[string, GeneratedType]>;
export declare const aminoConverters: {
    "/cosmos.gov.v1beta1.QueryProposalRequest": {
        aminoType: string;
        toAmino: (message: QueryProposalRequest) => {
            proposal_id: number;
        };
        fromAmino: (object: QueryProposalRequest) => {
            proposal_id: number;
        };
    };
    "/cosmos.gov.v1beta1.QueryProposalResponse": {
        aminoType: string;
        toAmino: (message: QueryProposalResponse) => {
            proposal?: import("../../../../types/cosmos/gov/v1beta1").Proposal;
        };
        fromAmino: (object: QueryProposalResponse) => {
            proposal?: import("../../../../types/cosmos/gov/v1beta1").Proposal;
        };
    };
    "/cosmos.gov.v1beta1.QueryProposalsRequest": {
        aminoType: string;
        toAmino: (message: QueryProposalsRequest) => {
            proposal_status: import("../../../../types/cosmos/gov/v1beta1").ProposalStatus;
            voter: string;
            depositor: string;
            pagination?: import("../../../../types/cosmos/base/query/v1beta1").PageRequest;
        };
        fromAmino: (object: QueryProposalsRequest) => {
            proposal_status: import("../../../../types/cosmos/gov/v1beta1").ProposalStatus;
            voter: string;
            depositor: string;
            pagination?: import("../../../../types/cosmos/base/query/v1beta1").PageRequest;
        };
    };
    "/cosmos.gov.v1beta1.QueryProposalsResponse": {
        aminoType: string;
        toAmino: (message: QueryProposalsResponse) => {
            proposals: import("../../../../types/cosmos/gov/v1beta1").Proposal[];
            pagination?: import("../../../../types/cosmos/base/query/v1beta1").PageResponse;
        };
        fromAmino: (object: QueryProposalsResponse) => {
            proposals: import("../../../../types/cosmos/gov/v1beta1").Proposal[];
            pagination?: import("../../../../types/cosmos/base/query/v1beta1").PageResponse;
        };
    };
    "/cosmos.gov.v1beta1.QueryVoteRequest": {
        aminoType: string;
        toAmino: (message: QueryVoteRequest) => {
            proposal_id: number;
            voter: string;
        };
        fromAmino: (object: QueryVoteRequest) => {
            proposal_id: number;
            voter: string;
        };
    };
    "/cosmos.gov.v1beta1.QueryVoteResponse": {
        aminoType: string;
        toAmino: (message: QueryVoteResponse) => {
            vote?: import("../../../../types/cosmos/gov/v1beta1").Vote;
        };
        fromAmino: (object: QueryVoteResponse) => {
            vote?: import("../../../../types/cosmos/gov/v1beta1").Vote;
        };
    };
    "/cosmos.gov.v1beta1.QueryVotesRequest": {
        aminoType: string;
        toAmino: (message: QueryVotesRequest) => {
            proposal_id: number;
            pagination?: import("../../../../types/cosmos/base/query/v1beta1").PageRequest;
        };
        fromAmino: (object: QueryVotesRequest) => {
            proposal_id: number;
            pagination?: import("../../../../types/cosmos/base/query/v1beta1").PageRequest;
        };
    };
    "/cosmos.gov.v1beta1.QueryVotesResponse": {
        aminoType: string;
        toAmino: (message: QueryVotesResponse) => {
            votes: import("../../../../types/cosmos/gov/v1beta1").Vote[];
            pagination?: import("../../../../types/cosmos/base/query/v1beta1").PageResponse;
        };
        fromAmino: (object: QueryVotesResponse) => {
            votes: import("../../../../types/cosmos/gov/v1beta1").Vote[];
            pagination?: import("../../../../types/cosmos/base/query/v1beta1").PageResponse;
        };
    };
    "/cosmos.gov.v1beta1.QueryParamsRequest": {
        aminoType: string;
        toAmino: (message: QueryParamsRequest) => {
            params_type: string;
        };
        fromAmino: (object: QueryParamsRequest) => {
            params_type: string;
        };
    };
    "/cosmos.gov.v1beta1.QueryParamsResponse": {
        aminoType: string;
        toAmino: (message: QueryParamsResponse) => {
            voting_params?: import("../../../../types/cosmos/gov/v1beta1").VotingParams;
            deposit_params?: import("../../../../types/cosmos/gov/v1beta1").DepositParams;
            tally_params?: import("../../../../types/cosmos/gov/v1beta1").TallyParams;
        };
        fromAmino: (object: QueryParamsResponse) => {
            voting_params?: import("../../../../types/cosmos/gov/v1beta1").VotingParams;
            deposit_params?: import("../../../../types/cosmos/gov/v1beta1").DepositParams;
            tally_params?: import("../../../../types/cosmos/gov/v1beta1").TallyParams;
        };
    };
    "/cosmos.gov.v1beta1.QueryDepositRequest": {
        aminoType: string;
        toAmino: (message: QueryDepositRequest) => {
            proposal_id: number;
            depositor: string;
        };
        fromAmino: (object: QueryDepositRequest) => {
            proposal_id: number;
            depositor: string;
        };
    };
    "/cosmos.gov.v1beta1.QueryDepositResponse": {
        aminoType: string;
        toAmino: (message: QueryDepositResponse) => {
            deposit?: import("../../../../types/cosmos/gov/v1beta1").Deposit;
        };
        fromAmino: (object: QueryDepositResponse) => {
            deposit?: import("../../../../types/cosmos/gov/v1beta1").Deposit;
        };
    };
    "/cosmos.gov.v1beta1.QueryDepositsRequest": {
        aminoType: string;
        toAmino: (message: QueryDepositsRequest) => {
            proposal_id: number;
            pagination?: import("../../../../types/cosmos/base/query/v1beta1").PageRequest;
        };
        fromAmino: (object: QueryDepositsRequest) => {
            proposal_id: number;
            pagination?: import("../../../../types/cosmos/base/query/v1beta1").PageRequest;
        };
    };
    "/cosmos.gov.v1beta1.QueryDepositsResponse": {
        aminoType: string;
        toAmino: (message: QueryDepositsResponse) => {
            deposits: import("../../../../types/cosmos/gov/v1beta1").Deposit[];
            pagination?: import("../../../../types/cosmos/base/query/v1beta1").PageResponse;
        };
        fromAmino: (object: QueryDepositsResponse) => {
            deposits: import("../../../../types/cosmos/gov/v1beta1").Deposit[];
            pagination?: import("../../../../types/cosmos/base/query/v1beta1").PageResponse;
        };
    };
    "/cosmos.gov.v1beta1.QueryTallyResultRequest": {
        aminoType: string;
        toAmino: (message: QueryTallyResultRequest) => {
            proposal_id: number;
        };
        fromAmino: (object: QueryTallyResultRequest) => {
            proposal_id: number;
        };
    };
    "/cosmos.gov.v1beta1.QueryTallyResultResponse": {
        aminoType: string;
        toAmino: (message: QueryTallyResultResponse) => {
            tally?: import("../../../../types/cosmos/gov/v1beta1").TallyResult;
        };
        fromAmino: (object: QueryTallyResultResponse) => {
            tally?: import("../../../../types/cosmos/gov/v1beta1").TallyResult;
        };
    };
};
