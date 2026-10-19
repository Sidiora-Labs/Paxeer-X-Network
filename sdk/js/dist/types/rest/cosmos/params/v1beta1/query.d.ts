import * as fm from "../../../fetch";
import type { QueryParamsRequest, QueryParamsResponse } from "../../../../types/cosmos/params/v1beta1/query.ts";
export declare class Query {
    static Params(req: QueryParamsRequest, initReq?: fm.InitReq): Promise<QueryParamsResponse>;
}
