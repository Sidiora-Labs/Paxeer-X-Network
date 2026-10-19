import type { GeneratedType } from "@cosmjs/proto-signing";
import type { ListResourceDependencyMappingRequest as ListResourceDependencyMappingRequest_type, ListResourceDependencyMappingResponse as ListResourceDependencyMappingResponse_type, ListWasmDependencyMappingRequest as ListWasmDependencyMappingRequest_type, ListWasmDependencyMappingResponse as ListWasmDependencyMappingResponse_type, QueryParamsRequest as QueryParamsRequest_type, QueryParamsResponse as QueryParamsResponse_type, ResourceDependencyMappingFromMessageKeyRequest as ResourceDependencyMappingFromMessageKeyRequest_type, ResourceDependencyMappingFromMessageKeyResponse as ResourceDependencyMappingFromMessageKeyResponse_type, WasmDependencyMappingRequest as WasmDependencyMappingRequest_type, WasmDependencyMappingResponse as WasmDependencyMappingResponse_type } from "../../../types/cosmos/accesscontrol_x";
import type { MessageFns } from "../../common";
export interface QueryParamsRequest extends QueryParamsRequest_type {
}
export interface QueryParamsResponse extends QueryParamsResponse_type {
}
export interface ResourceDependencyMappingFromMessageKeyRequest extends ResourceDependencyMappingFromMessageKeyRequest_type {
}
export interface ResourceDependencyMappingFromMessageKeyResponse extends ResourceDependencyMappingFromMessageKeyResponse_type {
}
export interface WasmDependencyMappingRequest extends WasmDependencyMappingRequest_type {
}
export interface WasmDependencyMappingResponse extends WasmDependencyMappingResponse_type {
}
export interface ListResourceDependencyMappingRequest extends ListResourceDependencyMappingRequest_type {
}
export interface ListResourceDependencyMappingResponse extends ListResourceDependencyMappingResponse_type {
}
export interface ListWasmDependencyMappingRequest extends ListWasmDependencyMappingRequest_type {
}
export interface ListWasmDependencyMappingResponse extends ListWasmDependencyMappingResponse_type {
}
export declare const QueryParamsRequest: MessageFns<QueryParamsRequest, "cosmos.accesscontrol_x.v1beta1.QueryParamsRequest">;
export declare const QueryParamsResponse: MessageFns<QueryParamsResponse, "cosmos.accesscontrol_x.v1beta1.QueryParamsResponse">;
export declare const ResourceDependencyMappingFromMessageKeyRequest: MessageFns<ResourceDependencyMappingFromMessageKeyRequest, "cosmos.accesscontrol_x.v1beta1.ResourceDependencyMappingFromMessageKeyRequest">;
export declare const ResourceDependencyMappingFromMessageKeyResponse: MessageFns<ResourceDependencyMappingFromMessageKeyResponse, "cosmos.accesscontrol_x.v1beta1.ResourceDependencyMappingFromMessageKeyResponse">;
export declare const WasmDependencyMappingRequest: MessageFns<WasmDependencyMappingRequest, "cosmos.accesscontrol_x.v1beta1.WasmDependencyMappingRequest">;
export declare const WasmDependencyMappingResponse: MessageFns<WasmDependencyMappingResponse, "cosmos.accesscontrol_x.v1beta1.WasmDependencyMappingResponse">;
export declare const ListResourceDependencyMappingRequest: MessageFns<ListResourceDependencyMappingRequest, "cosmos.accesscontrol_x.v1beta1.ListResourceDependencyMappingRequest">;
export declare const ListResourceDependencyMappingResponse: MessageFns<ListResourceDependencyMappingResponse, "cosmos.accesscontrol_x.v1beta1.ListResourceDependencyMappingResponse">;
export declare const ListWasmDependencyMappingRequest: MessageFns<ListWasmDependencyMappingRequest, "cosmos.accesscontrol_x.v1beta1.ListWasmDependencyMappingRequest">;
export declare const ListWasmDependencyMappingResponse: MessageFns<ListWasmDependencyMappingResponse, "cosmos.accesscontrol_x.v1beta1.ListWasmDependencyMappingResponse">;
export declare const registry: Array<[string, GeneratedType]>;
export declare const aminoConverters: {
    "/cosmos.accesscontrol_x.v1beta1.QueryParamsRequest": {
        aminoType: string;
        toAmino: (message: QueryParamsRequest) => {};
        fromAmino: (object: QueryParamsRequest) => {};
    };
    "/cosmos.accesscontrol_x.v1beta1.QueryParamsResponse": {
        aminoType: string;
        toAmino: (message: QueryParamsResponse) => {
            params?: import("../../../types/cosmos/accesscontrol_x").Params;
        };
        fromAmino: (object: QueryParamsResponse) => {
            params?: import("../../../types/cosmos/accesscontrol_x").Params;
        };
    };
    "/cosmos.accesscontrol_x.v1beta1.ResourceDependencyMappingFromMessageKeyRequest": {
        aminoType: string;
        toAmino: (message: ResourceDependencyMappingFromMessageKeyRequest) => {
            message_key: string;
        };
        fromAmino: (object: ResourceDependencyMappingFromMessageKeyRequest) => {
            message_key: string;
        };
    };
    "/cosmos.accesscontrol_x.v1beta1.ResourceDependencyMappingFromMessageKeyResponse": {
        aminoType: string;
        toAmino: (message: ResourceDependencyMappingFromMessageKeyResponse) => {
            message_dependency_mapping?: import("../../../types/cosmos/accesscontrol").MessageDependencyMapping;
        };
        fromAmino: (object: ResourceDependencyMappingFromMessageKeyResponse) => {
            message_dependency_mapping?: import("../../../types/cosmos/accesscontrol").MessageDependencyMapping;
        };
    };
    "/cosmos.accesscontrol_x.v1beta1.WasmDependencyMappingRequest": {
        aminoType: string;
        toAmino: (message: WasmDependencyMappingRequest) => {
            contract_address: string;
        };
        fromAmino: (object: WasmDependencyMappingRequest) => {
            contract_address: string;
        };
    };
    "/cosmos.accesscontrol_x.v1beta1.WasmDependencyMappingResponse": {
        aminoType: string;
        toAmino: (message: WasmDependencyMappingResponse) => {
            wasm_dependency_mapping?: import("../../../types/cosmos/accesscontrol").WasmDependencyMapping;
        };
        fromAmino: (object: WasmDependencyMappingResponse) => {
            wasm_dependency_mapping?: import("../../../types/cosmos/accesscontrol").WasmDependencyMapping;
        };
    };
    "/cosmos.accesscontrol_x.v1beta1.ListResourceDependencyMappingRequest": {
        aminoType: string;
        toAmino: (message: ListResourceDependencyMappingRequest) => {};
        fromAmino: (object: ListResourceDependencyMappingRequest) => {};
    };
    "/cosmos.accesscontrol_x.v1beta1.ListResourceDependencyMappingResponse": {
        aminoType: string;
        toAmino: (message: ListResourceDependencyMappingResponse) => {
            message_dependency_mapping_list: import("../../../types/cosmos/accesscontrol").MessageDependencyMapping[];
        };
        fromAmino: (object: ListResourceDependencyMappingResponse) => {
            message_dependency_mapping_list: import("../../../types/cosmos/accesscontrol").MessageDependencyMapping[];
        };
    };
    "/cosmos.accesscontrol_x.v1beta1.ListWasmDependencyMappingRequest": {
        aminoType: string;
        toAmino: (message: ListWasmDependencyMappingRequest) => {};
        fromAmino: (object: ListWasmDependencyMappingRequest) => {};
    };
    "/cosmos.accesscontrol_x.v1beta1.ListWasmDependencyMappingResponse": {
        aminoType: string;
        toAmino: (message: ListWasmDependencyMappingResponse) => {
            wasm_dependency_mapping_list: import("../../../types/cosmos/accesscontrol").WasmDependencyMapping[];
        };
        fromAmino: (object: ListWasmDependencyMappingResponse) => {
            wasm_dependency_mapping_list: import("../../../types/cosmos/accesscontrol").WasmDependencyMapping[];
        };
    };
};
