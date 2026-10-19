import type { Epoch } from "./epoch";
import type { Params } from "./params";
export type QueryParamsRequest = {};
export interface QueryParamsResponse {
    /** params holds all the parameters of this module. */
    params?: Params;
}
export type QueryEpochRequest = {};
export interface QueryEpochResponse {
    epoch?: Epoch;
}
