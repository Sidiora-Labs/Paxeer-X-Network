import type { GeneratedType } from "@cosmjs/proto-signing";
import type { UpdateMinterProposal as UpdateMinterProposal_type } from "../../../types/mint/v1beta1";
import type { MessageFns } from "../../common";
export interface UpdateMinterProposal extends UpdateMinterProposal_type {
}
export declare const UpdateMinterProposal: MessageFns<UpdateMinterProposal, "paxprotocol.paxchain.mint.UpdateMinterProposal">;
export declare const registry: Array<[string, GeneratedType]>;
export declare const aminoConverters: {
    "/paxprotocol.paxchain.mint.UpdateMinterProposal": {
        aminoType: string;
        toAmino: (message: UpdateMinterProposal) => {
            title: string;
            description: string;
            minter?: import("../../../types/mint/v1beta1").Minter;
        };
        fromAmino: (object: UpdateMinterProposal) => {
            title: string;
            description: string;
            minter?: import("../../../types/mint/v1beta1").Minter;
        };
    };
};
