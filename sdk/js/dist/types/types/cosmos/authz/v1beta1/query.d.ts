import type { PageRequest, PageResponse } from "../../base/query/v1beta1/pagination";
import type { Grant, GrantAuthorization } from "./authz";
export interface QueryGrantsRequest {
    granter: string;
    grantee: string;
    /** Optional, msg_type_url, when set, will query only grants matching given msg type. */
    msg_type_url: string;
    /** pagination defines an pagination for the request. */
    pagination?: PageRequest;
}
export interface QueryGrantsResponse {
    /** authorizations is a list of grants granted for grantee by granter. */
    grants: Grant[];
    /** pagination defines an pagination for the response. */
    pagination?: PageResponse;
}
export interface QueryGranterGrantsRequest {
    granter: string;
    /** pagination defines an pagination for the request. */
    pagination?: PageRequest;
}
export interface QueryGranterGrantsResponse {
    /** grants is a list of grants granted by the granter. */
    grants: GrantAuthorization[];
    /** pagination defines an pagination for the response. */
    pagination?: PageResponse;
}
export interface QueryGranteeGrantsRequest {
    grantee: string;
    /** pagination defines an pagination for the request. */
    pagination?: PageRequest;
}
export interface QueryGranteeGrantsResponse {
    /** grants is a list of grants granted to the grantee. */
    grants: GrantAuthorization[];
    /** pagination defines an pagination for the response. */
    pagination?: PageResponse;
}
