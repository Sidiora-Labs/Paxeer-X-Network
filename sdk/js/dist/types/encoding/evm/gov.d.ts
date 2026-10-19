import type { GeneratedType } from "@cosmjs/proto-signing";
import type { AddCWERC20PointerProposal as AddCWERC20PointerProposal_type, AddCWERC721PointerProposal as AddCWERC721PointerProposal_type, AddCWERC1155PointerProposal as AddCWERC1155PointerProposal_type, AddERCCW20PointerProposal as AddERCCW20PointerProposal_type, AddERCCW721PointerProposal as AddERCCW721PointerProposal_type, AddERCCW1155PointerProposal as AddERCCW1155PointerProposal_type, AddERCNativePointerProposalV2 as AddERCNativePointerProposalV2_type, AddERCNativePointerProposal as AddERCNativePointerProposal_type } from "../../types/evm";
import type { MessageFns } from "../common";
export interface AddERCNativePointerProposal extends AddERCNativePointerProposal_type {
}
export interface AddERCCW20PointerProposal extends AddERCCW20PointerProposal_type {
}
export interface AddERCCW721PointerProposal extends AddERCCW721PointerProposal_type {
}
export interface AddERCCW1155PointerProposal extends AddERCCW1155PointerProposal_type {
}
export interface AddCWERC20PointerProposal extends AddCWERC20PointerProposal_type {
}
export interface AddCWERC721PointerProposal extends AddCWERC721PointerProposal_type {
}
export interface AddCWERC1155PointerProposal extends AddCWERC1155PointerProposal_type {
}
export interface AddERCNativePointerProposalV2 extends AddERCNativePointerProposalV2_type {
}
export declare const AddERCNativePointerProposal: MessageFns<AddERCNativePointerProposal, "paxprotocol.paxchain.evm.AddERCNativePointerProposal">;
export declare const AddERCCW20PointerProposal: MessageFns<AddERCCW20PointerProposal, "paxprotocol.paxchain.evm.AddERCCW20PointerProposal">;
export declare const AddERCCW721PointerProposal: MessageFns<AddERCCW721PointerProposal, "paxprotocol.paxchain.evm.AddERCCW721PointerProposal">;
export declare const AddERCCW1155PointerProposal: MessageFns<AddERCCW1155PointerProposal, "paxprotocol.paxchain.evm.AddERCCW1155PointerProposal">;
export declare const AddCWERC20PointerProposal: MessageFns<AddCWERC20PointerProposal, "paxprotocol.paxchain.evm.AddCWERC20PointerProposal">;
export declare const AddCWERC721PointerProposal: MessageFns<AddCWERC721PointerProposal, "paxprotocol.paxchain.evm.AddCWERC721PointerProposal">;
export declare const AddCWERC1155PointerProposal: MessageFns<AddCWERC1155PointerProposal, "paxprotocol.paxchain.evm.AddCWERC1155PointerProposal">;
export declare const AddERCNativePointerProposalV2: MessageFns<AddERCNativePointerProposalV2, "paxprotocol.paxchain.evm.AddERCNativePointerProposalV2">;
export declare const registry: Array<[string, GeneratedType]>;
export declare const aminoConverters: {
    "/paxprotocol.paxchain.evm.AddERCNativePointerProposal": {
        aminoType: string;
        toAmino: (message: AddERCNativePointerProposal) => {
            title: string;
            description: string;
            token: string;
            pointer: string;
            version: number;
        };
        fromAmino: (object: AddERCNativePointerProposal) => {
            title: string;
            description: string;
            token: string;
            pointer: string;
            version: number;
        };
    };
    "/paxprotocol.paxchain.evm.AddERCCW20PointerProposal": {
        aminoType: string;
        toAmino: (message: AddERCCW20PointerProposal) => {
            title: string;
            description: string;
            pointee: string;
            pointer: string;
            version: number;
        };
        fromAmino: (object: AddERCCW20PointerProposal) => {
            title: string;
            description: string;
            pointee: string;
            pointer: string;
            version: number;
        };
    };
    "/paxprotocol.paxchain.evm.AddERCCW721PointerProposal": {
        aminoType: string;
        toAmino: (message: AddERCCW721PointerProposal) => {
            title: string;
            description: string;
            pointee: string;
            pointer: string;
            version: number;
        };
        fromAmino: (object: AddERCCW721PointerProposal) => {
            title: string;
            description: string;
            pointee: string;
            pointer: string;
            version: number;
        };
    };
    "/paxprotocol.paxchain.evm.AddERCCW1155PointerProposal": {
        aminoType: string;
        toAmino: (message: AddERCCW1155PointerProposal) => {
            title: string;
            description: string;
            pointee: string;
            pointer: string;
            version: number;
        };
        fromAmino: (object: AddERCCW1155PointerProposal) => {
            title: string;
            description: string;
            pointee: string;
            pointer: string;
            version: number;
        };
    };
    "/paxprotocol.paxchain.evm.AddCWERC20PointerProposal": {
        aminoType: string;
        toAmino: (message: AddCWERC20PointerProposal) => {
            title: string;
            description: string;
            pointee: string;
            pointer: string;
            version: number;
        };
        fromAmino: (object: AddCWERC20PointerProposal) => {
            title: string;
            description: string;
            pointee: string;
            pointer: string;
            version: number;
        };
    };
    "/paxprotocol.paxchain.evm.AddCWERC721PointerProposal": {
        aminoType: string;
        toAmino: (message: AddCWERC721PointerProposal) => {
            title: string;
            description: string;
            pointee: string;
            pointer: string;
            version: number;
        };
        fromAmino: (object: AddCWERC721PointerProposal) => {
            title: string;
            description: string;
            pointee: string;
            pointer: string;
            version: number;
        };
    };
    "/paxprotocol.paxchain.evm.AddCWERC1155PointerProposal": {
        aminoType: string;
        toAmino: (message: AddCWERC1155PointerProposal) => {
            title: string;
            description: string;
            pointee: string;
            pointer: string;
            version: number;
        };
        fromAmino: (object: AddCWERC1155PointerProposal) => {
            title: string;
            description: string;
            pointee: string;
            pointer: string;
            version: number;
        };
    };
    "/paxprotocol.paxchain.evm.AddERCNativePointerProposalV2": {
        aminoType: string;
        toAmino: (message: AddERCNativePointerProposalV2) => {
            title: string;
            description: string;
            token: string;
            name: string;
            symbol: string;
            decimals: number;
        };
        fromAmino: (object: AddERCNativePointerProposalV2) => {
            title: string;
            description: string;
            token: string;
            name: string;
            symbol: string;
            decimals: number;
        };
    };
};
