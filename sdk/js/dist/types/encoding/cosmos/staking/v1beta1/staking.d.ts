import type { GeneratedType } from "@cosmjs/proto-signing";
import type { CommissionRates as CommissionRates_type, Commission as Commission_type, DVPair as DVPair_type, DVPairs as DVPairs_type, DVVTriplet as DVVTriplet_type, DVVTriplets as DVVTriplets_type, DelegationResponse as DelegationResponse_type, Delegation as Delegation_type, Description as Description_type, HistoricalInfo as HistoricalInfo_type, Params as Params_type, Pool as Pool_type, RedelegationEntryResponse as RedelegationEntryResponse_type, RedelegationEntry as RedelegationEntry_type, RedelegationResponse as RedelegationResponse_type, Redelegation as Redelegation_type, UnbondingDelegationEntry as UnbondingDelegationEntry_type, UnbondingDelegation as UnbondingDelegation_type, ValAddresses as ValAddresses_type, Validator as Validator_type } from "../../../../types/cosmos/staking/v1beta1";
import { BondStatus } from "../../../../types/cosmos/staking/v1beta1";
import type { MessageFns } from "../../../common";
export interface HistoricalInfo extends HistoricalInfo_type {
}
export interface CommissionRates extends CommissionRates_type {
}
export interface Commission extends Commission_type {
}
export interface Description extends Description_type {
}
export interface Validator extends Validator_type {
}
export interface ValAddresses extends ValAddresses_type {
}
export interface DVPair extends DVPair_type {
}
export interface DVPairs extends DVPairs_type {
}
export interface DVVTriplet extends DVVTriplet_type {
}
export interface DVVTriplets extends DVVTriplets_type {
}
export interface Delegation extends Delegation_type {
}
export interface UnbondingDelegation extends UnbondingDelegation_type {
}
export interface UnbondingDelegationEntry extends UnbondingDelegationEntry_type {
}
export interface RedelegationEntry extends RedelegationEntry_type {
}
export interface Redelegation extends Redelegation_type {
}
export interface Params extends Params_type {
}
export interface DelegationResponse extends DelegationResponse_type {
}
export interface RedelegationEntryResponse extends RedelegationEntryResponse_type {
}
export interface RedelegationResponse extends RedelegationResponse_type {
}
export interface Pool extends Pool_type {
}
export declare const HistoricalInfo: MessageFns<HistoricalInfo, "cosmos.staking.v1beta1.HistoricalInfo">;
export declare const CommissionRates: MessageFns<CommissionRates, "cosmos.staking.v1beta1.CommissionRates">;
export declare const Commission: MessageFns<Commission, "cosmos.staking.v1beta1.Commission">;
export declare const Description: MessageFns<Description, "cosmos.staking.v1beta1.Description">;
export declare const Validator: MessageFns<Validator, "cosmos.staking.v1beta1.Validator">;
export declare const ValAddresses: MessageFns<ValAddresses, "cosmos.staking.v1beta1.ValAddresses">;
export declare const DVPair: MessageFns<DVPair, "cosmos.staking.v1beta1.DVPair">;
export declare const DVPairs: MessageFns<DVPairs, "cosmos.staking.v1beta1.DVPairs">;
export declare const DVVTriplet: MessageFns<DVVTriplet, "cosmos.staking.v1beta1.DVVTriplet">;
export declare const DVVTriplets: MessageFns<DVVTriplets, "cosmos.staking.v1beta1.DVVTriplets">;
export declare const Delegation: MessageFns<Delegation, "cosmos.staking.v1beta1.Delegation">;
export declare const UnbondingDelegation: MessageFns<UnbondingDelegation, "cosmos.staking.v1beta1.UnbondingDelegation">;
export declare const UnbondingDelegationEntry: MessageFns<UnbondingDelegationEntry, "cosmos.staking.v1beta1.UnbondingDelegationEntry">;
export declare const RedelegationEntry: MessageFns<RedelegationEntry, "cosmos.staking.v1beta1.RedelegationEntry">;
export declare const Redelegation: MessageFns<Redelegation, "cosmos.staking.v1beta1.Redelegation">;
export declare const Params: MessageFns<Params, "cosmos.staking.v1beta1.Params">;
export declare const DelegationResponse: MessageFns<DelegationResponse, "cosmos.staking.v1beta1.DelegationResponse">;
export declare const RedelegationEntryResponse: MessageFns<RedelegationEntryResponse, "cosmos.staking.v1beta1.RedelegationEntryResponse">;
export declare const RedelegationResponse: MessageFns<RedelegationResponse, "cosmos.staking.v1beta1.RedelegationResponse">;
export declare const Pool: MessageFns<Pool, "cosmos.staking.v1beta1.Pool">;
export declare function bondStatusFromJSON(object: any): BondStatus;
export declare function bondStatusToJSON(object: BondStatus): string;
export declare const registry: Array<[string, GeneratedType]>;
export declare const aminoConverters: {
    "/cosmos.staking.v1beta1.HistoricalInfo": {
        aminoType: string;
        toAmino: (message: HistoricalInfo) => {
            header?: import("../../../../types/tendermint/types").Header;
            valset: Validator_type[];
        };
        fromAmino: (object: HistoricalInfo) => {
            header?: import("../../../../types/tendermint/types").Header;
            valset: Validator_type[];
        };
    };
    "/cosmos.staking.v1beta1.CommissionRates": {
        aminoType: string;
        toAmino: (message: CommissionRates) => {
            rate: string;
            max_rate: string;
            max_change_rate: string;
        };
        fromAmino: (object: CommissionRates) => {
            rate: string;
            max_rate: string;
            max_change_rate: string;
        };
    };
    "/cosmos.staking.v1beta1.Commission": {
        aminoType: string;
        toAmino: (message: Commission) => {
            commission_rates?: CommissionRates_type;
            update_time?: Date;
        };
        fromAmino: (object: Commission) => {
            commission_rates?: CommissionRates_type;
            update_time?: Date;
        };
    };
    "/cosmos.staking.v1beta1.Description": {
        aminoType: string;
        toAmino: (message: Description) => {
            moniker: string;
            identity: string;
            website: string;
            security_contact: string;
            details: string;
        };
        fromAmino: (object: Description) => {
            moniker: string;
            identity: string;
            website: string;
            security_contact: string;
            details: string;
        };
    };
    "/cosmos.staking.v1beta1.Validator": {
        aminoType: string;
        toAmino: (message: Validator) => {
            operator_address: string;
            consensus_pubkey?: import("../../../../types/google/protobuf").Any;
            jailed: boolean;
            status: BondStatus;
            tokens: string;
            delegator_shares: string;
            description?: Description_type;
            unbonding_height: number;
            unbonding_time?: Date;
            commission?: Commission_type;
            min_self_delegation: string;
        };
        fromAmino: (object: Validator) => {
            operator_address: string;
            consensus_pubkey?: import("../../../../types/google/protobuf").Any;
            jailed: boolean;
            status: BondStatus;
            tokens: string;
            delegator_shares: string;
            description?: Description_type;
            unbonding_height: number;
            unbonding_time?: Date;
            commission?: Commission_type;
            min_self_delegation: string;
        };
    };
    "/cosmos.staking.v1beta1.ValAddresses": {
        aminoType: string;
        toAmino: (message: ValAddresses) => {
            addresses: string[];
        };
        fromAmino: (object: ValAddresses) => {
            addresses: string[];
        };
    };
    "/cosmos.staking.v1beta1.DVPair": {
        aminoType: string;
        toAmino: (message: DVPair) => {
            delegator_address: string;
            validator_address: string;
        };
        fromAmino: (object: DVPair) => {
            delegator_address: string;
            validator_address: string;
        };
    };
    "/cosmos.staking.v1beta1.DVPairs": {
        aminoType: string;
        toAmino: (message: DVPairs) => {
            pairs: DVPair_type[];
        };
        fromAmino: (object: DVPairs) => {
            pairs: DVPair_type[];
        };
    };
    "/cosmos.staking.v1beta1.DVVTriplet": {
        aminoType: string;
        toAmino: (message: DVVTriplet) => {
            delegator_address: string;
            validator_src_address: string;
            validator_dst_address: string;
        };
        fromAmino: (object: DVVTriplet) => {
            delegator_address: string;
            validator_src_address: string;
            validator_dst_address: string;
        };
    };
    "/cosmos.staking.v1beta1.DVVTriplets": {
        aminoType: string;
        toAmino: (message: DVVTriplets) => {
            triplets: DVVTriplet_type[];
        };
        fromAmino: (object: DVVTriplets) => {
            triplets: DVVTriplet_type[];
        };
    };
    "/cosmos.staking.v1beta1.Delegation": {
        aminoType: string;
        toAmino: (message: Delegation) => {
            delegator_address: string;
            validator_address: string;
            shares: string;
        };
        fromAmino: (object: Delegation) => {
            delegator_address: string;
            validator_address: string;
            shares: string;
        };
    };
    "/cosmos.staking.v1beta1.UnbondingDelegation": {
        aminoType: string;
        toAmino: (message: UnbondingDelegation) => {
            delegator_address: string;
            validator_address: string;
            entries: UnbondingDelegationEntry_type[];
        };
        fromAmino: (object: UnbondingDelegation) => {
            delegator_address: string;
            validator_address: string;
            entries: UnbondingDelegationEntry_type[];
        };
    };
    "/cosmos.staking.v1beta1.UnbondingDelegationEntry": {
        aminoType: string;
        toAmino: (message: UnbondingDelegationEntry) => {
            creation_height: number;
            completion_time?: Date;
            initial_balance: string;
            balance: string;
        };
        fromAmino: (object: UnbondingDelegationEntry) => {
            creation_height: number;
            completion_time?: Date;
            initial_balance: string;
            balance: string;
        };
    };
    "/cosmos.staking.v1beta1.RedelegationEntry": {
        aminoType: string;
        toAmino: (message: RedelegationEntry) => {
            creation_height: number;
            completion_time?: Date;
            initial_balance: string;
            shares_dst: string;
        };
        fromAmino: (object: RedelegationEntry) => {
            creation_height: number;
            completion_time?: Date;
            initial_balance: string;
            shares_dst: string;
        };
    };
    "/cosmos.staking.v1beta1.Redelegation": {
        aminoType: string;
        toAmino: (message: Redelegation) => {
            delegator_address: string;
            validator_src_address: string;
            validator_dst_address: string;
            entries: RedelegationEntry_type[];
        };
        fromAmino: (object: Redelegation) => {
            delegator_address: string;
            validator_src_address: string;
            validator_dst_address: string;
            entries: RedelegationEntry_type[];
        };
    };
    "/cosmos.staking.v1beta1.Params": {
        aminoType: string;
        toAmino: (message: Params) => {
            unbonding_time?: import("../../../../types/google/protobuf").Duration;
            max_validators: number;
            max_entries: number;
            historical_entries: number;
            bond_denom: string;
            min_commission_rate: string;
            max_voting_power_ratio: string;
            max_voting_power_enforcement_threshold: string;
        };
        fromAmino: (object: Params) => {
            unbonding_time?: import("../../../../types/google/protobuf").Duration;
            max_validators: number;
            max_entries: number;
            historical_entries: number;
            bond_denom: string;
            min_commission_rate: string;
            max_voting_power_ratio: string;
            max_voting_power_enforcement_threshold: string;
        };
    };
    "/cosmos.staking.v1beta1.DelegationResponse": {
        aminoType: string;
        toAmino: (message: DelegationResponse) => {
            delegation?: Delegation_type;
            balance?: import("../../../../types/cosmos/base/v1beta1").Coin;
        };
        fromAmino: (object: DelegationResponse) => {
            delegation?: Delegation_type;
            balance?: import("../../../../types/cosmos/base/v1beta1").Coin;
        };
    };
    "/cosmos.staking.v1beta1.RedelegationEntryResponse": {
        aminoType: string;
        toAmino: (message: RedelegationEntryResponse) => {
            redelegation_entry?: RedelegationEntry_type;
            balance: string;
        };
        fromAmino: (object: RedelegationEntryResponse) => {
            redelegation_entry?: RedelegationEntry_type;
            balance: string;
        };
    };
    "/cosmos.staking.v1beta1.RedelegationResponse": {
        aminoType: string;
        toAmino: (message: RedelegationResponse) => {
            redelegation?: Redelegation_type;
            entries: RedelegationEntryResponse_type[];
        };
        fromAmino: (object: RedelegationResponse) => {
            redelegation?: Redelegation_type;
            entries: RedelegationEntryResponse_type[];
        };
    };
    "/cosmos.staking.v1beta1.Pool": {
        aminoType: string;
        toAmino: (message: Pool) => {
            not_bonded_tokens: string;
            bonded_tokens: string;
        };
        fromAmino: (object: Pool) => {
            not_bonded_tokens: string;
            bonded_tokens: string;
        };
    };
};
