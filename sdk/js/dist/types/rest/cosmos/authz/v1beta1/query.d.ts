import * as fm from "../../../fetch";
import type { QueryGranteeGrantsRequest, QueryGranteeGrantsResponse, QueryGranterGrantsRequest, QueryGranterGrantsResponse, QueryGrantsRequest, QueryGrantsResponse } from "../../../../types/cosmos/authz/v1beta1/query.ts";
export declare class Query {
    static Grants(req: QueryGrantsRequest, initReq?: fm.InitReq): Promise<QueryGrantsResponse>;
    static GranterGrants(req: QueryGranterGrantsRequest, initReq?: fm.InitReq): Promise<QueryGranterGrantsResponse>;
    static GranteeGrants(req: QueryGranteeGrantsRequest, initReq?: fm.InitReq): Promise<QueryGranteeGrantsResponse>;
}
