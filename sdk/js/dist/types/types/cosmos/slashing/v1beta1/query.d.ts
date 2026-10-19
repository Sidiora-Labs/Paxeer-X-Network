import type { PageRequest, PageResponse } from "../../base/query/v1beta1/pagination";
import type { Params, ValidatorSigningInfo } from "./slashing";
export type QueryParamsRequest = {};
export interface QueryParamsResponse {
    params?: Params;
}
export interface QuerySigningInfoRequest {
    /** cons_address is the address to query signing info of */
    cons_address: string;
}
export interface QuerySigningInfoResponse {
    /** val_signing_info is the signing info of requested val cons address */
    val_signing_info?: ValidatorSigningInfo;
}
export interface QuerySigningInfosRequest {
    pagination?: PageRequest;
}
export interface QuerySigningInfosResponse {
    /** info is the signing info of all validators */
    info: ValidatorSigningInfo[];
    pagination?: PageResponse;
}
