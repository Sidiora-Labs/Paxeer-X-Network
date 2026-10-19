import type { GeneratedType } from "@cosmjs/proto-signing";
import type { GenesisStateLegacyMissingHeights as GenesisStateLegacyMissingHeights_type, GenesisStateLegacyV43 as GenesisStateLegacyV43_type, GenesisState as GenesisState_type, MissedBlock as MissedBlock_type, SigningInfoLegacyMissedHeights as SigningInfoLegacyMissedHeights_type, SigningInfo as SigningInfo_type, ValidatorMissedBlocks as ValidatorMissedBlocks_type } from "../../../../types/cosmos/slashing/v1beta1";
import type { MessageFns } from "../../../common";
export interface GenesisState extends GenesisState_type {
}
export interface GenesisStateLegacyMissingHeights extends GenesisStateLegacyMissingHeights_type {
}
export interface GenesisStateLegacyV43 extends GenesisStateLegacyV43_type {
}
export interface SigningInfo extends SigningInfo_type {
}
export interface SigningInfoLegacyMissedHeights extends SigningInfoLegacyMissedHeights_type {
}
export interface ValidatorMissedBlocks extends ValidatorMissedBlocks_type {
}
export interface MissedBlock extends MissedBlock_type {
}
export declare const GenesisState: MessageFns<GenesisState, "cosmos.slashing.v1beta1.GenesisState">;
export declare const GenesisStateLegacyMissingHeights: MessageFns<GenesisStateLegacyMissingHeights, "cosmos.slashing.v1beta1.GenesisStateLegacyMissingHeights">;
export declare const GenesisStateLegacyV43: MessageFns<GenesisStateLegacyV43, "cosmos.slashing.v1beta1.GenesisStateLegacyV43">;
export declare const SigningInfo: MessageFns<SigningInfo, "cosmos.slashing.v1beta1.SigningInfo">;
export declare const SigningInfoLegacyMissedHeights: MessageFns<SigningInfoLegacyMissedHeights, "cosmos.slashing.v1beta1.SigningInfoLegacyMissedHeights">;
export declare const ValidatorMissedBlocks: MessageFns<ValidatorMissedBlocks, "cosmos.slashing.v1beta1.ValidatorMissedBlocks">;
export declare const MissedBlock: MessageFns<MissedBlock, "cosmos.slashing.v1beta1.MissedBlock">;
export declare const registry: Array<[string, GeneratedType]>;
export declare const aminoConverters: {
    "/cosmos.slashing.v1beta1.GenesisState": {
        aminoType: string;
        toAmino: (message: GenesisState) => {
            params?: import("../../../../types/cosmos/slashing/v1beta1").Params;
            signing_infos: SigningInfo_type[];
            missed_blocks: import("../../../../types/cosmos/slashing/v1beta1").ValidatorMissedBlockArray[];
        };
        fromAmino: (object: GenesisState) => {
            params?: import("../../../../types/cosmos/slashing/v1beta1").Params;
            signing_infos: SigningInfo_type[];
            missed_blocks: import("../../../../types/cosmos/slashing/v1beta1").ValidatorMissedBlockArray[];
        };
    };
    "/cosmos.slashing.v1beta1.GenesisStateLegacyMissingHeights": {
        aminoType: string;
        toAmino: (message: GenesisStateLegacyMissingHeights) => {
            params?: import("../../../../types/cosmos/slashing/v1beta1").Params;
            signing_infos: SigningInfo_type[];
            missed_blocks: import("../../../../types/cosmos/slashing/v1beta1").ValidatorMissedBlockArrayLegacyMissedHeights[];
        };
        fromAmino: (object: GenesisStateLegacyMissingHeights) => {
            params?: import("../../../../types/cosmos/slashing/v1beta1").Params;
            signing_infos: SigningInfo_type[];
            missed_blocks: import("../../../../types/cosmos/slashing/v1beta1").ValidatorMissedBlockArrayLegacyMissedHeights[];
        };
    };
    "/cosmos.slashing.v1beta1.GenesisStateLegacyV43": {
        aminoType: string;
        toAmino: (message: GenesisStateLegacyV43) => {
            params?: import("../../../../types/cosmos/slashing/v1beta1").Params;
            signing_infos: SigningInfo_type[];
            missed_blocks: ValidatorMissedBlocks_type[];
        };
        fromAmino: (object: GenesisStateLegacyV43) => {
            params?: import("../../../../types/cosmos/slashing/v1beta1").Params;
            signing_infos: SigningInfo_type[];
            missed_blocks: ValidatorMissedBlocks_type[];
        };
    };
    "/cosmos.slashing.v1beta1.SigningInfo": {
        aminoType: string;
        toAmino: (message: SigningInfo) => {
            address: string;
            validator_signing_info?: import("../../../../types/cosmos/slashing/v1beta1").ValidatorSigningInfo;
        };
        fromAmino: (object: SigningInfo) => {
            address: string;
            validator_signing_info?: import("../../../../types/cosmos/slashing/v1beta1").ValidatorSigningInfo;
        };
    };
    "/cosmos.slashing.v1beta1.SigningInfoLegacyMissedHeights": {
        aminoType: string;
        toAmino: (message: SigningInfoLegacyMissedHeights) => {
            address: string;
            validator_signing_info?: import("../../../../types/cosmos/slashing/v1beta1").ValidatorSigningInfoLegacyMissedHeights;
        };
        fromAmino: (object: SigningInfoLegacyMissedHeights) => {
            address: string;
            validator_signing_info?: import("../../../../types/cosmos/slashing/v1beta1").ValidatorSigningInfoLegacyMissedHeights;
        };
    };
    "/cosmos.slashing.v1beta1.ValidatorMissedBlocks": {
        aminoType: string;
        toAmino: (message: ValidatorMissedBlocks) => {
            address: string;
            missed_blocks: MissedBlock_type[];
        };
        fromAmino: (object: ValidatorMissedBlocks) => {
            address: string;
            missed_blocks: MissedBlock_type[];
        };
    };
    "/cosmos.slashing.v1beta1.MissedBlock": {
        aminoType: string;
        toAmino: (message: MissedBlock) => {
            index: number;
            missed: boolean;
        };
        fromAmino: (object: MissedBlock) => {
            index: number;
            missed: boolean;
        };
    };
};
