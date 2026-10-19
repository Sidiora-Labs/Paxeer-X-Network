import type { MessageDependencyMapping, WasmDependencyMapping } from "../accesscontrol/accesscontrol";
export interface MsgUpdateResourceDependencyMappingProposal {
    title: string;
    description: string;
    message_dependency_mapping: MessageDependencyMapping[];
}
export interface MsgUpdateResourceDependencyMappingProposalJsonFile {
    title: string;
    description: string;
    deposit: string;
    message_dependency_mapping: MessageDependencyMapping[];
}
export type MsgUpdateResourceDependencyMappingProposalResponse = {};
export interface MsgUpdateWasmDependencyMappingProposal {
    title: string;
    description: string;
    contract_address: string;
    wasm_dependency_mapping?: WasmDependencyMapping;
}
export interface MsgUpdateWasmDependencyMappingProposalJsonFile {
    title: string;
    description: string;
    deposit: string;
    contract_address: string;
    wasm_dependency_mapping?: WasmDependencyMapping;
}
