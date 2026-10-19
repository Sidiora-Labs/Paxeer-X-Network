import type { GeneratedType } from "@cosmjs/proto-signing";
import type { Params as Params_type, ValidatorMissedBlockArrayLegacyMissedHeights as ValidatorMissedBlockArrayLegacyMissedHeights_type, ValidatorMissedBlockArray as ValidatorMissedBlockArray_type, ValidatorSigningInfoLegacyMissedHeights as ValidatorSigningInfoLegacyMissedHeights_type, ValidatorSigningInfo as ValidatorSigningInfo_type } from "../../../../types/cosmos/slashing/v1beta1";
import type { MessageFns } from "../../../common";
export interface ValidatorSigningInfoLegacyMissedHeights extends ValidatorSigningInfoLegacyMissedHeights_type {
}
export interface ValidatorSigningInfo extends ValidatorSigningInfo_type {
}
export interface ValidatorMissedBlockArrayLegacyMissedHeights extends ValidatorMissedBlockArrayLegacyMissedHeights_type {
}
export interface ValidatorMissedBlockArray extends ValidatorMissedBlockArray_type {
}
export interface Params extends Params_type {
}
export declare const ValidatorSigningInfoLegacyMissedHeights: MessageFns<ValidatorSigningInfoLegacyMissedHeights, "cosmos.slashing.v1beta1.ValidatorSigningInfoLegacyMissedHeights">;
export declare const ValidatorSigningInfo: MessageFns<ValidatorSigningInfo, "cosmos.slashing.v1beta1.ValidatorSigningInfo">;
export declare const ValidatorMissedBlockArrayLegacyMissedHeights: MessageFns<ValidatorMissedBlockArrayLegacyMissedHeights, "cosmos.slashing.v1beta1.ValidatorMissedBlockArrayLegacyMissedHeights">;
export declare const ValidatorMissedBlockArray: MessageFns<ValidatorMissedBlockArray, "cosmos.slashing.v1beta1.ValidatorMissedBlockArray">;
export declare const Params: MessageFns<Params, "cosmos.slashing.v1beta1.Params">;
export declare const registry: Array<[string, GeneratedType]>;
export declare const aminoConverters: {
    "/cosmos.slashing.v1beta1.ValidatorSigningInfoLegacyMissedHeights": {
        aminoType: string;
        toAmino: (message: ValidatorSigningInfoLegacyMissedHeights) => {
            address: string;
            start_height: number;
            jailed_until?: Date;
            tombstoned: boolean;
            missed_blocks_counter: number;
        };
        fromAmino: (object: ValidatorSigningInfoLegacyMissedHeights) => {
            address: string;
            start_height: number;
            jailed_until?: Date;
            tombstoned: boolean;
            missed_blocks_counter: number;
        };
    };
    "/cosmos.slashing.v1beta1.ValidatorSigningInfo": {
        aminoType: string;
        toAmino: (message: ValidatorSigningInfo) => {
            address: string;
            start_height: number;
            index_offset: number;
            jailed_until?: Date;
            tombstoned: boolean;
            missed_blocks_counter: number;
        };
        fromAmino: (object: ValidatorSigningInfo) => {
            address: string;
            start_height: number;
            index_offset: number;
            jailed_until?: Date;
            tombstoned: boolean;
            missed_blocks_counter: number;
        };
    };
    "/cosmos.slashing.v1beta1.ValidatorMissedBlockArrayLegacyMissedHeights": {
        aminoType: string;
        toAmino: (message: ValidatorMissedBlockArrayLegacyMissedHeights) => {
            address: string;
            missed_heights: number[];
        };
        fromAmino: (object: ValidatorMissedBlockArrayLegacyMissedHeights) => {
            address: string;
            missed_heights: number[];
        };
    };
    "/cosmos.slashing.v1beta1.ValidatorMissedBlockArray": {
        aminoType: string;
        toAmino: (message: ValidatorMissedBlockArray) => {
            address: string;
            window_size: number;
            missed_blocks: number[];
        };
        fromAmino: (object: ValidatorMissedBlockArray) => {
            address: string;
            window_size: number;
            missed_blocks: number[];
        };
    };
    "/cosmos.slashing.v1beta1.Params": {
        aminoType: string;
        toAmino: (message: Params) => {
            signed_blocks_window: number;
            min_signed_per_window: Uint8Array;
            downtime_jail_duration?: import("../../../../types/google/protobuf").Duration;
            slash_fraction_double_sign: Uint8Array;
            slash_fraction_downtime: Uint8Array;
        };
        fromAmino: (object: Params) => {
            signed_blocks_window: number;
            min_signed_per_window: Uint8Array;
            downtime_jail_duration?: import("../../../../types/google/protobuf").Duration;
            slash_fraction_double_sign: Uint8Array;
            slash_fraction_downtime: Uint8Array;
        };
    };
};
