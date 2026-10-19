import type { Coin } from "../../base/v1beta1/coin";
import type { Input, Output } from "./bank";
export interface MsgSend {
    from_address: string;
    to_address: string;
    amount: Coin[];
}
export type MsgSendResponse = {};
export interface MsgMultiSend {
    inputs: Input[];
    outputs: Output[];
}
export type MsgMultiSendResponse = {};
