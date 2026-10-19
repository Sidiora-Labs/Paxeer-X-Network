import * as fm from "../fetch";
import type { QueryEVMAddressByPaxAddressRequest, QueryEVMAddressByPaxAddressResponse, QueryPointeeRequest, QueryPointeeResponse, QueryPointerRequest, QueryPointerResponse, QueryPointerVersionRequest, QueryPointerVersionResponse, QueryPaxAddressByEVMAddressRequest, QueryPaxAddressByEVMAddressResponse, QueryStaticCallRequest, QueryStaticCallResponse } from "../../types/evm/query.ts";
export declare class Query {
    static PaxAddressByEVMAddress(req: QueryPaxAddressByEVMAddressRequest, initReq?: fm.InitReq): Promise<QueryPaxAddressByEVMAddressResponse>;
    static EVMAddressByPaxAddress(req: QueryEVMAddressByPaxAddressRequest, initReq?: fm.InitReq): Promise<QueryEVMAddressByPaxAddressResponse>;
    static StaticCall(req: QueryStaticCallRequest, initReq?: fm.InitReq): Promise<QueryStaticCallResponse>;
    static Pointer(req: QueryPointerRequest, initReq?: fm.InitReq): Promise<QueryPointerResponse>;
    static PointerVersion(req: QueryPointerVersionRequest, initReq?: fm.InitReq): Promise<QueryPointerVersionResponse>;
    static Pointee(req: QueryPointeeRequest, initReq?: fm.InitReq): Promise<QueryPointeeResponse>;
}
