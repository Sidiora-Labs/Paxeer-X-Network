import type { GeneratedType } from "@cosmjs/proto-signing";
import type { QueryAllEvidenceRequest as QueryAllEvidenceRequest_type, QueryAllEvidenceResponse as QueryAllEvidenceResponse_type, QueryEvidenceRequest as QueryEvidenceRequest_type, QueryEvidenceResponse as QueryEvidenceResponse_type } from "../../../../types/cosmos/evidence/v1beta1";
import type { MessageFns } from "../../../common";
export interface QueryEvidenceRequest extends QueryEvidenceRequest_type {
}
export interface QueryEvidenceResponse extends QueryEvidenceResponse_type {
}
export interface QueryAllEvidenceRequest extends QueryAllEvidenceRequest_type {
}
export interface QueryAllEvidenceResponse extends QueryAllEvidenceResponse_type {
}
export declare const QueryEvidenceRequest: MessageFns<QueryEvidenceRequest, "cosmos.evidence.v1beta1.QueryEvidenceRequest">;
export declare const QueryEvidenceResponse: MessageFns<QueryEvidenceResponse, "cosmos.evidence.v1beta1.QueryEvidenceResponse">;
export declare const QueryAllEvidenceRequest: MessageFns<QueryAllEvidenceRequest, "cosmos.evidence.v1beta1.QueryAllEvidenceRequest">;
export declare const QueryAllEvidenceResponse: MessageFns<QueryAllEvidenceResponse, "cosmos.evidence.v1beta1.QueryAllEvidenceResponse">;
export declare const registry: Array<[string, GeneratedType]>;
export declare const aminoConverters: {
    "/cosmos.evidence.v1beta1.QueryEvidenceRequest": {
        aminoType: string;
        toAmino: (message: QueryEvidenceRequest) => {
            evidence_hash: Uint8Array;
        };
        fromAmino: (object: QueryEvidenceRequest) => {
            evidence_hash: Uint8Array;
        };
    };
    "/cosmos.evidence.v1beta1.QueryEvidenceResponse": {
        aminoType: string;
        toAmino: (message: QueryEvidenceResponse) => {
            evidence?: import("../../../../types/google/protobuf").Any;
        };
        fromAmino: (object: QueryEvidenceResponse) => {
            evidence?: import("../../../../types/google/protobuf").Any;
        };
    };
    "/cosmos.evidence.v1beta1.QueryAllEvidenceRequest": {
        aminoType: string;
        toAmino: (message: QueryAllEvidenceRequest) => {
            pagination?: import("../../../../types/cosmos/base/query/v1beta1").PageRequest;
        };
        fromAmino: (object: QueryAllEvidenceRequest) => {
            pagination?: import("../../../../types/cosmos/base/query/v1beta1").PageRequest;
        };
    };
    "/cosmos.evidence.v1beta1.QueryAllEvidenceResponse": {
        aminoType: string;
        toAmino: (message: QueryAllEvidenceResponse) => {
            evidence: import("../../../../types/google/protobuf").Any[];
            pagination?: import("../../../../types/cosmos/base/query/v1beta1").PageResponse;
        };
        fromAmino: (object: QueryAllEvidenceResponse) => {
            evidence: import("../../../../types/google/protobuf").Any[];
            pagination?: import("../../../../types/cosmos/base/query/v1beta1").PageResponse;
        };
    };
};
