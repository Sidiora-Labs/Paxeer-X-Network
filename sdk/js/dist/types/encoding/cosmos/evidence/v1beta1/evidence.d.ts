import type { GeneratedType } from "@cosmjs/proto-signing";
import type { Equivocation as Equivocation_type } from "../../../../types/cosmos/evidence/v1beta1";
import type { MessageFns } from "../../../common";
export interface Equivocation extends Equivocation_type {
}
export declare const Equivocation: MessageFns<Equivocation, "cosmos.evidence.v1beta1.Equivocation">;
export declare const registry: Array<[string, GeneratedType]>;
export declare const aminoConverters: {
    "/cosmos.evidence.v1beta1.Equivocation": {
        aminoType: string;
        toAmino: (message: Equivocation) => {
            height: number;
            time?: Date;
            power: number;
            consensus_address: string;
        };
        fromAmino: (object: Equivocation) => {
            height: number;
            time?: Date;
            power: number;
            consensus_address: string;
        };
    };
};
