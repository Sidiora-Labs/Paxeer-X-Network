import type { GeneratedType } from "@cosmjs/proto-signing";
import type { MsgFundCommunityPoolResponse as MsgFundCommunityPoolResponse_type, MsgFundCommunityPool as MsgFundCommunityPool_type, MsgSetWithdrawAddressResponse as MsgSetWithdrawAddressResponse_type, MsgSetWithdrawAddress as MsgSetWithdrawAddress_type, MsgWithdrawDelegatorRewardResponse as MsgWithdrawDelegatorRewardResponse_type, MsgWithdrawDelegatorReward as MsgWithdrawDelegatorReward_type, MsgWithdrawValidatorCommissionResponse as MsgWithdrawValidatorCommissionResponse_type, MsgWithdrawValidatorCommission as MsgWithdrawValidatorCommission_type } from "../../../../types/cosmos/distribution/v1beta1";
import type { MessageFns } from "../../../common";
export interface MsgSetWithdrawAddress extends MsgSetWithdrawAddress_type {
}
export interface MsgSetWithdrawAddressResponse extends MsgSetWithdrawAddressResponse_type {
}
export interface MsgWithdrawDelegatorReward extends MsgWithdrawDelegatorReward_type {
}
export interface MsgWithdrawDelegatorRewardResponse extends MsgWithdrawDelegatorRewardResponse_type {
}
export interface MsgWithdrawValidatorCommission extends MsgWithdrawValidatorCommission_type {
}
export interface MsgWithdrawValidatorCommissionResponse extends MsgWithdrawValidatorCommissionResponse_type {
}
export interface MsgFundCommunityPool extends MsgFundCommunityPool_type {
}
export interface MsgFundCommunityPoolResponse extends MsgFundCommunityPoolResponse_type {
}
export declare const MsgSetWithdrawAddress: MessageFns<MsgSetWithdrawAddress, "cosmos.distribution.v1beta1.MsgSetWithdrawAddress">;
export declare const MsgSetWithdrawAddressResponse: MessageFns<MsgSetWithdrawAddressResponse, "cosmos.distribution.v1beta1.MsgSetWithdrawAddressResponse">;
export declare const MsgWithdrawDelegatorReward: MessageFns<MsgWithdrawDelegatorReward, "cosmos.distribution.v1beta1.MsgWithdrawDelegatorReward">;
export declare const MsgWithdrawDelegatorRewardResponse: MessageFns<MsgWithdrawDelegatorRewardResponse, "cosmos.distribution.v1beta1.MsgWithdrawDelegatorRewardResponse">;
export declare const MsgWithdrawValidatorCommission: MessageFns<MsgWithdrawValidatorCommission, "cosmos.distribution.v1beta1.MsgWithdrawValidatorCommission">;
export declare const MsgWithdrawValidatorCommissionResponse: MessageFns<MsgWithdrawValidatorCommissionResponse, "cosmos.distribution.v1beta1.MsgWithdrawValidatorCommissionResponse">;
export declare const MsgFundCommunityPool: MessageFns<MsgFundCommunityPool, "cosmos.distribution.v1beta1.MsgFundCommunityPool">;
export declare const MsgFundCommunityPoolResponse: MessageFns<MsgFundCommunityPoolResponse, "cosmos.distribution.v1beta1.MsgFundCommunityPoolResponse">;
export declare const registry: Array<[string, GeneratedType]>;
export declare const aminoConverters: {
    "/cosmos.distribution.v1beta1.MsgSetWithdrawAddress": {
        aminoType: string;
        toAmino: (message: MsgSetWithdrawAddress) => {
            delegator_address: string;
            withdraw_address: string;
        };
        fromAmino: (object: MsgSetWithdrawAddress) => {
            delegator_address: string;
            withdraw_address: string;
        };
    };
    "/cosmos.distribution.v1beta1.MsgSetWithdrawAddressResponse": {
        aminoType: string;
        toAmino: (message: MsgSetWithdrawAddressResponse) => {};
        fromAmino: (object: MsgSetWithdrawAddressResponse) => {};
    };
    "/cosmos.distribution.v1beta1.MsgWithdrawDelegatorReward": {
        aminoType: string;
        toAmino: (message: MsgWithdrawDelegatorReward) => {
            delegator_address: string;
            validator_address: string;
        };
        fromAmino: (object: MsgWithdrawDelegatorReward) => {
            delegator_address: string;
            validator_address: string;
        };
    };
    "/cosmos.distribution.v1beta1.MsgWithdrawDelegatorRewardResponse": {
        aminoType: string;
        toAmino: (message: MsgWithdrawDelegatorRewardResponse) => {};
        fromAmino: (object: MsgWithdrawDelegatorRewardResponse) => {};
    };
    "/cosmos.distribution.v1beta1.MsgWithdrawValidatorCommission": {
        aminoType: string;
        toAmino: (message: MsgWithdrawValidatorCommission) => {
            validator_address: string;
        };
        fromAmino: (object: MsgWithdrawValidatorCommission) => {
            validator_address: string;
        };
    };
    "/cosmos.distribution.v1beta1.MsgWithdrawValidatorCommissionResponse": {
        aminoType: string;
        toAmino: (message: MsgWithdrawValidatorCommissionResponse) => {};
        fromAmino: (object: MsgWithdrawValidatorCommissionResponse) => {};
    };
    "/cosmos.distribution.v1beta1.MsgFundCommunityPool": {
        aminoType: string;
        toAmino: (message: MsgFundCommunityPool) => {
            amount: import("../../../../types/cosmos/base/v1beta1").Coin[];
            depositor: string;
        };
        fromAmino: (object: MsgFundCommunityPool) => {
            amount: import("../../../../types/cosmos/base/v1beta1").Coin[];
            depositor: string;
        };
    };
    "/cosmos.distribution.v1beta1.MsgFundCommunityPoolResponse": {
        aminoType: string;
        toAmino: (message: MsgFundCommunityPoolResponse) => {};
        fromAmino: (object: MsgFundCommunityPoolResponse) => {};
    };
};
