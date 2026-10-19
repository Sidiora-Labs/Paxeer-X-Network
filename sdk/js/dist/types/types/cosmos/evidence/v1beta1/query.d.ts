import type { Any } from "../../../google/protobuf/any";
import type { PageRequest, PageResponse } from "../../base/query/v1beta1/pagination";
export interface QueryEvidenceRequest {
    /** evidence_hash defines the hash of the requested evidence. */
    evidence_hash: Uint8Array;
}
export interface QueryEvidenceResponse {
    /** evidence returns the requested evidence. */
    evidence?: Any;
}
export interface QueryAllEvidenceRequest {
    /** pagination defines an optional pagination for the request. */
    pagination?: PageRequest;
}
export interface QueryAllEvidenceResponse {
    /** evidence returns all evidences. */
    evidence: Any[];
    /** pagination defines the pagination in the response. */
    pagination?: PageResponse;
}
