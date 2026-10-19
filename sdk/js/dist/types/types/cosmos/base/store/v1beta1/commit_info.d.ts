export interface CommitInfo {
    version: number;
    store_infos: StoreInfo[];
}
export interface StoreInfo {
    name: string;
    commit_id?: CommitID;
}
export interface CommitID {
    version: number;
    hash: Uint8Array;
}
