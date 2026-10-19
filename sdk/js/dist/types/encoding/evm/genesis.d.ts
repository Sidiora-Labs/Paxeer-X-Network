import type { GeneratedType } from "@cosmjs/proto-signing";
import type { AddressAssociation as AddressAssociation_type, Code as Code_type, ContractState as ContractState_type, GenesisState as GenesisState_type, Nonce as Nonce_type, Serialized as Serialized_type } from "../../types/evm";
import type { MessageFns } from "../common";
export interface AddressAssociation extends AddressAssociation_type {
}
export interface Code extends Code_type {
}
export interface ContractState extends ContractState_type {
}
export interface Nonce extends Nonce_type {
}
export interface Serialized extends Serialized_type {
}
export interface GenesisState extends GenesisState_type {
}
export declare const AddressAssociation: MessageFns<AddressAssociation, "paxprotocol.paxchain.evm.AddressAssociation">;
export declare const Code: MessageFns<Code, "paxprotocol.paxchain.evm.Code">;
export declare const ContractState: MessageFns<ContractState, "paxprotocol.paxchain.evm.ContractState">;
export declare const Nonce: MessageFns<Nonce, "paxprotocol.paxchain.evm.Nonce">;
export declare const Serialized: MessageFns<Serialized, "paxprotocol.paxchain.evm.Serialized">;
export declare const GenesisState: MessageFns<GenesisState, "paxprotocol.paxchain.evm.GenesisState">;
export declare const registry: Array<[string, GeneratedType]>;
export declare const aminoConverters: {
    "/paxprotocol.paxchain.evm.AddressAssociation": {
        aminoType: string;
        toAmino: (message: AddressAssociation) => {
            pax_address: string;
            eth_address: string;
        };
        fromAmino: (object: AddressAssociation) => {
            pax_address: string;
            eth_address: string;
        };
    };
    "/paxprotocol.paxchain.evm.Code": {
        aminoType: string;
        toAmino: (message: Code) => {
            address: string;
            code: Uint8Array;
        };
        fromAmino: (object: Code) => {
            address: string;
            code: Uint8Array;
        };
    };
    "/paxprotocol.paxchain.evm.ContractState": {
        aminoType: string;
        toAmino: (message: ContractState) => {
            address: string;
            key: Uint8Array;
            value: Uint8Array;
        };
        fromAmino: (object: ContractState) => {
            address: string;
            key: Uint8Array;
            value: Uint8Array;
        };
    };
    "/paxprotocol.paxchain.evm.Nonce": {
        aminoType: string;
        toAmino: (message: Nonce) => {
            address: string;
            nonce: number;
        };
        fromAmino: (object: Nonce) => {
            address: string;
            nonce: number;
        };
    };
    "/paxprotocol.paxchain.evm.Serialized": {
        aminoType: string;
        toAmino: (message: Serialized) => {
            prefix: Uint8Array;
            key: Uint8Array;
            value: Uint8Array;
        };
        fromAmino: (object: Serialized) => {
            prefix: Uint8Array;
            key: Uint8Array;
            value: Uint8Array;
        };
    };
    "/paxprotocol.paxchain.evm.GenesisState": {
        aminoType: string;
        toAmino: (message: GenesisState) => {
            params?: import("../../types/evm").Params;
            address_associations: AddressAssociation_type[];
            codes: Code_type[];
            states: ContractState_type[];
            nonces: Nonce_type[];
            serialized: Serialized_type[];
        };
        fromAmino: (object: GenesisState) => {
            params?: import("../../types/evm").Params;
            address_associations: AddressAssociation_type[];
            codes: Code_type[];
            states: ContractState_type[];
            nonces: Nonce_type[];
            serialized: Serialized_type[];
        };
    };
};
