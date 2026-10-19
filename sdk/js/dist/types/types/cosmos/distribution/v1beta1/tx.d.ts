import type { Coin } from "../../base/v1beta1/coin";
export interface MsgSetWithdrawAddress {
    delegator_address: string;
    withdraw_address: string;
}
export type MsgSetWithdrawAddressResponse = {};
export interface MsgWithdrawDelegatorReward {
    delegator_address: string;
    validator_address: string;
}
export type MsgWithdrawDelegatorRewardResponse = {};
export interface MsgWithdrawValidatorCommission {
    validator_address: string;
}
export type MsgWithdrawValidatorCommissionResponse = {};
export interface MsgFundCommunityPool {
    amount: Coin[];
    depositor: string;
}
export type MsgFundCommunityPoolResponse = {};
