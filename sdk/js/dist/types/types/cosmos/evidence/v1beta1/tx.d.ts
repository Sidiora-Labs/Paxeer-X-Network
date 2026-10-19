import type { Any } from "../../../google/protobuf/any";
export interface MsgSubmitEvidence {
    submitter: string;
    evidence?: Any;
}
export interface MsgSubmitEvidenceResponse {
    /** hash defines the hash of the evidence. */
    hash: Uint8Array;
}
