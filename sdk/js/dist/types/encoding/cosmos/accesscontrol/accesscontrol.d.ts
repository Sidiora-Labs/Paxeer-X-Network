import type { GeneratedType } from "@cosmjs/proto-signing";
import type { AccessOperation as AccessOperation_type, MessageDependencyMapping as MessageDependencyMapping_type, WasmAccessOperation as WasmAccessOperation_type, WasmAccessOperations as WasmAccessOperations_type, WasmContractReference as WasmContractReference_type, WasmContractReferences as WasmContractReferences_type, WasmDependencyMapping as WasmDependencyMapping_type } from "../../../types/cosmos/accesscontrol";
import type { MessageFns } from "../../common";
export interface AccessOperation extends AccessOperation_type {
}
export interface WasmAccessOperation extends WasmAccessOperation_type {
}
export interface WasmContractReference extends WasmContractReference_type {
}
export interface WasmContractReferences extends WasmContractReferences_type {
}
export interface WasmAccessOperations extends WasmAccessOperations_type {
}
export interface MessageDependencyMapping extends MessageDependencyMapping_type {
}
export interface WasmDependencyMapping extends WasmDependencyMapping_type {
}
export declare const AccessOperation: MessageFns<AccessOperation, "cosmos.accesscontrol.v1beta1.AccessOperation">;
export declare const WasmAccessOperation: MessageFns<WasmAccessOperation, "cosmos.accesscontrol.v1beta1.WasmAccessOperation">;
export declare const WasmContractReference: MessageFns<WasmContractReference, "cosmos.accesscontrol.v1beta1.WasmContractReference">;
export declare const WasmContractReferences: MessageFns<WasmContractReferences, "cosmos.accesscontrol.v1beta1.WasmContractReferences">;
export declare const WasmAccessOperations: MessageFns<WasmAccessOperations, "cosmos.accesscontrol.v1beta1.WasmAccessOperations">;
export declare const MessageDependencyMapping: MessageFns<MessageDependencyMapping, "cosmos.accesscontrol.v1beta1.MessageDependencyMapping">;
export declare const WasmDependencyMapping: MessageFns<WasmDependencyMapping, "cosmos.accesscontrol.v1beta1.WasmDependencyMapping">;
export declare const registry: Array<[string, GeneratedType]>;
export declare const aminoConverters: {
    "/cosmos.accesscontrol.v1beta1.AccessOperation": {
        aminoType: string;
        toAmino: (message: AccessOperation) => {
            access_type: import("../../../types/cosmos/accesscontrol").AccessType;
            resource_type: import("../../../types/cosmos/accesscontrol").ResourceType;
            identifier_template: string;
        };
        fromAmino: (object: AccessOperation) => {
            access_type: import("../../../types/cosmos/accesscontrol").AccessType;
            resource_type: import("../../../types/cosmos/accesscontrol").ResourceType;
            identifier_template: string;
        };
    };
    "/cosmos.accesscontrol.v1beta1.WasmAccessOperation": {
        aminoType: string;
        toAmino: (message: WasmAccessOperation) => {
            operation?: AccessOperation_type;
            selector_type: import("../../../types/cosmos/accesscontrol").AccessOperationSelectorType;
            selector: string;
        };
        fromAmino: (object: WasmAccessOperation) => {
            operation?: AccessOperation_type;
            selector_type: import("../../../types/cosmos/accesscontrol").AccessOperationSelectorType;
            selector: string;
        };
    };
    "/cosmos.accesscontrol.v1beta1.WasmContractReference": {
        aminoType: string;
        toAmino: (message: WasmContractReference) => {
            contract_address: string;
            message_type: import("../../../types/cosmos/accesscontrol").WasmMessageSubtype;
            message_name: string;
            json_translation_template: string;
        };
        fromAmino: (object: WasmContractReference) => {
            contract_address: string;
            message_type: import("../../../types/cosmos/accesscontrol").WasmMessageSubtype;
            message_name: string;
            json_translation_template: string;
        };
    };
    "/cosmos.accesscontrol.v1beta1.WasmContractReferences": {
        aminoType: string;
        toAmino: (message: WasmContractReferences) => {
            message_name: string;
            contract_references: WasmContractReference_type[];
        };
        fromAmino: (object: WasmContractReferences) => {
            message_name: string;
            contract_references: WasmContractReference_type[];
        };
    };
    "/cosmos.accesscontrol.v1beta1.WasmAccessOperations": {
        aminoType: string;
        toAmino: (message: WasmAccessOperations) => {
            message_name: string;
            wasm_operations: WasmAccessOperation_type[];
        };
        fromAmino: (object: WasmAccessOperations) => {
            message_name: string;
            wasm_operations: WasmAccessOperation_type[];
        };
    };
    "/cosmos.accesscontrol.v1beta1.MessageDependencyMapping": {
        aminoType: string;
        toAmino: (message: MessageDependencyMapping) => {
            message_key: string;
            access_ops: AccessOperation_type[];
            dynamic_enabled: boolean;
        };
        fromAmino: (object: MessageDependencyMapping) => {
            message_key: string;
            access_ops: AccessOperation_type[];
            dynamic_enabled: boolean;
        };
    };
    "/cosmos.accesscontrol.v1beta1.WasmDependencyMapping": {
        aminoType: string;
        toAmino: (message: WasmDependencyMapping) => {
            base_access_ops: WasmAccessOperation_type[];
            query_access_ops: WasmAccessOperations_type[];
            execute_access_ops: WasmAccessOperations_type[];
            base_contract_references: WasmContractReference_type[];
            query_contract_references: WasmContractReferences_type[];
            execute_contract_references: WasmContractReferences_type[];
            reset_reason: string;
            contract_address: string;
        };
        fromAmino: (object: WasmDependencyMapping) => {
            base_access_ops: WasmAccessOperation_type[];
            query_access_ops: WasmAccessOperations_type[];
            execute_access_ops: WasmAccessOperations_type[];
            base_contract_references: WasmContractReference_type[];
            query_contract_references: WasmContractReferences_type[];
            execute_contract_references: WasmContractReferences_type[];
            reset_reason: string;
            contract_address: string;
        };
    };
};
