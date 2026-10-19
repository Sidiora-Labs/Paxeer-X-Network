import * as fm from "../fetch";
import type { QueryDenomAllowListRequest, QueryDenomAllowListResponse, QueryDenomAuthorityMetadataRequest, QueryDenomAuthorityMetadataResponse, QueryDenomMetadataRequest, QueryDenomMetadataResponse, QueryDenomsFromCreatorRequest, QueryDenomsFromCreatorResponse, QueryParamsRequest, QueryParamsResponse } from "../../types/tokenfactory/query.ts";
export declare class Query {
    static Params(req: QueryParamsRequest, initReq?: fm.InitReq): Promise<QueryParamsResponse>;
    static DenomAuthorityMetadata(req: QueryDenomAuthorityMetadataRequest, initReq?: fm.InitReq): Promise<QueryDenomAuthorityMetadataResponse>;
    static DenomMetadata(req: QueryDenomMetadataRequest, initReq?: fm.InitReq): Promise<QueryDenomMetadataResponse>;
    static DenomsFromCreator(req: QueryDenomsFromCreatorRequest, initReq?: fm.InitReq): Promise<QueryDenomsFromCreatorResponse>;
    static DenomAllowList(req: QueryDenomAllowListRequest, initReq?: fm.InitReq): Promise<QueryDenomAllowListResponse>;
}
