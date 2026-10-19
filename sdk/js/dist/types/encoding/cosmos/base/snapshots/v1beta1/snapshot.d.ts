import type { GeneratedType } from "@cosmjs/proto-signing";
import type { Metadata as Metadata_type, SnapshotExtensionMeta as SnapshotExtensionMeta_type, SnapshotExtensionPayload as SnapshotExtensionPayload_type, SnapshotIAVLItem as SnapshotIAVLItem_type, SnapshotItem as SnapshotItem_type, SnapshotStoreItem as SnapshotStoreItem_type, Snapshot as Snapshot_type } from "../../../../../types/cosmos/base/snapshots/v1beta1";
import type { MessageFns } from "../../../../common";
export interface Snapshot extends Snapshot_type {
}
export interface Metadata extends Metadata_type {
}
export interface SnapshotItem extends SnapshotItem_type {
}
export interface SnapshotStoreItem extends SnapshotStoreItem_type {
}
export interface SnapshotIAVLItem extends SnapshotIAVLItem_type {
}
export interface SnapshotExtensionMeta extends SnapshotExtensionMeta_type {
}
export interface SnapshotExtensionPayload extends SnapshotExtensionPayload_type {
}
export declare const Snapshot: MessageFns<Snapshot, "cosmos.base.snapshots.v1beta1.Snapshot">;
export declare const Metadata: MessageFns<Metadata, "cosmos.base.snapshots.v1beta1.Metadata">;
export declare const SnapshotItem: MessageFns<SnapshotItem, "cosmos.base.snapshots.v1beta1.SnapshotItem">;
export declare const SnapshotStoreItem: MessageFns<SnapshotStoreItem, "cosmos.base.snapshots.v1beta1.SnapshotStoreItem">;
export declare const SnapshotIAVLItem: MessageFns<SnapshotIAVLItem, "cosmos.base.snapshots.v1beta1.SnapshotIAVLItem">;
export declare const SnapshotExtensionMeta: MessageFns<SnapshotExtensionMeta, "cosmos.base.snapshots.v1beta1.SnapshotExtensionMeta">;
export declare const SnapshotExtensionPayload: MessageFns<SnapshotExtensionPayload, "cosmos.base.snapshots.v1beta1.SnapshotExtensionPayload">;
export declare const registry: Array<[string, GeneratedType]>;
export declare const aminoConverters: {
    "/cosmos.base.snapshots.v1beta1.Snapshot": {
        aminoType: string;
        toAmino: (message: Snapshot) => {
            height: number;
            format: number;
            chunks: number;
            hash: Uint8Array;
            metadata?: Metadata_type;
        };
        fromAmino: (object: Snapshot) => {
            height: number;
            format: number;
            chunks: number;
            hash: Uint8Array;
            metadata?: Metadata_type;
        };
    };
    "/cosmos.base.snapshots.v1beta1.Metadata": {
        aminoType: string;
        toAmino: (message: Metadata) => {
            chunk_hashes: Uint8Array[];
        };
        fromAmino: (object: Metadata) => {
            chunk_hashes: Uint8Array[];
        };
    };
    "/cosmos.base.snapshots.v1beta1.SnapshotItem": {
        aminoType: string;
        toAmino: (message: SnapshotItem) => {
            store?: SnapshotStoreItem_type;
            iavl?: SnapshotIAVLItem_type;
            extension?: SnapshotExtensionMeta_type;
            extension_payload?: SnapshotExtensionPayload_type;
        };
        fromAmino: (object: SnapshotItem) => {
            store?: SnapshotStoreItem_type;
            iavl?: SnapshotIAVLItem_type;
            extension?: SnapshotExtensionMeta_type;
            extension_payload?: SnapshotExtensionPayload_type;
        };
    };
    "/cosmos.base.snapshots.v1beta1.SnapshotStoreItem": {
        aminoType: string;
        toAmino: (message: SnapshotStoreItem) => {
            name: string;
        };
        fromAmino: (object: SnapshotStoreItem) => {
            name: string;
        };
    };
    "/cosmos.base.snapshots.v1beta1.SnapshotIAVLItem": {
        aminoType: string;
        toAmino: (message: SnapshotIAVLItem) => {
            key: Uint8Array;
            value: Uint8Array;
            version: number;
            height: number;
        };
        fromAmino: (object: SnapshotIAVLItem) => {
            key: Uint8Array;
            value: Uint8Array;
            version: number;
            height: number;
        };
    };
    "/cosmos.base.snapshots.v1beta1.SnapshotExtensionMeta": {
        aminoType: string;
        toAmino: (message: SnapshotExtensionMeta) => {
            name: string;
            format: number;
        };
        fromAmino: (object: SnapshotExtensionMeta) => {
            name: string;
            format: number;
        };
    };
    "/cosmos.base.snapshots.v1beta1.SnapshotExtensionPayload": {
        aminoType: string;
        toAmino: (message: SnapshotExtensionPayload) => {
            payload: Uint8Array;
        };
        fromAmino: (object: SnapshotExtensionPayload) => {
            payload: Uint8Array;
        };
    };
};
