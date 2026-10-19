import type { Coin } from "../../base/v1beta1/coin";
export interface GenesisState {
    /**
     * constant_fee is the fee used to verify the invariant in the crisis
     * module.
     */
    constant_fee?: Coin;
}
