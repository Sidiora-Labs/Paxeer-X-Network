import type { GeneratedType } from "@cosmjs/proto-signing";
import type { MsgRegisterWasmDependencyResponse as MsgRegisterWasmDependencyResponse_type, MsgRegisterWasmDependency as MsgRegisterWasmDependency_type, RegisterWasmDependencyJSONFile as RegisterWasmDependencyJSONFile_type } from "../../../types/cosmos/accesscontrol_x";
import type { MessageFns } from "../../common";
export interface RegisterWasmDependencyJSONFile extends RegisterWasmDependencyJSONFile_type {
}
export interface MsgRegisterWasmDependency extends MsgRegisterWasmDependency_type {
}
export interface MsgRegisterWasmDependencyResponse extends MsgRegisterWasmDependencyResponse_type {
}
export declare const RegisterWasmDependencyJSONFile: MessageFns<RegisterWasmDependencyJSONFile, "cosmos.accesscontrol_x.v1beta1.RegisterWasmDependencyJSONFile">;
export declare const MsgRegisterWasmDependency: MessageFns<MsgRegisterWasmDependency, "cosmos.accesscontrol_x.v1beta1.MsgRegisterWasmDependency">;
export declare const MsgRegisterWasmDependencyResponse: MessageFns<MsgRegisterWasmDependencyResponse, "cosmos.accesscontrol_x.v1beta1.MsgRegisterWasmDependencyResponse">;
export declare const registry: Array<[string, GeneratedType]>;
export declare const aminoConverters: {
    "/cosmos.accesscontrol_x.v1beta1.RegisterWasmDependencyJSONFile": {
        aminoType: string;
        toAmino: (message: RegisterWasmDependencyJSONFile) => {
            wasm_dependency_mapping?: import("../../../types/cosmos/accesscontrol").WasmDependencyMapping;
        };
        fromAmino: (object: RegisterWasmDependencyJSONFile) => {
            wasm_dependency_mapping?: import("../../../types/cosmos/accesscontrol").WasmDependencyMapping;
        };
    };
    "/cosmos.accesscontrol_x.v1beta1.MsgRegisterWasmDependency": {
        aminoType: string;
        toAmino: (message: MsgRegisterWasmDependency) => {
            from_address: string;
            wasm_dependency_mapping?: import("../../../types/cosmos/accesscontrol").WasmDependencyMapping;
        };
        fromAmino: (object: MsgRegisterWasmDependency) => {
            from_address: string;
            wasm_dependency_mapping?: import("../../../types/cosmos/accesscontrol").WasmDependencyMapping;
        };
    };
    "/cosmos.accesscontrol_x.v1beta1.MsgRegisterWasmDependencyResponse": {
        aminoType: string;
        toAmino: (message: MsgRegisterWasmDependencyResponse) => {};
        fromAmino: (object: MsgRegisterWasmDependencyResponse) => {};
    };
};
