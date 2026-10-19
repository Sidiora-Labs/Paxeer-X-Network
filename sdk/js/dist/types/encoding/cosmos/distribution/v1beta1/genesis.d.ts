import type { GeneratedType } from "@cosmjs/proto-signing";
import type { DelegatorStartingInfoRecord as DelegatorStartingInfoRecord_type, DelegatorWithdrawInfo as DelegatorWithdrawInfo_type, GenesisState as GenesisState_type, ValidatorAccumulatedCommissionRecord as ValidatorAccumulatedCommissionRecord_type, ValidatorCurrentRewardsRecord as ValidatorCurrentRewardsRecord_type, ValidatorHistoricalRewardsRecord as ValidatorHistoricalRewardsRecord_type, ValidatorOutstandingRewardsRecord as ValidatorOutstandingRewardsRecord_type, ValidatorSlashEventRecord as ValidatorSlashEventRecord_type } from "../../../../types/cosmos/distribution/v1beta1";
import type { MessageFns } from "../../../common";
export interface DelegatorWithdrawInfo extends DelegatorWithdrawInfo_type {
}
export interface ValidatorOutstandingRewardsRecord extends ValidatorOutstandingRewardsRecord_type {
}
export interface ValidatorAccumulatedCommissionRecord extends ValidatorAccumulatedCommissionRecord_type {
}
export interface ValidatorHistoricalRewardsRecord extends ValidatorHistoricalRewardsRecord_type {
}
export interface ValidatorCurrentRewardsRecord extends ValidatorCurrentRewardsRecord_type {
}
export interface DelegatorStartingInfoRecord extends DelegatorStartingInfoRecord_type {
}
export interface ValidatorSlashEventRecord extends ValidatorSlashEventRecord_type {
}
export interface GenesisState extends GenesisState_type {
}
export declare const DelegatorWithdrawInfo: MessageFns<DelegatorWithdrawInfo, "cosmos.distribution.v1beta1.DelegatorWithdrawInfo">;
export declare const ValidatorOutstandingRewardsRecord: MessageFns<ValidatorOutstandingRewardsRecord, "cosmos.distribution.v1beta1.ValidatorOutstandingRewardsRecord">;
export declare const ValidatorAccumulatedCommissionRecord: MessageFns<ValidatorAccumulatedCommissionRecord, "cosmos.distribution.v1beta1.ValidatorAccumulatedCommissionRecord">;
export declare const ValidatorHistoricalRewardsRecord: MessageFns<ValidatorHistoricalRewardsRecord, "cosmos.distribution.v1beta1.ValidatorHistoricalRewardsRecord">;
export declare const ValidatorCurrentRewardsRecord: MessageFns<ValidatorCurrentRewardsRecord, "cosmos.distribution.v1beta1.ValidatorCurrentRewardsRecord">;
export declare const DelegatorStartingInfoRecord: MessageFns<DelegatorStartingInfoRecord, "cosmos.distribution.v1beta1.DelegatorStartingInfoRecord">;
export declare const ValidatorSlashEventRecord: MessageFns<ValidatorSlashEventRecord, "cosmos.distribution.v1beta1.ValidatorSlashEventRecord">;
export declare const GenesisState: MessageFns<GenesisState, "cosmos.distribution.v1beta1.GenesisState">;
export declare const registry: Array<[string, GeneratedType]>;
export declare const aminoConverters: {
    "/cosmos.distribution.v1beta1.DelegatorWithdrawInfo": {
        aminoType: string;
        toAmino: (message: DelegatorWithdrawInfo) => {
            delegator_address: string;
            withdraw_address: string;
        };
        fromAmino: (object: DelegatorWithdrawInfo) => {
            delegator_address: string;
            withdraw_address: string;
        };
    };
    "/cosmos.distribution.v1beta1.ValidatorOutstandingRewardsRecord": {
        aminoType: string;
        toAmino: (message: ValidatorOutstandingRewardsRecord) => {
            validator_address: string;
            outstanding_rewards: import("../../../../types/cosmos/base/v1beta1").DecCoin[];
        };
        fromAmino: (object: ValidatorOutstandingRewardsRecord) => {
            validator_address: string;
            outstanding_rewards: import("../../../../types/cosmos/base/v1beta1").DecCoin[];
        };
    };
    "/cosmos.distribution.v1beta1.ValidatorAccumulatedCommissionRecord": {
        aminoType: string;
        toAmino: (message: ValidatorAccumulatedCommissionRecord) => {
            validator_address: string;
            accumulated?: import("../../../../types/cosmos/distribution/v1beta1").ValidatorAccumulatedCommission;
        };
        fromAmino: (object: ValidatorAccumulatedCommissionRecord) => {
            validator_address: string;
            accumulated?: import("../../../../types/cosmos/distribution/v1beta1").ValidatorAccumulatedCommission;
        };
    };
    "/cosmos.distribution.v1beta1.ValidatorHistoricalRewardsRecord": {
        aminoType: string;
        toAmino: (message: ValidatorHistoricalRewardsRecord) => {
            validator_address: string;
            period: number;
            rewards?: import("../../../../types/cosmos/distribution/v1beta1").ValidatorHistoricalRewards;
        };
        fromAmino: (object: ValidatorHistoricalRewardsRecord) => {
            validator_address: string;
            period: number;
            rewards?: import("../../../../types/cosmos/distribution/v1beta1").ValidatorHistoricalRewards;
        };
    };
    "/cosmos.distribution.v1beta1.ValidatorCurrentRewardsRecord": {
        aminoType: string;
        toAmino: (message: ValidatorCurrentRewardsRecord) => {
            validator_address: string;
            rewards?: import("../../../../types/cosmos/distribution/v1beta1").ValidatorCurrentRewards;
        };
        fromAmino: (object: ValidatorCurrentRewardsRecord) => {
            validator_address: string;
            rewards?: import("../../../../types/cosmos/distribution/v1beta1").ValidatorCurrentRewards;
        };
    };
    "/cosmos.distribution.v1beta1.DelegatorStartingInfoRecord": {
        aminoType: string;
        toAmino: (message: DelegatorStartingInfoRecord) => {
            delegator_address: string;
            validator_address: string;
            starting_info?: import("../../../../types/cosmos/distribution/v1beta1").DelegatorStartingInfo;
        };
        fromAmino: (object: DelegatorStartingInfoRecord) => {
            delegator_address: string;
            validator_address: string;
            starting_info?: import("../../../../types/cosmos/distribution/v1beta1").DelegatorStartingInfo;
        };
    };
    "/cosmos.distribution.v1beta1.ValidatorSlashEventRecord": {
        aminoType: string;
        toAmino: (message: ValidatorSlashEventRecord) => {
            validator_address: string;
            height: number;
            period: number;
            validator_slash_event?: import("../../../../types/cosmos/distribution/v1beta1").ValidatorSlashEvent;
        };
        fromAmino: (object: ValidatorSlashEventRecord) => {
            validator_address: string;
            height: number;
            period: number;
            validator_slash_event?: import("../../../../types/cosmos/distribution/v1beta1").ValidatorSlashEvent;
        };
    };
    "/cosmos.distribution.v1beta1.GenesisState": {
        aminoType: string;
        toAmino: (message: GenesisState) => {
            params?: import("../../../../types/cosmos/distribution/v1beta1").Params;
            fee_pool?: import("../../../../types/cosmos/distribution/v1beta1").FeePool;
            delegator_withdraw_infos: DelegatorWithdrawInfo_type[];
            previous_proposer: string;
            outstanding_rewards: ValidatorOutstandingRewardsRecord_type[];
            validator_accumulated_commissions: ValidatorAccumulatedCommissionRecord_type[];
            validator_historical_rewards: ValidatorHistoricalRewardsRecord_type[];
            validator_current_rewards: ValidatorCurrentRewardsRecord_type[];
            delegator_starting_infos: DelegatorStartingInfoRecord_type[];
            validator_slash_events: ValidatorSlashEventRecord_type[];
        };
        fromAmino: (object: GenesisState) => {
            params?: import("../../../../types/cosmos/distribution/v1beta1").Params;
            fee_pool?: import("../../../../types/cosmos/distribution/v1beta1").FeePool;
            delegator_withdraw_infos: DelegatorWithdrawInfo_type[];
            previous_proposer: string;
            outstanding_rewards: ValidatorOutstandingRewardsRecord_type[];
            validator_accumulated_commissions: ValidatorAccumulatedCommissionRecord_type[];
            validator_historical_rewards: ValidatorHistoricalRewardsRecord_type[];
            validator_current_rewards: ValidatorCurrentRewardsRecord_type[];
            delegator_starting_infos: DelegatorStartingInfoRecord_type[];
            validator_slash_events: ValidatorSlashEventRecord_type[];
        };
    };
};
