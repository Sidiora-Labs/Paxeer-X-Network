import type { Params } from "./params";
export interface AddressAssociation {
    /** Pax address */
    pax_address: string;
    /** Ethereum address */
    eth_address: string;
}
export interface Code {
    address: string;
    code: Uint8Array;
}
export interface ContractState {
    address: string;
    key: Uint8Array;
    value: Uint8Array;
}
export interface Nonce {
    address: string;
    nonce: number;
}
export interface Serialized {
    prefix: Uint8Array;
    key: Uint8Array;
    value: Uint8Array;
}
export interface GenesisState {
    params?: Params;
    /** List of address associations */
    address_associations: AddressAssociation[];
    /** List of stored code */
    codes: Code[];
    /** List of contract state */
    states: ContractState[];
    nonces: Nonce[];
    serialized: Serialized[];
}
