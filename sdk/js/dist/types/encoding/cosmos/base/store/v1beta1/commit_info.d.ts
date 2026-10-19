import type { GeneratedType } from "@cosmjs/proto-signing";
import type { CommitID as CommitID_type, CommitInfo as CommitInfo_type, StoreInfo as StoreInfo_type } from "../../../../../types/cosmos/base/store/v1beta1";
import type { MessageFns } from "../../../../common";
export interface CommitInfo extends CommitInfo_type {
}
export interface StoreInfo extends StoreInfo_type {
}
export interface CommitID extends CommitID_type {
}
export declare const CommitInfo: MessageFns<CommitInfo, "cosmos.base.store.v1beta1.CommitInfo">;
export declare const StoreInfo: MessageFns<StoreInfo, "cosmos.base.store.v1beta1.StoreInfo">;
export declare const CommitID: MessageFns<CommitID, "cosmos.base.store.v1beta1.CommitID">;
export declare const registry: Array<[string, GeneratedType]>;
export declare const aminoConverters: {
    "/cosmos.base.store.v1beta1.CommitInfo": {
        aminoType: string;
        toAmino: (message: CommitInfo) => {
            version: number;
            store_infos: StoreInfo_type[];
        };
        fromAmino: (object: CommitInfo) => {
            version: number;
            store_infos: StoreInfo_type[];
        };
    };
    "/cosmos.base.store.v1beta1.StoreInfo": {
        aminoType: string;
        toAmino: (message: StoreInfo) => {
            name: string;
            commit_id?: CommitID_type;
        };
        fromAmino: (object: StoreInfo) => {
            name: string;
            commit_id?: CommitID_type;
        };
    };
    "/cosmos.base.store.v1beta1.CommitID": {
        aminoType: string;
        toAmino: (message: CommitID) => {
            version: number;
            hash: Uint8Array;
        };
        fromAmino: (object: CommitID) => {
            version: number;
            hash: Uint8Array;
        };
    };
};
