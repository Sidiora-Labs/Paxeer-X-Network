import type { ParamChange } from "./params";
export interface QueryParamsRequest {
    /** subspace defines the module to query the parameter for. */
    subspace: string;
    /** key defines the key of the parameter in the subspace. */
    key: string;
}
export interface QueryParamsResponse {
    /** param defines the queried parameter. */
    param?: ParamChange;
}
