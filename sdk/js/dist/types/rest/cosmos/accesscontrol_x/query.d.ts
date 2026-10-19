import * as fm from "../../fetch";
import type { ListResourceDependencyMappingRequest, ListResourceDependencyMappingResponse, ListWasmDependencyMappingRequest, ListWasmDependencyMappingResponse, QueryParamsRequest, QueryParamsResponse, ResourceDependencyMappingFromMessageKeyRequest, ResourceDependencyMappingFromMessageKeyResponse, WasmDependencyMappingRequest, WasmDependencyMappingResponse } from "../../../types/cosmos/accesscontrol_x/query.ts";
export declare class Query {
    static Params(req: QueryParamsRequest, initReq?: fm.InitReq): Promise<QueryParamsResponse>;
    static ResourceDependencyMappingFromMessageKey(req: ResourceDependencyMappingFromMessageKeyRequest, initReq?: fm.InitReq): Promise<ResourceDependencyMappingFromMessageKeyResponse>;
    static ListResourceDependencyMapping(req: ListResourceDependencyMappingRequest, initReq?: fm.InitReq): Promise<ListResourceDependencyMappingResponse>;
    static WasmDependencyMapping(req: WasmDependencyMappingRequest, initReq?: fm.InitReq): Promise<WasmDependencyMappingResponse>;
    static ListWasmDependencyMapping(req: ListWasmDependencyMappingRequest, initReq?: fm.InitReq): Promise<ListWasmDependencyMappingResponse>;
}
