import type { Delegation, Params, Redelegation, UnbondingDelegation, Validator } from "./staking";
export interface GenesisState {
    /** params defines all the paramaters of related to deposit. */
    params?: Params;
    /**
     * last_total_power tracks the total amounts of bonded tokens recorded during
     * the previous end block.
     */
    last_total_power: Uint8Array;
    /**
     * last_validator_powers is a special index that provides a historical list
     * of the last-block's bonded validators.
     */
    last_validator_powers: LastValidatorPower[];
    /** delegations defines the validator set at genesis. */
    validators: Validator[];
    /** delegations defines the delegations active at genesis. */
    delegations: Delegation[];
    /** unbonding_delegations defines the unbonding delegations active at genesis. */
    unbonding_delegations: UnbondingDelegation[];
    /** redelegations defines the redelegations active at genesis. */
    redelegations: Redelegation[];
    exported: boolean;
}
export interface LastValidatorPower {
    /** address is the address of the validator. */
    address: string;
    /** power defines the power of the validator. */
    power: number;
}
