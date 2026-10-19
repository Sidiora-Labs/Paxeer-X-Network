import type { MessageDependencyMapping, WasmDependencyMapping } from "../accesscontrol/accesscontrol";
import type { Params } from "./genesis";
export type QueryParamsRequest = {};
export interface QueryParamsResponse {
    /** params defines the parameters of the module. */
    params?: Params;
}
export interface ResourceDependencyMappingFromMessageKeyRequest {
    message_key: string;
}
export interface ResourceDependencyMappingFromMessageKeyResponse {
    message_dependency_mapping?: MessageDependencyMapping;
}
export interface WasmDependencyMappingRequest {
    contract_address: string;
}
export interface WasmDependencyMappingResponse {
    wasm_dependency_mapping?: WasmDependencyMapping;
}
export type ListResourceDependencyMappingRequest = {};
export interface ListResourceDependencyMappingResponse {
    message_dependency_mapping_list: MessageDependencyMapping[];
}
export type ListWasmDependencyMappingRequest = {};
export interface ListWasmDependencyMappingResponse {
    wasm_dependency_mapping_list: WasmDependencyMapping[];
}
