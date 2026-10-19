import type { GeneratedType } from "@cosmjs/proto-signing";
import type { AllowedMsgAllowance as AllowedMsgAllowance_type, BasicAllowance as BasicAllowance_type, Grant as Grant_type, PeriodicAllowance as PeriodicAllowance_type } from "../../../../types/cosmos/feegrant/v1beta1";
import type { MessageFns } from "../../../common";
export interface BasicAllowance extends BasicAllowance_type {
}
export interface PeriodicAllowance extends PeriodicAllowance_type {
}
export interface AllowedMsgAllowance extends AllowedMsgAllowance_type {
}
export interface Grant extends Grant_type {
}
export declare const BasicAllowance: MessageFns<BasicAllowance, "cosmos.feegrant.v1beta1.BasicAllowance">;
export declare const PeriodicAllowance: MessageFns<PeriodicAllowance, "cosmos.feegrant.v1beta1.PeriodicAllowance">;
export declare const AllowedMsgAllowance: MessageFns<AllowedMsgAllowance, "cosmos.feegrant.v1beta1.AllowedMsgAllowance">;
export declare const Grant: MessageFns<Grant, "cosmos.feegrant.v1beta1.Grant">;
export declare const registry: Array<[string, GeneratedType]>;
export declare const aminoConverters: {
    "/cosmos.feegrant.v1beta1.BasicAllowance": {
        aminoType: string;
        toAmino: (message: BasicAllowance) => {
            spend_limit: import("../../../../types/cosmos/base/v1beta1").Coin[];
            expiration?: Date;
        };
        fromAmino: (object: BasicAllowance) => {
            spend_limit: import("../../../../types/cosmos/base/v1beta1").Coin[];
            expiration?: Date;
        };
    };
    "/cosmos.feegrant.v1beta1.PeriodicAllowance": {
        aminoType: string;
        toAmino: (message: PeriodicAllowance) => {
            basic?: BasicAllowance_type;
            period?: import("../../../../types/google/protobuf").Duration;
            period_spend_limit: import("../../../../types/cosmos/base/v1beta1").Coin[];
            period_can_spend: import("../../../../types/cosmos/base/v1beta1").Coin[];
            period_reset?: Date;
        };
        fromAmino: (object: PeriodicAllowance) => {
            basic?: BasicAllowance_type;
            period?: import("../../../../types/google/protobuf").Duration;
            period_spend_limit: import("../../../../types/cosmos/base/v1beta1").Coin[];
            period_can_spend: import("../../../../types/cosmos/base/v1beta1").Coin[];
            period_reset?: Date;
        };
    };
    "/cosmos.feegrant.v1beta1.AllowedMsgAllowance": {
        aminoType: string;
        toAmino: (message: AllowedMsgAllowance) => {
            allowance?: import("../../../../types/google/protobuf").Any;
            allowed_messages: string[];
        };
        fromAmino: (object: AllowedMsgAllowance) => {
            allowance?: import("../../../../types/google/protobuf").Any;
            allowed_messages: string[];
        };
    };
    "/cosmos.feegrant.v1beta1.Grant": {
        aminoType: string;
        toAmino: (message: Grant) => {
            granter: string;
            grantee: string;
            allowance?: import("../../../../types/google/protobuf").Any;
        };
        fromAmino: (object: Grant) => {
            granter: string;
            grantee: string;
            allowance?: import("../../../../types/google/protobuf").Any;
        };
    };
};
