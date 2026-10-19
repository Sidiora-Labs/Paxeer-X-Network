import type { Params } from "./mint";
export type QueryParamsRequest = {};
export interface QueryParamsResponse {
    /** params defines the parameters of the module. */
    params?: Params;
}
export type QueryInflationRequest = {};
export interface QueryInflationResponse {
    /** inflation is the current minting inflation value. */
    inflation: Uint8Array;
}
export type QueryAnnualProvisionsRequest = {};
export interface QueryAnnualProvisionsResponse {
    /** annual_provisions is the current minting annual provisions value. */
    annual_provisions: Uint8Array;
}
