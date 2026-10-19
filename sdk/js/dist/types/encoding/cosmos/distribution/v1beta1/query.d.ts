import type { GeneratedType } from "@cosmjs/proto-signing";
import type { QueryCommunityPoolRequest as QueryCommunityPoolRequest_type, QueryCommunityPoolResponse as QueryCommunityPoolResponse_type, QueryDelegationRewardsRequest as QueryDelegationRewardsRequest_type, QueryDelegationRewardsResponse as QueryDelegationRewardsResponse_type, QueryDelegationTotalRewardsRequest as QueryDelegationTotalRewardsRequest_type, QueryDelegationTotalRewardsResponse as QueryDelegationTotalRewardsResponse_type, QueryDelegatorValidatorsRequest as QueryDelegatorValidatorsRequest_type, QueryDelegatorValidatorsResponse as QueryDelegatorValidatorsResponse_type, QueryDelegatorWithdrawAddressRequest as QueryDelegatorWithdrawAddressRequest_type, QueryDelegatorWithdrawAddressResponse as QueryDelegatorWithdrawAddressResponse_type, QueryParamsRequest as QueryParamsRequest_type, QueryParamsResponse as QueryParamsResponse_type, QueryValidatorCommissionRequest as QueryValidatorCommissionRequest_type, QueryValidatorCommissionResponse as QueryValidatorCommissionResponse_type, QueryValidatorOutstandingRewardsRequest as QueryValidatorOutstandingRewardsRequest_type, QueryValidatorOutstandingRewardsResponse as QueryValidatorOutstandingRewardsResponse_type, QueryValidatorSlashesRequest as QueryValidatorSlashesRequest_type, QueryValidatorSlashesResponse as QueryValidatorSlashesResponse_type } from "../../../../types/cosmos/distribution/v1beta1";
import type { MessageFns } from "../../../common";
export interface QueryParamsRequest extends QueryParamsRequest_type {
}
export interface QueryParamsResponse extends QueryParamsResponse_type {
}
export interface QueryValidatorOutstandingRewardsRequest extends QueryValidatorOutstandingRewardsRequest_type {
}
export interface QueryValidatorOutstandingRewardsResponse extends QueryValidatorOutstandingRewardsResponse_type {
}
export interface QueryValidatorCommissionRequest extends QueryValidatorCommissionRequest_type {
}
export interface QueryValidatorCommissionResponse extends QueryValidatorCommissionResponse_type {
}
export interface QueryValidatorSlashesRequest extends QueryValidatorSlashesRequest_type {
}
export interface QueryValidatorSlashesResponse extends QueryValidatorSlashesResponse_type {
}
export interface QueryDelegationRewardsRequest extends QueryDelegationRewardsRequest_type {
}
export interface QueryDelegationRewardsResponse extends QueryDelegationRewardsResponse_type {
}
export interface QueryDelegationTotalRewardsRequest extends QueryDelegationTotalRewardsRequest_type {
}
export interface QueryDelegationTotalRewardsResponse extends QueryDelegationTotalRewardsResponse_type {
}
export interface QueryDelegatorValidatorsRequest extends QueryDelegatorValidatorsRequest_type {
}
export interface QueryDelegatorValidatorsResponse extends QueryDelegatorValidatorsResponse_type {
}
export interface QueryDelegatorWithdrawAddressRequest extends QueryDelegatorWithdrawAddressRequest_type {
}
export interface QueryDelegatorWithdrawAddressResponse extends QueryDelegatorWithdrawAddressResponse_type {
}
export interface QueryCommunityPoolRequest extends QueryCommunityPoolRequest_type {
}
export interface QueryCommunityPoolResponse extends QueryCommunityPoolResponse_type {
}
export declare const QueryParamsRequest: MessageFns<QueryParamsRequest, "cosmos.distribution.v1beta1.QueryParamsRequest">;
export declare const QueryParamsResponse: MessageFns<QueryParamsResponse, "cosmos.distribution.v1beta1.QueryParamsResponse">;
export declare const QueryValidatorOutstandingRewardsRequest: MessageFns<QueryValidatorOutstandingRewardsRequest, "cosmos.distribution.v1beta1.QueryValidatorOutstandingRewardsRequest">;
export declare const QueryValidatorOutstandingRewardsResponse: MessageFns<QueryValidatorOutstandingRewardsResponse, "cosmos.distribution.v1beta1.QueryValidatorOutstandingRewardsResponse">;
export declare const QueryValidatorCommissionRequest: MessageFns<QueryValidatorCommissionRequest, "cosmos.distribution.v1beta1.QueryValidatorCommissionRequest">;
export declare const QueryValidatorCommissionResponse: MessageFns<QueryValidatorCommissionResponse, "cosmos.distribution.v1beta1.QueryValidatorCommissionResponse">;
export declare const QueryValidatorSlashesRequest: MessageFns<QueryValidatorSlashesRequest, "cosmos.distribution.v1beta1.QueryValidatorSlashesRequest">;
export declare const QueryValidatorSlashesResponse: MessageFns<QueryValidatorSlashesResponse, "cosmos.distribution.v1beta1.QueryValidatorSlashesResponse">;
export declare const QueryDelegationRewardsRequest: MessageFns<QueryDelegationRewardsRequest, "cosmos.distribution.v1beta1.QueryDelegationRewardsRequest">;
export declare const QueryDelegationRewardsResponse: MessageFns<QueryDelegationRewardsResponse, "cosmos.distribution.v1beta1.QueryDelegationRewardsResponse">;
export declare const QueryDelegationTotalRewardsRequest: MessageFns<QueryDelegationTotalRewardsRequest, "cosmos.distribution.v1beta1.QueryDelegationTotalRewardsRequest">;
export declare const QueryDelegationTotalRewardsResponse: MessageFns<QueryDelegationTotalRewardsResponse, "cosmos.distribution.v1beta1.QueryDelegationTotalRewardsResponse">;
export declare const QueryDelegatorValidatorsRequest: MessageFns<QueryDelegatorValidatorsRequest, "cosmos.distribution.v1beta1.QueryDelegatorValidatorsRequest">;
export declare const QueryDelegatorValidatorsResponse: MessageFns<QueryDelegatorValidatorsResponse, "cosmos.distribution.v1beta1.QueryDelegatorValidatorsResponse">;
export declare const QueryDelegatorWithdrawAddressRequest: MessageFns<QueryDelegatorWithdrawAddressRequest, "cosmos.distribution.v1beta1.QueryDelegatorWithdrawAddressRequest">;
export declare const QueryDelegatorWithdrawAddressResponse: MessageFns<QueryDelegatorWithdrawAddressResponse, "cosmos.distribution.v1beta1.QueryDelegatorWithdrawAddressResponse">;
export declare const QueryCommunityPoolRequest: MessageFns<QueryCommunityPoolRequest, "cosmos.distribution.v1beta1.QueryCommunityPoolRequest">;
export declare const QueryCommunityPoolResponse: MessageFns<QueryCommunityPoolResponse, "cosmos.distribution.v1beta1.QueryCommunityPoolResponse">;
export declare const registry: Array<[string, GeneratedType]>;
export declare const aminoConverters: {
    "/cosmos.distribution.v1beta1.QueryParamsRequest": {
        aminoType: string;
        toAmino: (message: QueryParamsRequest) => {};
        fromAmino: (object: QueryParamsRequest) => {};
    };
    "/cosmos.distribution.v1beta1.QueryParamsResponse": {
        aminoType: string;
        toAmino: (message: QueryParamsResponse) => {
            params?: import("../../../../types/cosmos/distribution/v1beta1").Params;
        };
        fromAmino: (object: QueryParamsResponse) => {
            params?: import("../../../../types/cosmos/distribution/v1beta1").Params;
        };
    };
    "/cosmos.distribution.v1beta1.QueryValidatorOutstandingRewardsRequest": {
        aminoType: string;
        toAmino: (message: QueryValidatorOutstandingRewardsRequest) => {
            validator_address: string;
        };
        fromAmino: (object: QueryValidatorOutstandingRewardsRequest) => {
            validator_address: string;
        };
    };
    "/cosmos.distribution.v1beta1.QueryValidatorOutstandingRewardsResponse": {
        aminoType: string;
        toAmino: (message: QueryValidatorOutstandingRewardsResponse) => {
            rewards?: import("../../../../types/cosmos/distribution/v1beta1").ValidatorOutstandingRewards;
        };
        fromAmino: (object: QueryValidatorOutstandingRewardsResponse) => {
            rewards?: import("../../../../types/cosmos/distribution/v1beta1").ValidatorOutstandingRewards;
        };
    };
    "/cosmos.distribution.v1beta1.QueryValidatorCommissionRequest": {
        aminoType: string;
        toAmino: (message: QueryValidatorCommissionRequest) => {
            validator_address: string;
        };
        fromAmino: (object: QueryValidatorCommissionRequest) => {
            validator_address: string;
        };
    };
    "/cosmos.distribution.v1beta1.QueryValidatorCommissionResponse": {
        aminoType: string;
        toAmino: (message: QueryValidatorCommissionResponse) => {
            commission?: import("../../../../types/cosmos/distribution/v1beta1").ValidatorAccumulatedCommission;
        };
        fromAmino: (object: QueryValidatorCommissionResponse) => {
            commission?: import("../../../../types/cosmos/distribution/v1beta1").ValidatorAccumulatedCommission;
        };
    };
    "/cosmos.distribution.v1beta1.QueryValidatorSlashesRequest": {
        aminoType: string;
        toAmino: (message: QueryValidatorSlashesRequest) => {
            validator_address: string;
            starting_height: number;
            ending_height: number;
            pagination?: import("../../../../types/cosmos/base/query/v1beta1").PageRequest;
        };
        fromAmino: (object: QueryValidatorSlashesRequest) => {
            validator_address: string;
            starting_height: number;
            ending_height: number;
            pagination?: import("../../../../types/cosmos/base/query/v1beta1").PageRequest;
        };
    };
    "/cosmos.distribution.v1beta1.QueryValidatorSlashesResponse": {
        aminoType: string;
        toAmino: (message: QueryValidatorSlashesResponse) => {
            slashes: import("../../../../types/cosmos/distribution/v1beta1").ValidatorSlashEvent[];
            pagination?: import("../../../../types/cosmos/base/query/v1beta1").PageResponse;
        };
        fromAmino: (object: QueryValidatorSlashesResponse) => {
            slashes: import("../../../../types/cosmos/distribution/v1beta1").ValidatorSlashEvent[];
            pagination?: import("../../../../types/cosmos/base/query/v1beta1").PageResponse;
        };
    };
    "/cosmos.distribution.v1beta1.QueryDelegationRewardsRequest": {
        aminoType: string;
        toAmino: (message: QueryDelegationRewardsRequest) => {
            delegator_address: string;
            validator_address: string;
        };
        fromAmino: (object: QueryDelegationRewardsRequest) => {
            delegator_address: string;
            validator_address: string;
        };
    };
    "/cosmos.distribution.v1beta1.QueryDelegationRewardsResponse": {
        aminoType: string;
        toAmino: (message: QueryDelegationRewardsResponse) => {
            rewards: import("../../../../types/cosmos/base/v1beta1").DecCoin[];
        };
        fromAmino: (object: QueryDelegationRewardsResponse) => {
            rewards: import("../../../../types/cosmos/base/v1beta1").DecCoin[];
        };
    };
    "/cosmos.distribution.v1beta1.QueryDelegationTotalRewardsRequest": {
        aminoType: string;
        toAmino: (message: QueryDelegationTotalRewardsRequest) => {
            delegator_address: string;
        };
        fromAmino: (object: QueryDelegationTotalRewardsRequest) => {
            delegator_address: string;
        };
    };
    "/cosmos.distribution.v1beta1.QueryDelegationTotalRewardsResponse": {
        aminoType: string;
        toAmino: (message: QueryDelegationTotalRewardsResponse) => {
            rewards: import("../../../../types/cosmos/distribution/v1beta1").DelegationDelegatorReward[];
            total: import("../../../../types/cosmos/base/v1beta1").DecCoin[];
        };
        fromAmino: (object: QueryDelegationTotalRewardsResponse) => {
            rewards: import("../../../../types/cosmos/distribution/v1beta1").DelegationDelegatorReward[];
            total: import("../../../../types/cosmos/base/v1beta1").DecCoin[];
        };
    };
    "/cosmos.distribution.v1beta1.QueryDelegatorValidatorsRequest": {
        aminoType: string;
        toAmino: (message: QueryDelegatorValidatorsRequest) => {
            delegator_address: string;
        };
        fromAmino: (object: QueryDelegatorValidatorsRequest) => {
            delegator_address: string;
        };
    };
    "/cosmos.distribution.v1beta1.QueryDelegatorValidatorsResponse": {
        aminoType: string;
        toAmino: (message: QueryDelegatorValidatorsResponse) => {
            validators: string[];
        };
        fromAmino: (object: QueryDelegatorValidatorsResponse) => {
            validators: string[];
        };
    };
    "/cosmos.distribution.v1beta1.QueryDelegatorWithdrawAddressRequest": {
        aminoType: string;
        toAmino: (message: QueryDelegatorWithdrawAddressRequest) => {
            delegator_address: string;
        };
        fromAmino: (object: QueryDelegatorWithdrawAddressRequest) => {
            delegator_address: string;
        };
    };
    "/cosmos.distribution.v1beta1.QueryDelegatorWithdrawAddressResponse": {
        aminoType: string;
        toAmino: (message: QueryDelegatorWithdrawAddressResponse) => {
            withdraw_address: string;
        };
        fromAmino: (object: QueryDelegatorWithdrawAddressResponse) => {
            withdraw_address: string;
        };
    };
    "/cosmos.distribution.v1beta1.QueryCommunityPoolRequest": {
        aminoType: string;
        toAmino: (message: QueryCommunityPoolRequest) => {};
        fromAmino: (object: QueryCommunityPoolRequest) => {};
    };
    "/cosmos.distribution.v1beta1.QueryCommunityPoolResponse": {
        aminoType: string;
        toAmino: (message: QueryCommunityPoolResponse) => {
            pool: import("../../../../types/cosmos/base/v1beta1").DecCoin[];
        };
        fromAmino: (object: QueryCommunityPoolResponse) => {
            pool: import("../../../../types/cosmos/base/v1beta1").DecCoin[];
        };
    };
};
