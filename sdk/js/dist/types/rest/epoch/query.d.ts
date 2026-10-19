import * as fm from "../fetch";
import type { QueryEpochRequest, QueryEpochResponse, QueryParamsRequest, QueryParamsResponse } from "../../types/epoch/query.ts";
export declare class Query {
    static Epoch(req: QueryEpochRequest, initReq?: fm.InitReq): Promise<QueryEpochResponse>;
    static Params(req: QueryParamsRequest, initReq?: fm.InitReq): Promise<QueryParamsResponse>;
}
