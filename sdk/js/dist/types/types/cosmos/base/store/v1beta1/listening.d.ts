export interface StoreKVPair {
    /** the store key for the KVStore this pair originates from */
    store_key: string;
    /** true indicates a delete operation, false indicates a set operation */
    delete: boolean;
    key: Uint8Array;
    value: Uint8Array;
}
