import type { GeneratedType } from "@cosmjs/proto-signing";
import type { Balance as Balance_type, GenesisState as GenesisState_type, WeiBalance as WeiBalance_type } from "../../../../types/cosmos/bank/v1beta1";
import type { MessageFns } from "../../../common";
export interface GenesisState extends GenesisState_type {
}
export interface Balance extends Balance_type {
}
export interface WeiBalance extends WeiBalance_type {
}
export declare const GenesisState: MessageFns<GenesisState, "cosmos.bank.v1beta1.GenesisState">;
export declare const Balance: MessageFns<Balance, "cosmos.bank.v1beta1.Balance">;
export declare const WeiBalance: MessageFns<WeiBalance, "cosmos.bank.v1beta1.WeiBalance">;
export declare const registry: Array<[string, GeneratedType]>;
export declare const aminoConverters: {
    "/cosmos.bank.v1beta1.GenesisState": {
        aminoType: string;
        toAmino: (message: GenesisState) => {
            params?: import("../../../../types/cosmos/bank/v1beta1").Params;
            balances: Balance_type[];
            supply: import("../../../../types/cosmos/base/v1beta1").Coin[];
            denom_metadata: import("../../../../types/cosmos/bank/v1beta1").Metadata[];
            wei_balances: WeiBalance_type[];
        };
        fromAmino: (object: GenesisState) => {
            params?: import("../../../../types/cosmos/bank/v1beta1").Params;
            balances: Balance_type[];
            supply: import("../../../../types/cosmos/base/v1beta1").Coin[];
            denom_metadata: import("../../../../types/cosmos/bank/v1beta1").Metadata[];
            wei_balances: WeiBalance_type[];
        };
    };
    "/cosmos.bank.v1beta1.Balance": {
        aminoType: string;
        toAmino: (message: Balance) => {
            address: string;
            coins: import("../../../../types/cosmos/base/v1beta1").Coin[];
        };
        fromAmino: (object: Balance) => {
            address: string;
            coins: import("../../../../types/cosmos/base/v1beta1").Coin[];
        };
    };
    "/cosmos.bank.v1beta1.WeiBalance": {
        aminoType: string;
        toAmino: (message: WeiBalance) => {
            address: string;
            amount: string;
        };
        fromAmino: (object: WeiBalance) => {
            address: string;
            amount: string;
        };
    };
};
