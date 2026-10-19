import type { GeneratedType } from "@cosmjs/proto-signing";
import type { MsgVerifyInvariantResponse as MsgVerifyInvariantResponse_type, MsgVerifyInvariant as MsgVerifyInvariant_type } from "../../../../types/cosmos/crisis/v1beta1";
import type { MessageFns } from "../../../common";
export interface MsgVerifyInvariant extends MsgVerifyInvariant_type {
}
export interface MsgVerifyInvariantResponse extends MsgVerifyInvariantResponse_type {
}
export declare const MsgVerifyInvariant: MessageFns<MsgVerifyInvariant, "cosmos.crisis.v1beta1.MsgVerifyInvariant">;
export declare const MsgVerifyInvariantResponse: MessageFns<MsgVerifyInvariantResponse, "cosmos.crisis.v1beta1.MsgVerifyInvariantResponse">;
export declare const registry: Array<[string, GeneratedType]>;
export declare const aminoConverters: {
    "/cosmos.crisis.v1beta1.MsgVerifyInvariant": {
        aminoType: string;
        toAmino: (message: MsgVerifyInvariant) => {
            sender: string;
            invariant_module_name: string;
            invariant_route: string;
        };
        fromAmino: (object: MsgVerifyInvariant) => {
            sender: string;
            invariant_module_name: string;
            invariant_route: string;
        };
    };
    "/cosmos.crisis.v1beta1.MsgVerifyInvariantResponse": {
        aminoType: string;
        toAmino: (message: MsgVerifyInvariantResponse) => {};
        fromAmino: (object: MsgVerifyInvariantResponse) => {};
    };
};
