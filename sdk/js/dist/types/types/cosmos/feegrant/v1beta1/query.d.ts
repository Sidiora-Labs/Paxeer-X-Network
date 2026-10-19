import type { PageRequest, PageResponse } from "../../base/query/v1beta1/pagination";
import type { Grant } from "./feegrant";
export interface QueryAllowanceRequest {
    /** granter is the address of the user granting an allowance of their funds. */
    granter: string;
    /** grantee is the address of the user being granted an allowance of another user's funds. */
    grantee: string;
}
export interface QueryAllowanceResponse {
    /** allowance is a allowance granted for grantee by granter. */
    allowance?: Grant;
}
export interface QueryAllowancesRequest {
    grantee: string;
    /** pagination defines an pagination for the request. */
    pagination?: PageRequest;
}
export interface QueryAllowancesResponse {
    /** allowances are allowance's granted for grantee by granter. */
    allowances: Grant[];
    /** pagination defines an pagination for the response. */
    pagination?: PageResponse;
}
export interface QueryAllowancesByGranterRequest {
    granter: string;
    /** pagination defines an pagination for the request. */
    pagination?: PageRequest;
}
export interface QueryAllowancesByGranterResponse {
    /** allowances that have been issued by the granter. */
    allowances: Grant[];
    /** pagination defines an pagination for the response. */
    pagination?: PageResponse;
}
