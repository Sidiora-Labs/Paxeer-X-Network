import type { GeneratedType } from "@cosmjs/proto-signing";
import type { MsgMultiSendResponse as MsgMultiSendResponse_type, MsgMultiSend as MsgMultiSend_type, MsgSendResponse as MsgSendResponse_type, MsgSend as MsgSend_type } from "../../../../types/cosmos/bank/v1beta1";
import type { MessageFns } from "../../../common";
export interface MsgSend extends MsgSend_type {
}
export interface MsgSendResponse extends MsgSendResponse_type {
}
export interface MsgMultiSend extends MsgMultiSend_type {
}
export interface MsgMultiSendResponse extends MsgMultiSendResponse_type {
}
export declare const MsgSend: MessageFns<MsgSend, "cosmos.bank.v1beta1.MsgSend">;
export declare const MsgSendResponse: MessageFns<MsgSendResponse, "cosmos.bank.v1beta1.MsgSendResponse">;
export declare const MsgMultiSend: MessageFns<MsgMultiSend, "cosmos.bank.v1beta1.MsgMultiSend">;
export declare const MsgMultiSendResponse: MessageFns<MsgMultiSendResponse, "cosmos.bank.v1beta1.MsgMultiSendResponse">;
export declare const registry: Array<[string, GeneratedType]>;
export declare const aminoConverters: {
    "/cosmos.bank.v1beta1.MsgSend": {
        aminoType: string;
        toAmino: (message: MsgSend) => {
            from_address: string;
            to_address: string;
            amount: import("../../../../types/cosmos/base/v1beta1").Coin[];
        };
        fromAmino: (object: MsgSend) => {
            from_address: string;
            to_address: string;
            amount: import("../../../../types/cosmos/base/v1beta1").Coin[];
        };
    };
    "/cosmos.bank.v1beta1.MsgSendResponse": {
        aminoType: string;
        toAmino: (message: MsgSendResponse) => {};
        fromAmino: (object: MsgSendResponse) => {};
    };
    "/cosmos.bank.v1beta1.MsgMultiSend": {
        aminoType: string;
        toAmino: (message: MsgMultiSend) => {
            inputs: import("../../../../types/cosmos/bank/v1beta1").Input[];
            outputs: import("../../../../types/cosmos/bank/v1beta1").Output[];
        };
        fromAmino: (object: MsgMultiSend) => {
            inputs: import("../../../../types/cosmos/bank/v1beta1").Input[];
            outputs: import("../../../../types/cosmos/bank/v1beta1").Output[];
        };
    };
    "/cosmos.bank.v1beta1.MsgMultiSendResponse": {
        aminoType: string;
        toAmino: (message: MsgMultiSendResponse) => {};
        fromAmino: (object: MsgMultiSendResponse) => {};
    };
};
