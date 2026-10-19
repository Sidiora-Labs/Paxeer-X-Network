import type { GeneratedType } from "@cosmjs/proto-signing";
import type { GenesisState as GenesisState_type, Params as Params_type } from "../../../types/cosmos/accesscontrol_x";
import type { MessageFns } from "../../common";
export interface GenesisState extends GenesisState_type {
}
export interface Params extends Params_type {
}
export declare const GenesisState: MessageFns<GenesisState, "cosmos.accesscontrol_x.v1beta1.GenesisState">;
export declare const Params: MessageFns<Params, "cosmos.accesscontrol_x.v1beta1.Params">;
export declare const registry: Array<[string, GeneratedType]>;
export declare const aminoConverters: {
    "/cosmos.accesscontrol_x.v1beta1.GenesisState": {
        aminoType: string;
        toAmino: (message: GenesisState) => {
            params?: Params_type;
            message_dependency_mapping: import("../../../types/cosmos/accesscontrol").MessageDependencyMapping[];
            wasm_dependency_mappings: import("../../../types/cosmos/accesscontrol").WasmDependencyMapping[];
        };
        fromAmino: (object: GenesisState) => {
            params?: Params_type;
            message_dependency_mapping: import("../../../types/cosmos/accesscontrol").MessageDependencyMapping[];
            wasm_dependency_mappings: import("../../../types/cosmos/accesscontrol").WasmDependencyMapping[];
        };
    };
    "/cosmos.accesscontrol_x.v1beta1.Params": {
        aminoType: string;
        toAmino: (message: Params) => {};
        fromAmino: (object: Params) => {};
    };
};
