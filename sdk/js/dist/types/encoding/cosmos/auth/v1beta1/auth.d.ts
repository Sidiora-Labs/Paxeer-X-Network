import type { GeneratedType } from "@cosmjs/proto-signing";
import type { BaseAccount as BaseAccount_type, ModuleAccount as ModuleAccount_type, Params as Params_type } from "../../../../types/cosmos/auth/v1beta1";
import type { MessageFns } from "../../../common";
export interface BaseAccount extends BaseAccount_type {
}
export interface ModuleAccount extends ModuleAccount_type {
}
export interface Params extends Params_type {
}
export declare const BaseAccount: MessageFns<BaseAccount, "cosmos.auth.v1beta1.BaseAccount">;
export declare const ModuleAccount: MessageFns<ModuleAccount, "cosmos.auth.v1beta1.ModuleAccount">;
export declare const Params: MessageFns<Params, "cosmos.auth.v1beta1.Params">;
export declare const registry: Array<[string, GeneratedType]>;
export declare const aminoConverters: {
    "/cosmos.auth.v1beta1.BaseAccount": {
        aminoType: string;
        toAmino: (message: BaseAccount) => {
            address: string;
            pub_key?: import("../../../../types/google/protobuf").Any;
            account_number: number;
            sequence: number;
        };
        fromAmino: (object: BaseAccount) => {
            address: string;
            pub_key?: import("../../../../types/google/protobuf").Any;
            account_number: number;
            sequence: number;
        };
    };
    "/cosmos.auth.v1beta1.ModuleAccount": {
        aminoType: string;
        toAmino: (message: ModuleAccount) => {
            base_account?: BaseAccount_type;
            name: string;
            permissions: string[];
        };
        fromAmino: (object: ModuleAccount) => {
            base_account?: BaseAccount_type;
            name: string;
            permissions: string[];
        };
    };
    "/cosmos.auth.v1beta1.Params": {
        aminoType: string;
        toAmino: (message: Params) => {
            max_memo_characters: number;
            tx_sig_limit: number;
            tx_size_cost_per_byte: number;
            sig_verify_cost_ed25519: number;
            sig_verify_cost_secp256k1: number;
            disable_seqno_check: boolean;
        };
        fromAmino: (object: Params) => {
            max_memo_characters: number;
            tx_sig_limit: number;
            tx_size_cost_per_byte: number;
            sig_verify_cost_ed25519: number;
            sig_verify_cost_secp256k1: number;
            disable_seqno_check: boolean;
        };
    };
};
