import type { GeneratedType } from "@cosmjs/proto-signing";
import type { ParamChange as ParamChange_type, ParameterChangeProposal as ParameterChangeProposal_type } from "../../../../types/cosmos/params/v1beta1";
import type { MessageFns } from "../../../common";
export interface ParameterChangeProposal extends ParameterChangeProposal_type {
}
export interface ParamChange extends ParamChange_type {
}
export declare const ParameterChangeProposal: MessageFns<ParameterChangeProposal, "cosmos.params.v1beta1.ParameterChangeProposal">;
export declare const ParamChange: MessageFns<ParamChange, "cosmos.params.v1beta1.ParamChange">;
export declare const registry: Array<[string, GeneratedType]>;
export declare const aminoConverters: {
    "/cosmos.params.v1beta1.ParameterChangeProposal": {
        aminoType: string;
        toAmino: (message: ParameterChangeProposal) => {
            title: string;
            description: string;
            changes: ParamChange_type[];
            is_expedited: boolean;
        };
        fromAmino: (object: ParameterChangeProposal) => {
            title: string;
            description: string;
            changes: ParamChange_type[];
            is_expedited: boolean;
        };
    };
    "/cosmos.params.v1beta1.ParamChange": {
        aminoType: string;
        toAmino: (message: ParamChange) => {
            subspace: string;
            key: string;
            value: string;
        };
        fromAmino: (object: ParamChange) => {
            subspace: string;
            key: string;
            value: string;
        };
    };
};
