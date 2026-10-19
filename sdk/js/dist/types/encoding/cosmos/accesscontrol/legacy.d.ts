import type { GeneratedType } from "@cosmjs/proto-signing";
import type { LegacyAccessOperationWithSelector as LegacyAccessOperationWithSelector_type, LegacyWasmDependencyMapping as LegacyWasmDependencyMapping_type } from "../../../types/cosmos/accesscontrol";
import type { MessageFns } from "../../common";
export interface LegacyAccessOperationWithSelector extends LegacyAccessOperationWithSelector_type {
}
export interface LegacyWasmDependencyMapping extends LegacyWasmDependencyMapping_type {
}
export declare const LegacyAccessOperationWithSelector: MessageFns<LegacyAccessOperationWithSelector, "cosmos.accesscontrol.v1beta1.LegacyAccessOperationWithSelector">;
export declare const LegacyWasmDependencyMapping: MessageFns<LegacyWasmDependencyMapping, "cosmos.accesscontrol.v1beta1.LegacyWasmDependencyMapping">;
export declare const registry: Array<[string, GeneratedType]>;
export declare const aminoConverters: {
    "/cosmos.accesscontrol.v1beta1.LegacyAccessOperationWithSelector": {
        aminoType: string;
        toAmino: (message: LegacyAccessOperationWithSelector) => {
            operation?: import("../../../types/cosmos/accesscontrol").AccessOperation;
            selector_type: import("../../../types/cosmos/accesscontrol").AccessOperationSelectorType;
            selector: string;
        };
        fromAmino: (object: LegacyAccessOperationWithSelector) => {
            operation?: import("../../../types/cosmos/accesscontrol").AccessOperation;
            selector_type: import("../../../types/cosmos/accesscontrol").AccessOperationSelectorType;
            selector: string;
        };
    };
    "/cosmos.accesscontrol.v1beta1.LegacyWasmDependencyMapping": {
        aminoType: string;
        toAmino: (message: LegacyWasmDependencyMapping) => {
            enabled: boolean;
            access_ops: LegacyAccessOperationWithSelector_type[];
            reset_reason: string;
            contract_address: string;
        };
        fromAmino: (object: LegacyWasmDependencyMapping) => {
            enabled: boolean;
            access_ops: LegacyAccessOperationWithSelector_type[];
            reset_reason: string;
            contract_address: string;
        };
    };
};
