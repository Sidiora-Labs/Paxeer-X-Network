import type { AllowList, Metadata } from "../cosmos/bank/v1beta1/bank";
import type { Coin } from "../cosmos/base/v1beta1/coin";
export interface MsgCreateDenom {
    sender: string;
    /** subdenom can be up to 44 "alphanumeric" characters long. */
    subdenom: string;
    allow_list?: AllowList;
}
export interface MsgCreateDenomResponse {
    new_token_denom: string;
}
export interface MsgMint {
    sender: string;
    amount?: Coin;
}
export type MsgMintResponse = {};
export interface MsgBurn {
    sender: string;
    amount?: Coin;
}
export type MsgBurnResponse = {};
export interface MsgChangeAdmin {
    sender: string;
    denom: string;
    new_admin: string;
}
export type MsgChangeAdminResponse = {};
export interface MsgSetDenomMetadata {
    sender: string;
    metadata?: Metadata;
}
export type MsgSetDenomMetadataResponse = {};
export interface MsgUpdateDenom {
    sender: string;
    denom: string;
    allow_list?: AllowList;
}
export type MsgUpdateDenomResponse = {};
