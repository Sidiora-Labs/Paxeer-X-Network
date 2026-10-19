import type { Params, ValidatorMissedBlockArray, ValidatorMissedBlockArrayLegacyMissedHeights, ValidatorSigningInfo, ValidatorSigningInfoLegacyMissedHeights } from "./slashing";
export interface GenesisState {
    /** params defines all the paramaters of related to slashing. */
    params?: Params;
    /**
     * signing_infos represents a map between validator addresses and their
     * signing infos.
     */
    signing_infos: SigningInfo[];
    /**
     * missed_blocks represents a map between validator addresses and their
     * missed blocks.
     */
    missed_blocks: ValidatorMissedBlockArray[];
}
export interface GenesisStateLegacyMissingHeights {
    /** params defines all the paramaters of related to slashing. */
    params?: Params;
    /**
     * signing_infos represents a map between validator addresses and their
     * signing infos.
     */
    signing_infos: SigningInfo[];
    /**
     * missed_blocks represents a map between validator addresses and their
     * missed blocks.
     */
    missed_blocks: ValidatorMissedBlockArrayLegacyMissedHeights[];
}
export interface GenesisStateLegacyV43 {
    /** params defines all the paramaters of related to slashing. */
    params?: Params;
    /**
     * signing_infos represents a map between validator addresses and their
     * signing infos.
     */
    signing_infos: SigningInfo[];
    /**
     * missed_blocks represents a map between validator addresses and their
     * missed blocks.
     */
    missed_blocks: ValidatorMissedBlocks[];
}
export interface SigningInfo {
    /** address is the validator address. */
    address: string;
    /** validator_signing_info represents the signing info of this validator. */
    validator_signing_info?: ValidatorSigningInfo;
}
export interface SigningInfoLegacyMissedHeights {
    /** address is the validator address. */
    address: string;
    /** validator_signing_info represents the signing info of this validator. */
    validator_signing_info?: ValidatorSigningInfoLegacyMissedHeights;
}
export interface ValidatorMissedBlocks {
    /** address is the validator address. */
    address: string;
    /** missed_blocks is an array of missed blocks by the validator. */
    missed_blocks: MissedBlock[];
}
export interface MissedBlock {
    /** index is the height at which the block was missed. */
    index: number;
    /** missed is the missed status. */
    missed: boolean;
}
