import type { GeneratedType } from "@cosmjs/proto-signing";
import type { MsgUpdateResourceDependencyMappingProposalJsonFile as MsgUpdateResourceDependencyMappingProposalJsonFile_type, MsgUpdateResourceDependencyMappingProposalResponse as MsgUpdateResourceDependencyMappingProposalResponse_type, MsgUpdateResourceDependencyMappingProposal as MsgUpdateResourceDependencyMappingProposal_type, MsgUpdateWasmDependencyMappingProposalJsonFile as MsgUpdateWasmDependencyMappingProposalJsonFile_type, MsgUpdateWasmDependencyMappingProposal as MsgUpdateWasmDependencyMappingProposal_type } from "../../../types/cosmos/accesscontrol_x";
import type { MessageFns } from "../../common";
export interface MsgUpdateResourceDependencyMappingProposal extends MsgUpdateResourceDependencyMappingProposal_type {
}
export interface MsgUpdateResourceDependencyMappingProposalJsonFile extends MsgUpdateResourceDependencyMappingProposalJsonFile_type {
}
export interface MsgUpdateResourceDependencyMappingProposalResponse extends MsgUpdateResourceDependencyMappingProposalResponse_type {
}
export interface MsgUpdateWasmDependencyMappingProposal extends MsgUpdateWasmDependencyMappingProposal_type {
}
export interface MsgUpdateWasmDependencyMappingProposalJsonFile extends MsgUpdateWasmDependencyMappingProposalJsonFile_type {
}
export declare const MsgUpdateResourceDependencyMappingProposal: MessageFns<MsgUpdateResourceDependencyMappingProposal, "cosmos.accesscontrol.v1beta1.MsgUpdateResourceDependencyMappingProposal">;
export declare const MsgUpdateResourceDependencyMappingProposalJsonFile: MessageFns<MsgUpdateResourceDependencyMappingProposalJsonFile, "cosmos.accesscontrol.v1beta1.MsgUpdateResourceDependencyMappingProposalJsonFile">;
export declare const MsgUpdateResourceDependencyMappingProposalResponse: MessageFns<MsgUpdateResourceDependencyMappingProposalResponse, "cosmos.accesscontrol.v1beta1.MsgUpdateResourceDependencyMappingProposalResponse">;
export declare const MsgUpdateWasmDependencyMappingProposal: MessageFns<MsgUpdateWasmDependencyMappingProposal, "cosmos.accesscontrol.v1beta1.MsgUpdateWasmDependencyMappingProposal">;
export declare const MsgUpdateWasmDependencyMappingProposalJsonFile: MessageFns<MsgUpdateWasmDependencyMappingProposalJsonFile, "cosmos.accesscontrol.v1beta1.MsgUpdateWasmDependencyMappingProposalJsonFile">;
export declare const registry: Array<[string, GeneratedType]>;
export declare const aminoConverters: {
    "/cosmos.accesscontrol.v1beta1.MsgUpdateResourceDependencyMappingProposal": {
        aminoType: string;
        toAmino: (message: MsgUpdateResourceDependencyMappingProposal) => {
            title: string;
            description: string;
            message_dependency_mapping: import("../../../types/cosmos/accesscontrol").MessageDependencyMapping[];
        };
        fromAmino: (object: MsgUpdateResourceDependencyMappingProposal) => {
            title: string;
            description: string;
            message_dependency_mapping: import("../../../types/cosmos/accesscontrol").MessageDependencyMapping[];
        };
    };
    "/cosmos.accesscontrol.v1beta1.MsgUpdateResourceDependencyMappingProposalJsonFile": {
        aminoType: string;
        toAmino: (message: MsgUpdateResourceDependencyMappingProposalJsonFile) => {
            title: string;
            description: string;
            deposit: string;
            message_dependency_mapping: import("../../../types/cosmos/accesscontrol").MessageDependencyMapping[];
        };
        fromAmino: (object: MsgUpdateResourceDependencyMappingProposalJsonFile) => {
            title: string;
            description: string;
            deposit: string;
            message_dependency_mapping: import("../../../types/cosmos/accesscontrol").MessageDependencyMapping[];
        };
    };
    "/cosmos.accesscontrol.v1beta1.MsgUpdateResourceDependencyMappingProposalResponse": {
        aminoType: string;
        toAmino: (message: MsgUpdateResourceDependencyMappingProposalResponse) => {};
        fromAmino: (object: MsgUpdateResourceDependencyMappingProposalResponse) => {};
    };
    "/cosmos.accesscontrol.v1beta1.MsgUpdateWasmDependencyMappingProposal": {
        aminoType: string;
        toAmino: (message: MsgUpdateWasmDependencyMappingProposal) => {
            title: string;
            description: string;
            contract_address: string;
            wasm_dependency_mapping?: import("../../../types/cosmos/accesscontrol").WasmDependencyMapping;
        };
        fromAmino: (object: MsgUpdateWasmDependencyMappingProposal) => {
            title: string;
            description: string;
            contract_address: string;
            wasm_dependency_mapping?: import("../../../types/cosmos/accesscontrol").WasmDependencyMapping;
        };
    };
    "/cosmos.accesscontrol.v1beta1.MsgUpdateWasmDependencyMappingProposalJsonFile": {
        aminoType: string;
        toAmino: (message: MsgUpdateWasmDependencyMappingProposalJsonFile) => {
            title: string;
            description: string;
            deposit: string;
            contract_address: string;
            wasm_dependency_mapping?: import("../../../types/cosmos/accesscontrol").WasmDependencyMapping;
        };
        fromAmino: (object: MsgUpdateWasmDependencyMappingProposalJsonFile) => {
            title: string;
            description: string;
            deposit: string;
            contract_address: string;
            wasm_dependency_mapping?: import("../../../types/cosmos/accesscontrol").WasmDependencyMapping;
        };
    };
};
