/**
 * The power of the IMR factor.
 * @constant
 * @default
 */
export const IMRFactorPower = 4 / 5;

/**
 * Isolated margin frozen amount reserves the highest taker fee tier.
 * This mirrors the backend IsoOrderFrozenBuffer Apollo config.
 */
export const IsoTakerFeeBuffer = 0.0006;

/**
 * Safety factor used by max quantity formulas to absorb price races and rounding.
 */
export const MaxQtySafetyFactor = 0.995;
