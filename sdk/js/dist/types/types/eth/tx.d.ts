export interface AccessTuple {
    address: string;
    storage_keys: string[];
}
export interface AssociateTx {
    /** signature values */
    v: Uint8Array;
    r: Uint8Array;
    s: Uint8Array;
    custom_message: string;
}
export interface LegacyTx {
    nonce: number;
    gas_price: string;
    gas_limit: number;
    to: string;
    value: string;
    data: Uint8Array;
    /** signature values */
    v: Uint8Array;
    r: Uint8Array;
    s: Uint8Array;
}
export interface AccessListTx {
    chain_id: string;
    nonce: number;
    gas_price: string;
    gas_limit: number;
    to: string;
    value: string;
    data: Uint8Array;
    accesses: AccessTuple[];
    /** signature values */
    v: Uint8Array;
    r: Uint8Array;
    s: Uint8Array;
}
export interface DynamicFeeTx {
    chain_id: string;
    nonce: number;
    gas_tip_cap: string;
    gas_fee_cap: string;
    gas_limit: number;
    to: string;
    value: string;
    data: Uint8Array;
    accesses: AccessTuple[];
    /** signature values */
    v: Uint8Array;
    r: Uint8Array;
    s: Uint8Array;
}
export interface BlobTx {
    chain_id: string;
    nonce: number;
    gas_tip_cap: string;
    gas_fee_cap: string;
    gas_limit: number;
    to: string;
    value: string;
    data: Uint8Array;
    accesses: AccessTuple[];
    blob_fee_cap: string;
    blob_hashes: Uint8Array[];
    sidecar?: BlobTxSidecar;
    /** signature values */
    v: Uint8Array;
    r: Uint8Array;
    s: Uint8Array;
}
export interface BlobTxSidecar {
    blobs: Uint8Array[];
    commitments: Uint8Array[];
    proofs: Uint8Array[];
}
export type ExtensionOptionsEthereumTx = {};
