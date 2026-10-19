import * as fm from "../../fetch";
import type { QueryMinterRequest, QueryMinterResponse, QueryParamsRequest, QueryParamsResponse } from "../../../types/mint/v1beta1/query.ts";
export declare class Query {
    static Params(req: QueryParamsRequest, initReq?: fm.InitReq): Promise<QueryParamsResponse>;
    static Minter(req: QueryMinterRequest, initReq?: fm.InitReq): Promise<QueryMinterResponse>;
}
