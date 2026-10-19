import type { Params } from "./mint";
export type QueryParamsRequest = {};
export interface QueryParamsResponse {
    /** params defines the parameters of the module. */
    params?: Params;
}
export type QueryMinterRequest = {};
export interface QueryMinterResponse {
    start_date: string;
    end_date: string;
    denom: string;
    total_mint_amount: number;
    remaining_mint_amount: number;
    last_mint_amount: number;
    last_mint_date: string;
    last_mint_height: number;
}
