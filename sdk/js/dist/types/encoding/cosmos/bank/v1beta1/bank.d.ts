import type { GeneratedType } from "@cosmjs/proto-signing";
import type { AllowList as AllowList_type, DenomUnit as DenomUnit_type, Input as Input_type, Metadata as Metadata_type, Output as Output_type, Params as Params_type, SendEnabled as SendEnabled_type, Supply as Supply_type } from "../../../../types/cosmos/bank/v1beta1";
import type { MessageFns } from "../../../common";
export interface Params extends Params_type {
}
export interface SendEnabled extends SendEnabled_type {
}
export interface Input extends Input_type {
}
export interface Output extends Output_type {
}
export interface Supply extends Supply_type {
}
export interface DenomUnit extends DenomUnit_type {
}
export interface Metadata extends Metadata_type {
}
export interface AllowList extends AllowList_type {
}
export declare const Params: MessageFns<Params, "cosmos.bank.v1beta1.Params">;
export declare const SendEnabled: MessageFns<SendEnabled, "cosmos.bank.v1beta1.SendEnabled">;
export declare const Input: MessageFns<Input, "cosmos.bank.v1beta1.Input">;
export declare const Output: MessageFns<Output, "cosmos.bank.v1beta1.Output">;
export declare const Supply: MessageFns<Supply, "cosmos.bank.v1beta1.Supply">;
export declare const DenomUnit: MessageFns<DenomUnit, "cosmos.bank.v1beta1.DenomUnit">;
export declare const Metadata: MessageFns<Metadata, "cosmos.bank.v1beta1.Metadata">;
export declare const AllowList: MessageFns<AllowList, "cosmos.bank.v1beta1.AllowList">;
export declare const registry: Array<[string, GeneratedType]>;
export declare const aminoConverters: {
    "/cosmos.bank.v1beta1.Params": {
        aminoType: string;
        toAmino: (message: Params) => {
            send_enabled: SendEnabled_type[];
            default_send_enabled: boolean;
        };
        fromAmino: (object: Params) => {
            send_enabled: SendEnabled_type[];
            default_send_enabled: boolean;
        };
    };
    "/cosmos.bank.v1beta1.SendEnabled": {
        aminoType: string;
        toAmino: (message: SendEnabled) => {
            denom: string;
            enabled: boolean;
        };
        fromAmino: (object: SendEnabled) => {
            denom: string;
            enabled: boolean;
        };
    };
    "/cosmos.bank.v1beta1.Input": {
        aminoType: string;
        toAmino: (message: Input) => {
            address: string;
            coins: import("../../../../types/cosmos/base/v1beta1").Coin[];
        };
        fromAmino: (object: Input) => {
            address: string;
            coins: import("../../../../types/cosmos/base/v1beta1").Coin[];
        };
    };
    "/cosmos.bank.v1beta1.Output": {
        aminoType: string;
        toAmino: (message: Output) => {
            address: string;
            coins: import("../../../../types/cosmos/base/v1beta1").Coin[];
        };
        fromAmino: (object: Output) => {
            address: string;
            coins: import("../../../../types/cosmos/base/v1beta1").Coin[];
        };
    };
    "/cosmos.bank.v1beta1.Supply": {
        aminoType: string;
        toAmino: (message: Supply) => {
            total: import("../../../../types/cosmos/base/v1beta1").Coin[];
        };
        fromAmino: (object: Supply) => {
            total: import("../../../../types/cosmos/base/v1beta1").Coin[];
        };
    };
    "/cosmos.bank.v1beta1.DenomUnit": {
        aminoType: string;
        toAmino: (message: DenomUnit) => {
            denom: string;
            exponent: number;
            aliases: string[];
        };
        fromAmino: (object: DenomUnit) => {
            denom: string;
            exponent: number;
            aliases: string[];
        };
    };
    "/cosmos.bank.v1beta1.Metadata": {
        aminoType: string;
        toAmino: (message: Metadata) => {
            description: string;
            denom_units: DenomUnit_type[];
            base: string;
            display: string;
            name: string;
            symbol: string;
        };
        fromAmino: (object: Metadata) => {
            description: string;
            denom_units: DenomUnit_type[];
            base: string;
            display: string;
            name: string;
            symbol: string;
        };
    };
    "/cosmos.bank.v1beta1.AllowList": {
        aminoType: string;
        toAmino: (message: AllowList) => {
            addresses: string[];
        };
        fromAmino: (object: AllowList) => {
            addresses: string[];
        };
    };
};
