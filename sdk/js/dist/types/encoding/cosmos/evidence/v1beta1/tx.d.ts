import type { GeneratedType } from "@cosmjs/proto-signing";
import type { MsgSubmitEvidenceResponse as MsgSubmitEvidenceResponse_type, MsgSubmitEvidence as MsgSubmitEvidence_type } from "../../../../types/cosmos/evidence/v1beta1";
import type { MessageFns } from "../../../common";
export interface MsgSubmitEvidence extends MsgSubmitEvidence_type {
}
export interface MsgSubmitEvidenceResponse extends MsgSubmitEvidenceResponse_type {
}
export declare const MsgSubmitEvidence: MessageFns<MsgSubmitEvidence, "cosmos.evidence.v1beta1.MsgSubmitEvidence">;
export declare const MsgSubmitEvidenceResponse: MessageFns<MsgSubmitEvidenceResponse, "cosmos.evidence.v1beta1.MsgSubmitEvidenceResponse">;
export declare const registry: Array<[string, GeneratedType]>;
export declare const aminoConverters: {
    "/cosmos.evidence.v1beta1.MsgSubmitEvidence": {
        aminoType: string;
        toAmino: (message: MsgSubmitEvidence) => {
            submitter: string;
            evidence?: import("../../../../types/google/protobuf").Any;
        };
        fromAmino: (object: MsgSubmitEvidence) => {
            submitter: string;
            evidence?: import("../../../../types/google/protobuf").Any;
        };
    };
    "/cosmos.evidence.v1beta1.MsgSubmitEvidenceResponse": {
        aminoType: string;
        toAmino: (message: MsgSubmitEvidenceResponse) => {
            hash: Uint8Array;
        };
        fromAmino: (object: MsgSubmitEvidenceResponse) => {
            hash: Uint8Array;
        };
    };
};
