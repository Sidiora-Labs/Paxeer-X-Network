import type { BaseAccount } from "../../auth/v1beta1/auth";
import type { Coin } from "../../base/v1beta1/coin";
export interface BaseVestingAccount {
    base_account?: BaseAccount;
    original_vesting: Coin[];
    delegated_free: Coin[];
    delegated_vesting: Coin[];
    end_time: number;
    /** admin field (optional), an address who has oversight powers for the vesting account such as cancelling */
    admin: string;
    /** this field (default nil) indicates whether the vesting for the account has been cancelled (and what time it was cancelled) */
    cancelled_time: number;
}
export interface ContinuousVestingAccount {
    base_vesting_account?: BaseVestingAccount;
    start_time: number;
}
export interface DelayedVestingAccount {
    base_vesting_account?: BaseVestingAccount;
}
export interface Period {
    length: number;
    amount: Coin[];
}
export interface PeriodicVestingAccount {
    base_vesting_account?: BaseVestingAccount;
    start_time: number;
    vesting_periods: Period[];
}
export interface PermanentLockedAccount {
    base_vesting_account?: BaseVestingAccount;
}
