import type { GeneratedType } from "@cosmjs/proto-signing";
import type { MsgBeginRedelegateResponse as MsgBeginRedelegateResponse_type, MsgBeginRedelegate as MsgBeginRedelegate_type, MsgCreateValidatorResponse as MsgCreateValidatorResponse_type, MsgCreateValidator as MsgCreateValidator_type, MsgDelegateResponse as MsgDelegateResponse_type, MsgDelegate as MsgDelegate_type, MsgEditValidatorResponse as MsgEditValidatorResponse_type, MsgEditValidator as MsgEditValidator_type, MsgUndelegateResponse as MsgUndelegateResponse_type, MsgUndelegate as MsgUndelegate_type } from "../../../../types/cosmos/staking/v1beta1";
import type { MessageFns } from "../../../common";
export interface MsgCreateValidator extends MsgCreateValidator_type {
}
export interface MsgCreateValidatorResponse extends MsgCreateValidatorResponse_type {
}
export interface MsgEditValidator extends MsgEditValidator_type {
}
export interface MsgEditValidatorResponse extends MsgEditValidatorResponse_type {
}
export interface MsgDelegate extends MsgDelegate_type {
}
export interface MsgDelegateResponse extends MsgDelegateResponse_type {
}
export interface MsgBeginRedelegate extends MsgBeginRedelegate_type {
}
export interface MsgBeginRedelegateResponse extends MsgBeginRedelegateResponse_type {
}
export interface MsgUndelegate extends MsgUndelegate_type {
}
export interface MsgUndelegateResponse extends MsgUndelegateResponse_type {
}
export declare const MsgCreateValidator: MessageFns<MsgCreateValidator, "cosmos.staking.v1beta1.MsgCreateValidator">;
export declare const MsgCreateValidatorResponse: MessageFns<MsgCreateValidatorResponse, "cosmos.staking.v1beta1.MsgCreateValidatorResponse">;
export declare const MsgEditValidator: MessageFns<MsgEditValidator, "cosmos.staking.v1beta1.MsgEditValidator">;
export declare const MsgEditValidatorResponse: MessageFns<MsgEditValidatorResponse, "cosmos.staking.v1beta1.MsgEditValidatorResponse">;
export declare const MsgDelegate: MessageFns<MsgDelegate, "cosmos.staking.v1beta1.MsgDelegate">;
export declare const MsgDelegateResponse: MessageFns<MsgDelegateResponse, "cosmos.staking.v1beta1.MsgDelegateResponse">;
export declare const MsgBeginRedelegate: MessageFns<MsgBeginRedelegate, "cosmos.staking.v1beta1.MsgBeginRedelegate">;
export declare const MsgBeginRedelegateResponse: MessageFns<MsgBeginRedelegateResponse, "cosmos.staking.v1beta1.MsgBeginRedelegateResponse">;
export declare const MsgUndelegate: MessageFns<MsgUndelegate, "cosmos.staking.v1beta1.MsgUndelegate">;
export declare const MsgUndelegateResponse: MessageFns<MsgUndelegateResponse, "cosmos.staking.v1beta1.MsgUndelegateResponse">;
export declare const registry: Array<[string, GeneratedType]>;
export declare const aminoConverters: {
    "/cosmos.staking.v1beta1.MsgCreateValidator": {
        aminoType: string;
        toAmino: (message: MsgCreateValidator) => {
            description?: import("../../../../types/cosmos/staking/v1beta1").Description;
            commission?: import("../../../../types/cosmos/staking/v1beta1").CommissionRates;
            min_self_delegation: string;
            delegator_address: string;
            validator_address: string;
            pubkey?: import("../../../../types/google/protobuf").Any;
            value?: import("../../../../types/cosmos/base/v1beta1").Coin;
        };
        fromAmino: (object: MsgCreateValidator) => {
            description?: import("../../../../types/cosmos/staking/v1beta1").Description;
            commission?: import("../../../../types/cosmos/staking/v1beta1").CommissionRates;
            min_self_delegation: string;
            delegator_address: string;
            validator_address: string;
            pubkey?: import("../../../../types/google/protobuf").Any;
            value?: import("../../../../types/cosmos/base/v1beta1").Coin;
        };
    };
    "/cosmos.staking.v1beta1.MsgCreateValidatorResponse": {
        aminoType: string;
        toAmino: (message: MsgCreateValidatorResponse) => {};
        fromAmino: (object: MsgCreateValidatorResponse) => {};
    };
    "/cosmos.staking.v1beta1.MsgEditValidator": {
        aminoType: string;
        toAmino: (message: MsgEditValidator) => {
            description?: import("../../../../types/cosmos/staking/v1beta1").Description;
            validator_address: string;
            commission_rate: string;
            min_self_delegation: string;
        };
        fromAmino: (object: MsgEditValidator) => {
            description?: import("../../../../types/cosmos/staking/v1beta1").Description;
            validator_address: string;
            commission_rate: string;
            min_self_delegation: string;
        };
    };
    "/cosmos.staking.v1beta1.MsgEditValidatorResponse": {
        aminoType: string;
        toAmino: (message: MsgEditValidatorResponse) => {};
        fromAmino: (object: MsgEditValidatorResponse) => {};
    };
    "/cosmos.staking.v1beta1.MsgDelegate": {
        aminoType: string;
        toAmino: (message: MsgDelegate) => {
            delegator_address: string;
            validator_address: string;
            amount?: import("../../../../types/cosmos/base/v1beta1").Coin;
        };
        fromAmino: (object: MsgDelegate) => {
            delegator_address: string;
            validator_address: string;
            amount?: import("../../../../types/cosmos/base/v1beta1").Coin;
        };
    };
    "/cosmos.staking.v1beta1.MsgDelegateResponse": {
        aminoType: string;
        toAmino: (message: MsgDelegateResponse) => {};
        fromAmino: (object: MsgDelegateResponse) => {};
    };
    "/cosmos.staking.v1beta1.MsgBeginRedelegate": {
        aminoType: string;
        toAmino: (message: MsgBeginRedelegate) => {
            delegator_address: string;
            validator_src_address: string;
            validator_dst_address: string;
            amount?: import("../../../../types/cosmos/base/v1beta1").Coin;
        };
        fromAmino: (object: MsgBeginRedelegate) => {
            delegator_address: string;
            validator_src_address: string;
            validator_dst_address: string;
            amount?: import("../../../../types/cosmos/base/v1beta1").Coin;
        };
    };
    "/cosmos.staking.v1beta1.MsgBeginRedelegateResponse": {
        aminoType: string;
        toAmino: (message: MsgBeginRedelegateResponse) => {
            completion_time?: Date;
        };
        fromAmino: (object: MsgBeginRedelegateResponse) => {
            completion_time?: Date;
        };
    };
    "/cosmos.staking.v1beta1.MsgUndelegate": {
        aminoType: string;
        toAmino: (message: MsgUndelegate) => {
            delegator_address: string;
            validator_address: string;
            amount?: import("../../../../types/cosmos/base/v1beta1").Coin;
        };
        fromAmino: (object: MsgUndelegate) => {
            delegator_address: string;
            validator_address: string;
            amount?: import("../../../../types/cosmos/base/v1beta1").Coin;
        };
    };
    "/cosmos.staking.v1beta1.MsgUndelegateResponse": {
        aminoType: string;
        toAmino: (message: MsgUndelegateResponse) => {
            completion_time?: Date;
        };
        fromAmino: (object: MsgUndelegateResponse) => {
            completion_time?: Date;
        };
    };
};
