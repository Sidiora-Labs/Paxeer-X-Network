import type { GeneratedType } from "@cosmjs/proto-signing";
import type { Coin as Coin_type, DecCoin as DecCoin_type, DecProto as DecProto_type, IntProto as IntProto_type } from "../../../../types/cosmos/base/v1beta1";
import type { MessageFns } from "../../../common";
export interface Coin extends Coin_type {
}
export interface DecCoin extends DecCoin_type {
}
export interface IntProto extends IntProto_type {
}
export interface DecProto extends DecProto_type {
}
export declare const Coin: MessageFns<Coin, "cosmos.base.v1beta1.Coin">;
export declare const DecCoin: MessageFns<DecCoin, "cosmos.base.v1beta1.DecCoin">;
export declare const IntProto: MessageFns<IntProto, "cosmos.base.v1beta1.IntProto">;
export declare const DecProto: MessageFns<DecProto, "cosmos.base.v1beta1.DecProto">;
export declare const registry: Array<[string, GeneratedType]>;
export declare const aminoConverters: {
    "/cosmos.base.v1beta1.Coin": {
        aminoType: string;
        toAmino: (message: Coin) => {
            denom: string;
            amount: string;
        };
        fromAmino: (object: Coin) => {
            denom: string;
            amount: string;
        };
    };
    "/cosmos.base.v1beta1.DecCoin": {
        aminoType: string;
        toAmino: (message: DecCoin) => {
            denom: string;
            amount: string;
        };
        fromAmino: (object: DecCoin) => {
            denom: string;
            amount: string;
        };
    };
    "/cosmos.base.v1beta1.IntProto": {
        aminoType: string;
        toAmino: (message: IntProto) => {
            int: string;
        };
        fromAmino: (object: IntProto) => {
            int: string;
        };
    };
    "/cosmos.base.v1beta1.DecProto": {
        aminoType: string;
        toAmino: (message: DecProto) => {
            dec: string;
        };
        fromAmino: (object: DecProto) => {
            dec: string;
        };
    };
};
