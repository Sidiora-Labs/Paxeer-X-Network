import type { Coin } from "../../base/v1beta1/coin";
import type { Metadata, Params } from "./bank";
export interface GenesisState {
    /** params defines all the paramaters of the module. */
    params?: Params;
    /** balances is an array containing the balances of all the accounts. */
    balances: Balance[];
    /**
     * supply represents the total supply. If it is left empty, then supply will be calculated based on the provided
     * balances. Otherwise, it will be used to validate that the sum of the balances equals this amount.
     */
    supply: Coin[];
    /** denom_metadata defines the metadata of the differents coins. */
    denom_metadata: Metadata[];
    /** wei balances */
    wei_balances: WeiBalance[];
}
export interface Balance {
    /** address is the address of the balance holder. */
    address: string;
    /** coins defines the different coins this balance holds. */
    coins: Coin[];
}
export interface WeiBalance {
    /** address is the address of the balance holder. */
    address: string;
    /** wei balance amount. */
    amount: string;
}
