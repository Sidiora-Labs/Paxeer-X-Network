import type { Any } from "../../../google/protobuf/any";
import type { PageRequest, PageResponse } from "../../base/query/v1beta1/pagination";
import type { Params } from "./auth";
export interface QueryAccountsRequest {
    /** pagination defines an optional pagination for the request. */
    pagination?: PageRequest;
}
export interface QueryAccountsResponse {
    /** accounts are the existing accounts */
    accounts: Any[];
    /** pagination defines the pagination in the response. */
    pagination?: PageResponse;
}
export interface QueryAccountRequest {
    /** address defines the address to query for. */
    address: string;
}
export interface QueryAccountResponse {
    /** account defines the account of the corresponding address. */
    account?: Any;
}
export type QueryParamsRequest = {};
export interface QueryParamsResponse {
    /** params defines the parameters of the module. */
    params?: Params;
}
export type QueryNextAccountNumberRequest = {};
export interface QueryNextAccountNumberResponse {
    /** count defines the next account number. */
    count: number;
}
