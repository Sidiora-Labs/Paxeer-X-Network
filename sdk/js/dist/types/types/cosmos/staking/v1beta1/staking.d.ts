import type { Any } from "../../../google/protobuf/any";
import type { Duration } from "../../../google/protobuf/duration";
import type { Header } from "../../../tendermint/types/types";
import type { Coin } from "../../base/v1beta1/coin";
export declare enum BondStatus {
    /** BOND_STATUS_UNSPECIFIED - UNSPECIFIED defines an invalid validator status. */
    BOND_STATUS_UNSPECIFIED = 0,
    /** BOND_STATUS_UNBONDED - UNBONDED defines a validator that is not bonded. */
    BOND_STATUS_UNBONDED = 1,
    /** BOND_STATUS_UNBONDING - UNBONDING defines a validator that is unbonding. */
    BOND_STATUS_UNBONDING = 2,
    /** BOND_STATUS_BONDED - BONDED defines a validator that is bonded. */
    BOND_STATUS_BONDED = 3,
    UNRECOGNIZED = -1
}
export interface HistoricalInfo {
    header?: Header;
    valset: Validator[];
}
export interface CommissionRates {
    /** rate is the commission rate charged to delegators, as a fraction. */
    rate: string;
    /** max_rate defines the maximum commission rate which validator can ever charge, as a fraction. */
    max_rate: string;
    /** max_change_rate defines the maximum daily increase of the validator commission, as a fraction. */
    max_change_rate: string;
}
export interface Commission {
    /** commission_rates defines the initial commission rates to be used for creating a validator. */
    commission_rates?: CommissionRates;
    /** update_time is the last time the commission rate was changed. */
    update_time?: Date;
}
export interface Description {
    /** moniker defines a human-readable name for the validator. */
    moniker: string;
    /** identity defines an optional identity signature (ex. UPort or Keybase). */
    identity: string;
    /** website defines an optional website link. */
    website: string;
    /** security_contact defines an optional email for security contact. */
    security_contact: string;
    /** details define other optional details. */
    details: string;
}
export interface Validator {
    /** operator_address defines the address of the validator's operator; bech encoded in JSON. */
    operator_address: string;
    /** consensus_pubkey is the consensus public key of the validator, as a Protobuf Any. */
    consensus_pubkey?: Any;
    /** jailed defined whether the validator has been jailed from bonded status or not. */
    jailed: boolean;
    /** status is the validator status (bonded/unbonding/unbonded). */
    status: BondStatus;
    /** tokens define the delegated tokens (incl. self-delegation). */
    tokens: string;
    /** delegator_shares defines total shares issued to a validator's delegators. */
    delegator_shares: string;
    /** description defines the description terms for the validator. */
    description?: Description;
    /** unbonding_height defines, if unbonding, the height at which this validator has begun unbonding. */
    unbonding_height: number;
    /** unbonding_time defines, if unbonding, the min time for the validator to complete unbonding. */
    unbonding_time?: Date;
    /** commission defines the commission parameters. */
    commission?: Commission;
    /** min_self_delegation is the validator's self declared minimum self delegation. */
    min_self_delegation: string;
}
export interface ValAddresses {
    addresses: string[];
}
export interface DVPair {
    delegator_address: string;
    validator_address: string;
}
export interface DVPairs {
    pairs: DVPair[];
}
export interface DVVTriplet {
    delegator_address: string;
    validator_src_address: string;
    validator_dst_address: string;
}
export interface DVVTriplets {
    triplets: DVVTriplet[];
}
export interface Delegation {
    /** delegator_address is the bech32-encoded address of the delegator. */
    delegator_address: string;
    /** validator_address is the bech32-encoded address of the validator. */
    validator_address: string;
    /** shares define the delegation shares received. */
    shares: string;
}
export interface UnbondingDelegation {
    /** delegator_address is the bech32-encoded address of the delegator. */
    delegator_address: string;
    /** validator_address is the bech32-encoded address of the validator. */
    validator_address: string;
    /** entries are the unbonding delegation entries. */
    entries: UnbondingDelegationEntry[];
}
export interface UnbondingDelegationEntry {
    /** creation_height is the height which the unbonding took place. */
    creation_height: number;
    /** completion_time is the unix time for unbonding completion. */
    completion_time?: Date;
    /** initial_balance defines the tokens initially scheduled to receive at completion. */
    initial_balance: string;
    /** balance defines the tokens to receive at completion. */
    balance: string;
}
export interface RedelegationEntry {
    /** creation_height  defines the height which the redelegation took place. */
    creation_height: number;
    /** completion_time defines the unix time for redelegation completion. */
    completion_time?: Date;
    /** initial_balance defines the initial balance when redelegation started. */
    initial_balance: string;
    /** shares_dst is the amount of destination-validator shares created by redelegation. */
    shares_dst: string;
}
export interface Redelegation {
    /** delegator_address is the bech32-encoded address of the delegator. */
    delegator_address: string;
    /** validator_src_address is the validator redelegation source operator address. */
    validator_src_address: string;
    /** validator_dst_address is the validator redelegation destination operator address. */
    validator_dst_address: string;
    /** entries are the redelegation entries. */
    entries: RedelegationEntry[];
}
export interface Params {
    /** unbonding_time is the time duration of unbonding. */
    unbonding_time?: Duration;
    /** max_validators is the maximum number of validators. */
    max_validators: number;
    /** max_entries is the max entries for either unbonding delegation or redelegation (per pair/trio). */
    max_entries: number;
    /** historical_entries is the number of historical entries to persist. */
    historical_entries: number;
    /** bond_denom defines the bondable coin denomination. */
    bond_denom: string;
    /** min_commission_rate is the chain-wide minimum commission rate that a validator can charge their delegators */
    min_commission_rate: string;
    /** max_voting_power_ratio defines the maximal allowable voting power ratio delegated to a validator. */
    max_voting_power_ratio: string;
    /** max_voting_power_enforcement_threshold defines the minimal bonded voting power of the max voting power ratio enforcement */
    max_voting_power_enforcement_threshold: string;
}
export interface DelegationResponse {
    delegation?: Delegation;
    balance?: Coin;
}
export interface RedelegationEntryResponse {
    redelegation_entry?: RedelegationEntry;
    balance: string;
}
export interface RedelegationResponse {
    redelegation?: Redelegation;
    entries: RedelegationEntryResponse[];
}
export interface Pool {
    not_bonded_tokens: string;
    bonded_tokens: string;
}
