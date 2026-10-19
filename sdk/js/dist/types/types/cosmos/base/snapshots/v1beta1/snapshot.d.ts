export interface Snapshot {
    height: number;
    format: number;
    chunks: number;
    hash: Uint8Array;
    metadata?: Metadata;
}
export interface Metadata {
    /** SHA-256 chunk hashes */
    chunk_hashes: Uint8Array[];
}
export interface SnapshotItem {
    store?: SnapshotStoreItem;
    iavl?: SnapshotIAVLItem;
    extension?: SnapshotExtensionMeta;
    extension_payload?: SnapshotExtensionPayload;
}
export interface SnapshotStoreItem {
    name: string;
}
export interface SnapshotIAVLItem {
    key: Uint8Array;
    value: Uint8Array;
    /** version is block height */
    version: number;
    /** height is depth of the tree. */
    height: number;
}
export interface SnapshotExtensionMeta {
    name: string;
    format: number;
}
export interface SnapshotExtensionPayload {
    payload: Uint8Array;
}
