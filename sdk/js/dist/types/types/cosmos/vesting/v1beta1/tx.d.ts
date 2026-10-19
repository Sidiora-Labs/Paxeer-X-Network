import type { Coin } from "../../base/v1beta1/coin";
export interface MsgCreateVestingAccount {
    from_address: string;
    to_address: string;
    amount: Coin[];
    end_time: number;
    delayed: boolean;
    admin: string;
}
export type MsgCreateVestingAccountResponse = {};
