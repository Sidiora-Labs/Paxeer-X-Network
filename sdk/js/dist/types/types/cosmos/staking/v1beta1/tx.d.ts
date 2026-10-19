import type { Any } from "../../../google/protobuf/any";
import type { Coin } from "../../base/v1beta1/coin";
import type { CommissionRates, Description } from "./staking";
export interface MsgCreateValidator {
    description?: Description;
    commission?: CommissionRates;
    min_self_delegation: string;
    delegator_address: string;
    validator_address: string;
    pubkey?: Any;
    value?: Coin;
}
export type MsgCreateValidatorResponse = {};
export interface MsgEditValidator {
    description?: Description;
    validator_address: string;
    /**
     * We pass a reference to the new commission rate and min self delegation as
     * it's not mandatory to update. If not updated, the deserialized rate will be
     * zero with no way to distinguish if an update was intended.
     * REF: #2373
     */
    commission_rate: string;
    min_self_delegation: string;
}
export type MsgEditValidatorResponse = {};
export interface MsgDelegate {
    delegator_address: string;
    validator_address: string;
    amount?: Coin;
}
export type MsgDelegateResponse = {};
export interface MsgBeginRedelegate {
    delegator_address: string;
    validator_src_address: string;
    validator_dst_address: string;
    amount?: Coin;
}
export interface MsgBeginRedelegateResponse {
    completion_time?: Date;
}
export interface MsgUndelegate {
    delegator_address: string;
    validator_address: string;
    amount?: Coin;
}
export interface MsgUndelegateResponse {
    completion_time?: Date;
}
