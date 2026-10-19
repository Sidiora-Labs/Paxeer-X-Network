export interface Whitelist {
    hashes: string[];
}
export interface DeferredInfo {
    tx_index: number;
    tx_hash: Uint8Array;
    tx_bloom: Uint8Array;
    surplus: string;
    error: string;
}
