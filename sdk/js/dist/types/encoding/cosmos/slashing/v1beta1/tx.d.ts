import type { GeneratedType } from "@cosmjs/proto-signing";
import type { MsgUnjailResponse as MsgUnjailResponse_type, MsgUnjail as MsgUnjail_type } from "../../../../types/cosmos/slashing/v1beta1";
import type { MessageFns } from "../../../common";
export interface MsgUnjail extends MsgUnjail_type {
}
export interface MsgUnjailResponse extends MsgUnjailResponse_type {
}
export declare const MsgUnjail: MessageFns<MsgUnjail, "cosmos.slashing.v1beta1.MsgUnjail">;
export declare const MsgUnjailResponse: MessageFns<MsgUnjailResponse, "cosmos.slashing.v1beta1.MsgUnjailResponse">;
export declare const registry: Array<[string, GeneratedType]>;
export declare const aminoConverters: {
    "/cosmos.slashing.v1beta1.MsgUnjail": {
        aminoType: string;
        toAmino: (message: MsgUnjail) => {
            validator_addr: string;
        };
        fromAmino: (object: MsgUnjail) => {
            validator_addr: string;
        };
    };
    "/cosmos.slashing.v1beta1.MsgUnjailResponse": {
        aminoType: string;
        toAmino: (message: MsgUnjailResponse) => {};
        fromAmino: (object: MsgUnjailResponse) => {};
    };
};
