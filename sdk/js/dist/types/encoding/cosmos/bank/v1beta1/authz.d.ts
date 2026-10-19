import type { GeneratedType } from "@cosmjs/proto-signing";
import type { SendAuthorization as SendAuthorization_type } from "../../../../types/cosmos/bank/v1beta1";
import type { MessageFns } from "../../../common";
export interface SendAuthorization extends SendAuthorization_type {
}
export declare const SendAuthorization: MessageFns<SendAuthorization, "cosmos.bank.v1beta1.SendAuthorization">;
export declare const registry: Array<[string, GeneratedType]>;
export declare const aminoConverters: {
    "/cosmos.bank.v1beta1.SendAuthorization": {
        aminoType: string;
        toAmino: (message: SendAuthorization) => {
            spend_limit: import("../../../../types/cosmos/base/v1beta1").Coin[];
        };
        fromAmino: (object: SendAuthorization) => {
            spend_limit: import("../../../../types/cosmos/base/v1beta1").Coin[];
        };
    };
};
