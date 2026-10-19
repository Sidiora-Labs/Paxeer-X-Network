import type { GeneratedType } from "@cosmjs/proto-signing";
import type { CommunityPoolSpendProposalWithDeposit as CommunityPoolSpendProposalWithDeposit_type, CommunityPoolSpendProposal as CommunityPoolSpendProposal_type, DelegationDelegatorReward as DelegationDelegatorReward_type, DelegatorStartingInfo as DelegatorStartingInfo_type, FeePool as FeePool_type, Params as Params_type, ValidatorAccumulatedCommission as ValidatorAccumulatedCommission_type, ValidatorCurrentRewards as ValidatorCurrentRewards_type, ValidatorHistoricalRewards as ValidatorHistoricalRewards_type, ValidatorOutstandingRewards as ValidatorOutstandingRewards_type, ValidatorSlashEvent as ValidatorSlashEvent_type, ValidatorSlashEvents as ValidatorSlashEvents_type } from "../../../../types/cosmos/distribution/v1beta1";
import type { MessageFns } from "../../../common";
export interface Params extends Params_type {
}
export interface ValidatorHistoricalRewards extends ValidatorHistoricalRewards_type {
}
export interface ValidatorCurrentRewards extends ValidatorCurrentRewards_type {
}
export interface ValidatorAccumulatedCommission extends ValidatorAccumulatedCommission_type {
}
export interface ValidatorOutstandingRewards extends ValidatorOutstandingRewards_type {
}
export interface ValidatorSlashEvent extends ValidatorSlashEvent_type {
}
export interface ValidatorSlashEvents extends ValidatorSlashEvents_type {
}
export interface FeePool extends FeePool_type {
}
export interface CommunityPoolSpendProposal extends CommunityPoolSpendProposal_type {
}
export interface DelegatorStartingInfo extends DelegatorStartingInfo_type {
}
export interface DelegationDelegatorReward extends DelegationDelegatorReward_type {
}
export interface CommunityPoolSpendProposalWithDeposit extends CommunityPoolSpendProposalWithDeposit_type {
}
export declare const Params: MessageFns<Params, "cosmos.distribution.v1beta1.Params">;
export declare const ValidatorHistoricalRewards: MessageFns<ValidatorHistoricalRewards, "cosmos.distribution.v1beta1.ValidatorHistoricalRewards">;
export declare const ValidatorCurrentRewards: MessageFns<ValidatorCurrentRewards, "cosmos.distribution.v1beta1.ValidatorCurrentRewards">;
export declare const ValidatorAccumulatedCommission: MessageFns<ValidatorAccumulatedCommission, "cosmos.distribution.v1beta1.ValidatorAccumulatedCommission">;
export declare const ValidatorOutstandingRewards: MessageFns<ValidatorOutstandingRewards, "cosmos.distribution.v1beta1.ValidatorOutstandingRewards">;
export declare const ValidatorSlashEvent: MessageFns<ValidatorSlashEvent, "cosmos.distribution.v1beta1.ValidatorSlashEvent">;
export declare const ValidatorSlashEvents: MessageFns<ValidatorSlashEvents, "cosmos.distribution.v1beta1.ValidatorSlashEvents">;
export declare const FeePool: MessageFns<FeePool, "cosmos.distribution.v1beta1.FeePool">;
export declare const CommunityPoolSpendProposal: MessageFns<CommunityPoolSpendProposal, "cosmos.distribution.v1beta1.CommunityPoolSpendProposal">;
export declare const DelegatorStartingInfo: MessageFns<DelegatorStartingInfo, "cosmos.distribution.v1beta1.DelegatorStartingInfo">;
export declare const DelegationDelegatorReward: MessageFns<DelegationDelegatorReward, "cosmos.distribution.v1beta1.DelegationDelegatorReward">;
export declare const CommunityPoolSpendProposalWithDeposit: MessageFns<CommunityPoolSpendProposalWithDeposit, "cosmos.distribution.v1beta1.CommunityPoolSpendProposalWithDeposit">;
export declare const registry: Array<[string, GeneratedType]>;
export declare const aminoConverters: {
    "/cosmos.distribution.v1beta1.Params": {
        aminoType: string;
        toAmino: (message: Params) => {
            community_tax: string;
            base_proposer_reward: string;
            bonus_proposer_reward: string;
            withdraw_addr_enabled: boolean;
        };
        fromAmino: (object: Params) => {
            community_tax: string;
            base_proposer_reward: string;
            bonus_proposer_reward: string;
            withdraw_addr_enabled: boolean;
        };
    };
    "/cosmos.distribution.v1beta1.ValidatorHistoricalRewards": {
        aminoType: string;
        toAmino: (message: ValidatorHistoricalRewards) => {
            cumulative_reward_ratio: import("../../../../types/cosmos/base/v1beta1").DecCoin[];
            reference_count: number;
        };
        fromAmino: (object: ValidatorHistoricalRewards) => {
            cumulative_reward_ratio: import("../../../../types/cosmos/base/v1beta1").DecCoin[];
            reference_count: number;
        };
    };
    "/cosmos.distribution.v1beta1.ValidatorCurrentRewards": {
        aminoType: string;
        toAmino: (message: ValidatorCurrentRewards) => {
            rewards: import("../../../../types/cosmos/base/v1beta1").DecCoin[];
            period: number;
        };
        fromAmino: (object: ValidatorCurrentRewards) => {
            rewards: import("../../../../types/cosmos/base/v1beta1").DecCoin[];
            period: number;
        };
    };
    "/cosmos.distribution.v1beta1.ValidatorAccumulatedCommission": {
        aminoType: string;
        toAmino: (message: ValidatorAccumulatedCommission) => {
            commission: import("../../../../types/cosmos/base/v1beta1").DecCoin[];
        };
        fromAmino: (object: ValidatorAccumulatedCommission) => {
            commission: import("../../../../types/cosmos/base/v1beta1").DecCoin[];
        };
    };
    "/cosmos.distribution.v1beta1.ValidatorOutstandingRewards": {
        aminoType: string;
        toAmino: (message: ValidatorOutstandingRewards) => {
            rewards: import("../../../../types/cosmos/base/v1beta1").DecCoin[];
        };
        fromAmino: (object: ValidatorOutstandingRewards) => {
            rewards: import("../../../../types/cosmos/base/v1beta1").DecCoin[];
        };
    };
    "/cosmos.distribution.v1beta1.ValidatorSlashEvent": {
        aminoType: string;
        toAmino: (message: ValidatorSlashEvent) => {
            validator_period: number;
            fraction: string;
        };
        fromAmino: (object: ValidatorSlashEvent) => {
            validator_period: number;
            fraction: string;
        };
    };
    "/cosmos.distribution.v1beta1.ValidatorSlashEvents": {
        aminoType: string;
        toAmino: (message: ValidatorSlashEvents) => {
            validator_slash_events: ValidatorSlashEvent_type[];
        };
        fromAmino: (object: ValidatorSlashEvents) => {
            validator_slash_events: ValidatorSlashEvent_type[];
        };
    };
    "/cosmos.distribution.v1beta1.FeePool": {
        aminoType: string;
        toAmino: (message: FeePool) => {
            community_pool: import("../../../../types/cosmos/base/v1beta1").DecCoin[];
        };
        fromAmino: (object: FeePool) => {
            community_pool: import("../../../../types/cosmos/base/v1beta1").DecCoin[];
        };
    };
    "/cosmos.distribution.v1beta1.CommunityPoolSpendProposal": {
        aminoType: string;
        toAmino: (message: CommunityPoolSpendProposal) => {
            title: string;
            description: string;
            recipient: string;
            amount: import("../../../../types/cosmos/base/v1beta1").Coin[];
        };
        fromAmino: (object: CommunityPoolSpendProposal) => {
            title: string;
            description: string;
            recipient: string;
            amount: import("../../../../types/cosmos/base/v1beta1").Coin[];
        };
    };
    "/cosmos.distribution.v1beta1.DelegatorStartingInfo": {
        aminoType: string;
        toAmino: (message: DelegatorStartingInfo) => {
            previous_period: number;
            stake: string;
            height: number;
        };
        fromAmino: (object: DelegatorStartingInfo) => {
            previous_period: number;
            stake: string;
            height: number;
        };
    };
    "/cosmos.distribution.v1beta1.DelegationDelegatorReward": {
        aminoType: string;
        toAmino: (message: DelegationDelegatorReward) => {
            validator_address: string;
            reward: import("../../../../types/cosmos/base/v1beta1").DecCoin[];
        };
        fromAmino: (object: DelegationDelegatorReward) => {
            validator_address: string;
            reward: import("../../../../types/cosmos/base/v1beta1").DecCoin[];
        };
    };
    "/cosmos.distribution.v1beta1.CommunityPoolSpendProposalWithDeposit": {
        aminoType: string;
        toAmino: (message: CommunityPoolSpendProposalWithDeposit) => {
            title: string;
            description: string;
            recipient: string;
            amount: string;
            deposit: string;
        };
        fromAmino: (object: CommunityPoolSpendProposalWithDeposit) => {
            title: string;
            description: string;
            recipient: string;
            amount: string;
            deposit: string;
        };
    };
};
