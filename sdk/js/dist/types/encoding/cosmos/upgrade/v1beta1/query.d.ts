import type { GeneratedType } from "@cosmjs/proto-signing";
import type { QueryAppliedPlanRequest as QueryAppliedPlanRequest_type, QueryAppliedPlanResponse as QueryAppliedPlanResponse_type, QueryCurrentPlanRequest as QueryCurrentPlanRequest_type, QueryCurrentPlanResponse as QueryCurrentPlanResponse_type, QueryModuleVersionsRequest as QueryModuleVersionsRequest_type, QueryModuleVersionsResponse as QueryModuleVersionsResponse_type, QueryUpgradedConsensusStateRequest as QueryUpgradedConsensusStateRequest_type, QueryUpgradedConsensusStateResponse as QueryUpgradedConsensusStateResponse_type } from "../../../../types/cosmos/upgrade/v1beta1";
import type { MessageFns } from "../../../common";
export interface QueryCurrentPlanRequest extends QueryCurrentPlanRequest_type {
}
export interface QueryCurrentPlanResponse extends QueryCurrentPlanResponse_type {
}
export interface QueryAppliedPlanRequest extends QueryAppliedPlanRequest_type {
}
export interface QueryAppliedPlanResponse extends QueryAppliedPlanResponse_type {
}
export interface QueryUpgradedConsensusStateRequest extends QueryUpgradedConsensusStateRequest_type {
}
export interface QueryUpgradedConsensusStateResponse extends QueryUpgradedConsensusStateResponse_type {
}
export interface QueryModuleVersionsRequest extends QueryModuleVersionsRequest_type {
}
export interface QueryModuleVersionsResponse extends QueryModuleVersionsResponse_type {
}
export declare const QueryCurrentPlanRequest: MessageFns<QueryCurrentPlanRequest, "cosmos.upgrade.v1beta1.QueryCurrentPlanRequest">;
export declare const QueryCurrentPlanResponse: MessageFns<QueryCurrentPlanResponse, "cosmos.upgrade.v1beta1.QueryCurrentPlanResponse">;
export declare const QueryAppliedPlanRequest: MessageFns<QueryAppliedPlanRequest, "cosmos.upgrade.v1beta1.QueryAppliedPlanRequest">;
export declare const QueryAppliedPlanResponse: MessageFns<QueryAppliedPlanResponse, "cosmos.upgrade.v1beta1.QueryAppliedPlanResponse">;
export declare const QueryUpgradedConsensusStateRequest: MessageFns<QueryUpgradedConsensusStateRequest, "cosmos.upgrade.v1beta1.QueryUpgradedConsensusStateRequest">;
export declare const QueryUpgradedConsensusStateResponse: MessageFns<QueryUpgradedConsensusStateResponse, "cosmos.upgrade.v1beta1.QueryUpgradedConsensusStateResponse">;
export declare const QueryModuleVersionsRequest: MessageFns<QueryModuleVersionsRequest, "cosmos.upgrade.v1beta1.QueryModuleVersionsRequest">;
export declare const QueryModuleVersionsResponse: MessageFns<QueryModuleVersionsResponse, "cosmos.upgrade.v1beta1.QueryModuleVersionsResponse">;
export declare const registry: Array<[string, GeneratedType]>;
export declare const aminoConverters: {
    "/cosmos.upgrade.v1beta1.QueryCurrentPlanRequest": {
        aminoType: string;
        toAmino: (message: QueryCurrentPlanRequest) => {};
        fromAmino: (object: QueryCurrentPlanRequest) => {};
    };
    "/cosmos.upgrade.v1beta1.QueryCurrentPlanResponse": {
        aminoType: string;
        toAmino: (message: QueryCurrentPlanResponse) => {
            plan?: import("../../../../types/cosmos/upgrade/v1beta1").Plan;
        };
        fromAmino: (object: QueryCurrentPlanResponse) => {
            plan?: import("../../../../types/cosmos/upgrade/v1beta1").Plan;
        };
    };
    "/cosmos.upgrade.v1beta1.QueryAppliedPlanRequest": {
        aminoType: string;
        toAmino: (message: QueryAppliedPlanRequest) => {
            name: string;
        };
        fromAmino: (object: QueryAppliedPlanRequest) => {
            name: string;
        };
    };
    "/cosmos.upgrade.v1beta1.QueryAppliedPlanResponse": {
        aminoType: string;
        toAmino: (message: QueryAppliedPlanResponse) => {
            height: number;
        };
        fromAmino: (object: QueryAppliedPlanResponse) => {
            height: number;
        };
    };
    "/cosmos.upgrade.v1beta1.QueryUpgradedConsensusStateRequest": {
        aminoType: string;
        toAmino: (message: QueryUpgradedConsensusStateRequest) => {
            last_height: number;
        };
        fromAmino: (object: QueryUpgradedConsensusStateRequest) => {
            last_height: number;
        };
    };
    "/cosmos.upgrade.v1beta1.QueryUpgradedConsensusStateResponse": {
        aminoType: string;
        toAmino: (message: QueryUpgradedConsensusStateResponse) => {
            upgraded_consensus_state: Uint8Array;
        };
        fromAmino: (object: QueryUpgradedConsensusStateResponse) => {
            upgraded_consensus_state: Uint8Array;
        };
    };
    "/cosmos.upgrade.v1beta1.QueryModuleVersionsRequest": {
        aminoType: string;
        toAmino: (message: QueryModuleVersionsRequest) => {
            module_name: string;
        };
        fromAmino: (object: QueryModuleVersionsRequest) => {
            module_name: string;
        };
    };
    "/cosmos.upgrade.v1beta1.QueryModuleVersionsResponse": {
        aminoType: string;
        toAmino: (message: QueryModuleVersionsResponse) => {
            module_versions: import("../../../../types/cosmos/upgrade/v1beta1").ModuleVersion[];
        };
        fromAmino: (object: QueryModuleVersionsResponse) => {
            module_versions: import("../../../../types/cosmos/upgrade/v1beta1").ModuleVersion[];
        };
    };
};
