import * as fm from "../../../fetch";
import type { QueryParamsRequest, QueryParamsResponse, QuerySigningInfoRequest, QuerySigningInfoResponse, QuerySigningInfosRequest, QuerySigningInfosResponse } from "../../../../types/cosmos/slashing/v1beta1/query.ts";
export declare class Query {
    static Params(req: QueryParamsRequest, initReq?: fm.InitReq): Promise<QueryParamsResponse>;
    static SigningInfo(req: QuerySigningInfoRequest, initReq?: fm.InitReq): Promise<QuerySigningInfoResponse>;
    static SigningInfos(req: QuerySigningInfosRequest, initReq?: fm.InitReq): Promise<QuerySigningInfosResponse>;
}
