import type { GeneratedType } from "@cosmjs/proto-signing";
import type { MsgCreateVestingAccountResponse as MsgCreateVestingAccountResponse_type, MsgCreateVestingAccount as MsgCreateVestingAccount_type } from "../../../../types/cosmos/vesting/v1beta1";
import type { MessageFns } from "../../../common";
export interface MsgCreateVestingAccount extends MsgCreateVestingAccount_type {
}
export interface MsgCreateVestingAccountResponse extends MsgCreateVestingAccountResponse_type {
}
export declare const MsgCreateVestingAccount: MessageFns<MsgCreateVestingAccount, "cosmos.vesting.v1beta1.MsgCreateVestingAccount">;
export declare const MsgCreateVestingAccountResponse: MessageFns<MsgCreateVestingAccountResponse, "cosmos.vesting.v1beta1.MsgCreateVestingAccountResponse">;
export declare const registry: Array<[string, GeneratedType]>;
export declare const aminoConverters: {
    "/cosmos.vesting.v1beta1.MsgCreateVestingAccount": {
        aminoType: string;
        toAmino: (message: MsgCreateVestingAccount) => {
            from_address: string;
            to_address: string;
            amount: import("../../../../types/cosmos/base/v1beta1").Coin[];
            end_time: number;
            delayed: boolean;
            admin: string;
        };
        fromAmino: (object: MsgCreateVestingAccount) => {
            from_address: string;
            to_address: string;
            amount: import("../../../../types/cosmos/base/v1beta1").Coin[];
            end_time: number;
            delayed: boolean;
            admin: string;
        };
    };
    "/cosmos.vesting.v1beta1.MsgCreateVestingAccountResponse": {
        aminoType: string;
        toAmino: (message: MsgCreateVestingAccountResponse) => {};
        fromAmino: (object: MsgCreateVestingAccountResponse) => {};
    };
};
