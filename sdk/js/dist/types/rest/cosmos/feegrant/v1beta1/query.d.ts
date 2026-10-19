import * as fm from "../../../fetch";
import type { QueryAllowanceRequest, QueryAllowanceResponse, QueryAllowancesByGranterRequest, QueryAllowancesByGranterResponse, QueryAllowancesRequest, QueryAllowancesResponse } from "../../../../types/cosmos/feegrant/v1beta1/query.ts";
export declare class Query {
    static Allowance(req: QueryAllowanceRequest, initReq?: fm.InitReq): Promise<QueryAllowanceResponse>;
    static Allowances(req: QueryAllowancesRequest, initReq?: fm.InitReq): Promise<QueryAllowancesResponse>;
    static AllowancesByGranter(req: QueryAllowancesByGranterRequest, initReq?: fm.InitReq): Promise<QueryAllowancesByGranterResponse>;
}
