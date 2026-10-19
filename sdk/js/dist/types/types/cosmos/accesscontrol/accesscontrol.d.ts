import type { AccessOperationSelectorType, AccessType, ResourceType, WasmMessageSubtype } from "./constants";
export interface AccessOperation {
    access_type: AccessType;
    resource_type: ResourceType;
    identifier_template: string;
}
export interface WasmAccessOperation {
    operation?: AccessOperation;
    selector_type: AccessOperationSelectorType;
    selector: string;
}
export interface WasmContractReference {
    contract_address: string;
    message_type: WasmMessageSubtype;
    message_name: string;
    json_translation_template: string;
}
export interface WasmContractReferences {
    message_name: string;
    contract_references: WasmContractReference[];
}
export interface WasmAccessOperations {
    message_name: string;
    wasm_operations: WasmAccessOperation[];
}
export interface MessageDependencyMapping {
    message_key: string;
    access_ops: AccessOperation[];
    dynamic_enabled: boolean;
}
export interface WasmDependencyMapping {
    base_access_ops: WasmAccessOperation[];
    query_access_ops: WasmAccessOperations[];
    execute_access_ops: WasmAccessOperations[];
    base_contract_references: WasmContractReference[];
    query_contract_references: WasmContractReferences[];
    execute_contract_references: WasmContractReferences[];
    reset_reason: string;
    contract_address: string;
}
