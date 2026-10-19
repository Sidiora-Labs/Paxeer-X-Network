import type { GeneratedType } from "@cosmjs/proto-signing";
import type { BaseVestingAccount as BaseVestingAccount_type, ContinuousVestingAccount as ContinuousVestingAccount_type, DelayedVestingAccount as DelayedVestingAccount_type, Period as Period_type, PeriodicVestingAccount as PeriodicVestingAccount_type, PermanentLockedAccount as PermanentLockedAccount_type } from "../../../../types/cosmos/vesting/v1beta1";
import type { MessageFns } from "../../../common";
export interface BaseVestingAccount extends BaseVestingAccount_type {
}
export interface ContinuousVestingAccount extends ContinuousVestingAccount_type {
}
export interface DelayedVestingAccount extends DelayedVestingAccount_type {
}
export interface Period extends Period_type {
}
export interface PeriodicVestingAccount extends PeriodicVestingAccount_type {
}
export interface PermanentLockedAccount extends PermanentLockedAccount_type {
}
export declare const BaseVestingAccount: MessageFns<BaseVestingAccount, "cosmos.vesting.v1beta1.BaseVestingAccount">;
export declare const ContinuousVestingAccount: MessageFns<ContinuousVestingAccount, "cosmos.vesting.v1beta1.ContinuousVestingAccount">;
export declare const DelayedVestingAccount: MessageFns<DelayedVestingAccount, "cosmos.vesting.v1beta1.DelayedVestingAccount">;
export declare const Period: MessageFns<Period, "cosmos.vesting.v1beta1.Period">;
export declare const PeriodicVestingAccount: MessageFns<PeriodicVestingAccount, "cosmos.vesting.v1beta1.PeriodicVestingAccount">;
export declare const PermanentLockedAccount: MessageFns<PermanentLockedAccount, "cosmos.vesting.v1beta1.PermanentLockedAccount">;
export declare const registry: Array<[string, GeneratedType]>;
export declare const aminoConverters: {
    "/cosmos.vesting.v1beta1.BaseVestingAccount": {
        aminoType: string;
        toAmino: (message: BaseVestingAccount) => {
            base_account?: import("../../../../types/cosmos/auth/v1beta1").BaseAccount;
            original_vesting: import("../../../../types/cosmos/base/v1beta1").Coin[];
            delegated_free: import("../../../../types/cosmos/base/v1beta1").Coin[];
            delegated_vesting: import("../../../../types/cosmos/base/v1beta1").Coin[];
            end_time: number;
            admin: string;
            cancelled_time: number;
        };
        fromAmino: (object: BaseVestingAccount) => {
            base_account?: import("../../../../types/cosmos/auth/v1beta1").BaseAccount;
            original_vesting: import("../../../../types/cosmos/base/v1beta1").Coin[];
            delegated_free: import("../../../../types/cosmos/base/v1beta1").Coin[];
            delegated_vesting: import("../../../../types/cosmos/base/v1beta1").Coin[];
            end_time: number;
            admin: string;
            cancelled_time: number;
        };
    };
    "/cosmos.vesting.v1beta1.ContinuousVestingAccount": {
        aminoType: string;
        toAmino: (message: ContinuousVestingAccount) => {
            base_vesting_account?: BaseVestingAccount_type;
            start_time: number;
        };
        fromAmino: (object: ContinuousVestingAccount) => {
            base_vesting_account?: BaseVestingAccount_type;
            start_time: number;
        };
    };
    "/cosmos.vesting.v1beta1.DelayedVestingAccount": {
        aminoType: string;
        toAmino: (message: DelayedVestingAccount) => {
            base_vesting_account?: BaseVestingAccount_type;
        };
        fromAmino: (object: DelayedVestingAccount) => {
            base_vesting_account?: BaseVestingAccount_type;
        };
    };
    "/cosmos.vesting.v1beta1.Period": {
        aminoType: string;
        toAmino: (message: Period) => {
            length: number;
            amount: import("../../../../types/cosmos/base/v1beta1").Coin[];
        };
        fromAmino: (object: Period) => {
            length: number;
            amount: import("../../../../types/cosmos/base/v1beta1").Coin[];
        };
    };
    "/cosmos.vesting.v1beta1.PeriodicVestingAccount": {
        aminoType: string;
        toAmino: (message: PeriodicVestingAccount) => {
            base_vesting_account?: BaseVestingAccount_type;
            start_time: number;
            vesting_periods: Period_type[];
        };
        fromAmino: (object: PeriodicVestingAccount) => {
            base_vesting_account?: BaseVestingAccount_type;
            start_time: number;
            vesting_periods: Period_type[];
        };
    };
    "/cosmos.vesting.v1beta1.PermanentLockedAccount": {
        aminoType: string;
        toAmino: (message: PermanentLockedAccount) => {
            base_vesting_account?: BaseVestingAccount_type;
        };
        fromAmino: (object: PermanentLockedAccount) => {
            base_vesting_account?: BaseVestingAccount_type;
        };
    };
};
