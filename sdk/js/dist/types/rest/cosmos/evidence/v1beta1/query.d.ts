import * as fm from "../../../fetch";
import type { QueryAllEvidenceRequest, QueryAllEvidenceResponse, QueryEvidenceRequest, QueryEvidenceResponse } from "../../../../types/cosmos/evidence/v1beta1/query.ts";
export declare class Query {
    static Evidence(req: QueryEvidenceRequest, initReq?: fm.InitReq): Promise<QueryEvidenceResponse>;
    static AllEvidence(req: QueryAllEvidenceRequest, initReq?: fm.InitReq): Promise<QueryAllEvidenceResponse>;
}
