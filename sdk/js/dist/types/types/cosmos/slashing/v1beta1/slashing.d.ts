import type { Duration } from "../../../google/protobuf/duration";
export interface ValidatorSigningInfoLegacyMissedHeights {
    address: string;
    /** Height at which validator was first a candidate OR was unjailed */
    start_height: number;
    /** Timestamp until which the validator is jailed due to liveness downtime. */
    jailed_until?: Date;
    /**
     * Whether or not a validator has been tombstoned (killed out of validator set). It is set
     * once the validator commits an equivocation or for any other configured misbehiavor.
     */
    tombstoned: boolean;
    /**
     * A counter kept to avoid unnecessary array reads.
     * Note that `Sum(MissedBlocksBitArray)` always equals `MissedBlocksCounter`.
     */
    missed_blocks_counter: number;
}
export interface ValidatorSigningInfo {
    address: string;
    /** Height at which validator was first a candidate OR was unjailed */
    start_height: number;
    /**
     * Index which is incremented each time the validator was a bonded
     * in a block and may have signed a precommit or not. This in conjunction with the
     * `SignedBlocksWindow` param determines the index in the `MissedBlocksBitArray`.
     */
    index_offset: number;
    /** Timestamp until which the validator is jailed due to liveness downtime. */
    jailed_until?: Date;
    /**
     * Whether or not a validator has been tombstoned (killed out of validator set). It is set
     * once the validator commits an equivocation or for any other configured misbehiavor.
     */
    tombstoned: boolean;
    /**
     * A counter kept to avoid unnecessary array reads.
     * Note that `Sum(MissedBlocksBitArray)` always equals `MissedBlocksCounter`.
     */
    missed_blocks_counter: number;
}
export interface ValidatorMissedBlockArrayLegacyMissedHeights {
    address: string;
    /** Array of contains the heights when the validator missed the block */
    missed_heights: number[];
}
export interface ValidatorMissedBlockArray {
    address: string;
    /** store this in case window size changes but doesn't affect number of bit groups */
    window_size: number;
    /** Array of contains the missed block bits packed into uint64s */
    missed_blocks: number[];
}
export interface Params {
    signed_blocks_window: number;
    min_signed_per_window: Uint8Array;
    downtime_jail_duration?: Duration;
    slash_fraction_double_sign: Uint8Array;
    slash_fraction_downtime: Uint8Array;
}
