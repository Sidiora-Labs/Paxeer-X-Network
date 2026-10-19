import type { GeneratedType } from "@cosmjs/proto-signing";
import type { GetBlockByHeightRequest as GetBlockByHeightRequest_type, GetBlockByHeightResponse as GetBlockByHeightResponse_type, GetLatestBlockRequest as GetLatestBlockRequest_type, GetLatestBlockResponse as GetLatestBlockResponse_type, GetLatestValidatorSetRequest as GetLatestValidatorSetRequest_type, GetLatestValidatorSetResponse as GetLatestValidatorSetResponse_type, GetNodeInfoRequest as GetNodeInfoRequest_type, GetNodeInfoResponse as GetNodeInfoResponse_type, GetSyncingRequest as GetSyncingRequest_type, GetSyncingResponse as GetSyncingResponse_type, GetValidatorSetByHeightRequest as GetValidatorSetByHeightRequest_type, GetValidatorSetByHeightResponse as GetValidatorSetByHeightResponse_type, Module as Module_type, Validator as Validator_type, VersionInfo as VersionInfo_type } from "../../../../../types/cosmos/base/tendermint/v1beta1";
import type { MessageFns } from "../../../../common";
export interface GetValidatorSetByHeightRequest extends GetValidatorSetByHeightRequest_type {
}
export interface GetValidatorSetByHeightResponse extends GetValidatorSetByHeightResponse_type {
}
export interface GetLatestValidatorSetRequest extends GetLatestValidatorSetRequest_type {
}
export interface GetLatestValidatorSetResponse extends GetLatestValidatorSetResponse_type {
}
export interface Validator extends Validator_type {
}
export interface GetBlockByHeightRequest extends GetBlockByHeightRequest_type {
}
export interface GetBlockByHeightResponse extends GetBlockByHeightResponse_type {
}
export interface GetLatestBlockRequest extends GetLatestBlockRequest_type {
}
export interface GetLatestBlockResponse extends GetLatestBlockResponse_type {
}
export interface GetSyncingRequest extends GetSyncingRequest_type {
}
export interface GetSyncingResponse extends GetSyncingResponse_type {
}
export interface GetNodeInfoRequest extends GetNodeInfoRequest_type {
}
export interface GetNodeInfoResponse extends GetNodeInfoResponse_type {
}
export interface VersionInfo extends VersionInfo_type {
}
export interface Module extends Module_type {
}
export declare const GetValidatorSetByHeightRequest: MessageFns<GetValidatorSetByHeightRequest, "cosmos.base.tendermint.v1beta1.GetValidatorSetByHeightRequest">;
export declare const GetValidatorSetByHeightResponse: MessageFns<GetValidatorSetByHeightResponse, "cosmos.base.tendermint.v1beta1.GetValidatorSetByHeightResponse">;
export declare const GetLatestValidatorSetRequest: MessageFns<GetLatestValidatorSetRequest, "cosmos.base.tendermint.v1beta1.GetLatestValidatorSetRequest">;
export declare const GetLatestValidatorSetResponse: MessageFns<GetLatestValidatorSetResponse, "cosmos.base.tendermint.v1beta1.GetLatestValidatorSetResponse">;
export declare const Validator: MessageFns<Validator, "cosmos.base.tendermint.v1beta1.Validator">;
export declare const GetBlockByHeightRequest: MessageFns<GetBlockByHeightRequest, "cosmos.base.tendermint.v1beta1.GetBlockByHeightRequest">;
export declare const GetBlockByHeightResponse: MessageFns<GetBlockByHeightResponse, "cosmos.base.tendermint.v1beta1.GetBlockByHeightResponse">;
export declare const GetLatestBlockRequest: MessageFns<GetLatestBlockRequest, "cosmos.base.tendermint.v1beta1.GetLatestBlockRequest">;
export declare const GetLatestBlockResponse: MessageFns<GetLatestBlockResponse, "cosmos.base.tendermint.v1beta1.GetLatestBlockResponse">;
export declare const GetSyncingRequest: MessageFns<GetSyncingRequest, "cosmos.base.tendermint.v1beta1.GetSyncingRequest">;
export declare const GetSyncingResponse: MessageFns<GetSyncingResponse, "cosmos.base.tendermint.v1beta1.GetSyncingResponse">;
export declare const GetNodeInfoRequest: MessageFns<GetNodeInfoRequest, "cosmos.base.tendermint.v1beta1.GetNodeInfoRequest">;
export declare const GetNodeInfoResponse: MessageFns<GetNodeInfoResponse, "cosmos.base.tendermint.v1beta1.GetNodeInfoResponse">;
export declare const VersionInfo: MessageFns<VersionInfo, "cosmos.base.tendermint.v1beta1.VersionInfo">;
export declare const Module: MessageFns<Module, "cosmos.base.tendermint.v1beta1.Module">;
export declare const registry: Array<[string, GeneratedType]>;
export declare const aminoConverters: {
    "/cosmos.base.tendermint.v1beta1.GetValidatorSetByHeightRequest": {
        aminoType: string;
        toAmino: (message: GetValidatorSetByHeightRequest) => {
            height: number;
            pagination?: import("../../../../../types/cosmos/base/query/v1beta1").PageRequest;
        };
        fromAmino: (object: GetValidatorSetByHeightRequest) => {
            height: number;
            pagination?: import("../../../../../types/cosmos/base/query/v1beta1").PageRequest;
        };
    };
    "/cosmos.base.tendermint.v1beta1.GetValidatorSetByHeightResponse": {
        aminoType: string;
        toAmino: (message: GetValidatorSetByHeightResponse) => {
            block_height: number;
            validators: Validator_type[];
            pagination?: import("../../../../../types/cosmos/base/query/v1beta1").PageResponse;
        };
        fromAmino: (object: GetValidatorSetByHeightResponse) => {
            block_height: number;
            validators: Validator_type[];
            pagination?: import("../../../../../types/cosmos/base/query/v1beta1").PageResponse;
        };
    };
    "/cosmos.base.tendermint.v1beta1.GetLatestValidatorSetRequest": {
        aminoType: string;
        toAmino: (message: GetLatestValidatorSetRequest) => {
            pagination?: import("../../../../../types/cosmos/base/query/v1beta1").PageRequest;
        };
        fromAmino: (object: GetLatestValidatorSetRequest) => {
            pagination?: import("../../../../../types/cosmos/base/query/v1beta1").PageRequest;
        };
    };
    "/cosmos.base.tendermint.v1beta1.GetLatestValidatorSetResponse": {
        aminoType: string;
        toAmino: (message: GetLatestValidatorSetResponse) => {
            block_height: number;
            validators: Validator_type[];
            pagination?: import("../../../../../types/cosmos/base/query/v1beta1").PageResponse;
        };
        fromAmino: (object: GetLatestValidatorSetResponse) => {
            block_height: number;
            validators: Validator_type[];
            pagination?: import("../../../../../types/cosmos/base/query/v1beta1").PageResponse;
        };
    };
    "/cosmos.base.tendermint.v1beta1.Validator": {
        aminoType: string;
        toAmino: (message: Validator) => {
            address: string;
            pub_key?: import("../../../../../types/google/protobuf").Any;
            voting_power: number;
            proposer_priority: number;
        };
        fromAmino: (object: Validator) => {
            address: string;
            pub_key?: import("../../../../../types/google/protobuf").Any;
            voting_power: number;
            proposer_priority: number;
        };
    };
    "/cosmos.base.tendermint.v1beta1.GetBlockByHeightRequest": {
        aminoType: string;
        toAmino: (message: GetBlockByHeightRequest) => {
            height: number;
        };
        fromAmino: (object: GetBlockByHeightRequest) => {
            height: number;
        };
    };
    "/cosmos.base.tendermint.v1beta1.GetBlockByHeightResponse": {
        aminoType: string;
        toAmino: (message: GetBlockByHeightResponse) => {
            block_id?: import("../../../../../types/tendermint/types").BlockID;
            block?: import("../../../../../types/tendermint/types").Block;
        };
        fromAmino: (object: GetBlockByHeightResponse) => {
            block_id?: import("../../../../../types/tendermint/types").BlockID;
            block?: import("../../../../../types/tendermint/types").Block;
        };
    };
    "/cosmos.base.tendermint.v1beta1.GetLatestBlockRequest": {
        aminoType: string;
        toAmino: (message: GetLatestBlockRequest) => {};
        fromAmino: (object: GetLatestBlockRequest) => {};
    };
    "/cosmos.base.tendermint.v1beta1.GetLatestBlockResponse": {
        aminoType: string;
        toAmino: (message: GetLatestBlockResponse) => {
            block_id?: import("../../../../../types/tendermint/types").BlockID;
            block?: import("../../../../../types/tendermint/types").Block;
        };
        fromAmino: (object: GetLatestBlockResponse) => {
            block_id?: import("../../../../../types/tendermint/types").BlockID;
            block?: import("../../../../../types/tendermint/types").Block;
        };
    };
    "/cosmos.base.tendermint.v1beta1.GetSyncingRequest": {
        aminoType: string;
        toAmino: (message: GetSyncingRequest) => {};
        fromAmino: (object: GetSyncingRequest) => {};
    };
    "/cosmos.base.tendermint.v1beta1.GetSyncingResponse": {
        aminoType: string;
        toAmino: (message: GetSyncingResponse) => {
            syncing: boolean;
        };
        fromAmino: (object: GetSyncingResponse) => {
            syncing: boolean;
        };
    };
    "/cosmos.base.tendermint.v1beta1.GetNodeInfoRequest": {
        aminoType: string;
        toAmino: (message: GetNodeInfoRequest) => {};
        fromAmino: (object: GetNodeInfoRequest) => {};
    };
    "/cosmos.base.tendermint.v1beta1.GetNodeInfoResponse": {
        aminoType: string;
        toAmino: (message: GetNodeInfoResponse) => {
            default_node_info?: import("../../../../../types/tendermint/p2p").NodeInfo;
            application_version?: VersionInfo_type;
        };
        fromAmino: (object: GetNodeInfoResponse) => {
            default_node_info?: import("../../../../../types/tendermint/p2p").NodeInfo;
            application_version?: VersionInfo_type;
        };
    };
    "/cosmos.base.tendermint.v1beta1.VersionInfo": {
        aminoType: string;
        toAmino: (message: VersionInfo) => {
            name: string;
            app_name: string;
            version: string;
            git_commit: string;
            build_tags: string;
            go_version: string;
            build_deps: Module_type[];
            cosmos_sdk_version: string;
        };
        fromAmino: (object: VersionInfo) => {
            name: string;
            app_name: string;
            version: string;
            git_commit: string;
            build_tags: string;
            go_version: string;
            build_deps: Module_type[];
            cosmos_sdk_version: string;
        };
    };
    "/cosmos.base.tendermint.v1beta1.Module": {
        aminoType: string;
        toAmino: (message: Module) => {
            path: string;
            version: string;
            sum: string;
        };
        fromAmino: (object: Module) => {
            path: string;
            version: string;
            sum: string;
        };
    };
};
