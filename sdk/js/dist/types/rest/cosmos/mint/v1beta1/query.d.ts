import * as fm from "../../../fetch";
import type { QueryAnnualProvisionsRequest, QueryAnnualProvisionsResponse, QueryInflationRequest, QueryInflationResponse, QueryParamsRequest, QueryParamsResponse } from "../../../../types/cosmos/mint/v1beta1/query.ts";
export declare class Query {
    static Params(req: QueryParamsRequest, initReq?: fm.InitReq): Promise<QueryParamsResponse>;
    static Inflation(req: QueryInflationRequest, initReq?: fm.InitReq): Promise<QueryInflationResponse>;
    static AnnualProvisions(req: QueryAnnualProvisionsRequest, initReq?: fm.InitReq): Promise<QueryAnnualProvisionsResponse>;
}
