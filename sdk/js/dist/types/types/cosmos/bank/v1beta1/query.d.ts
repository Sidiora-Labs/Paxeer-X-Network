import type { PageRequest, PageResponse } from "../../base/query/v1beta1/pagination";
import type { Coin } from "../../base/v1beta1/coin";
import type { Metadata, Params } from "./bank";
export interface QueryBalanceRequest {
    /** address is the address to query balances for. */
    address: string;
    /** denom is the coin denom to query balances for. */
    denom: string;
}
export interface QueryBalanceResponse {
    /** balance is the balance of the coin. */
    balance?: Coin;
}
export interface QueryAllBalancesRequest {
    /** address is the address to query balances for. */
    address: string;
    /** pagination defines an optional pagination for the request. */
    pagination?: PageRequest;
}
export interface QueryAllBalancesResponse {
    /** balances is the balances of all the coins. */
    balances: Coin[];
    /** pagination defines the pagination in the response. */
    pagination?: PageResponse;
}
export interface QuerySpendableBalancesRequest {
    /** address is the address to query spendable balances for. */
    address: string;
    /** pagination defines an optional pagination for the request. */
    pagination?: PageRequest;
}
export interface QuerySpendableBalancesResponse {
    /** balances is the spendable balances of all the coins. */
    balances: Coin[];
    /** pagination defines the pagination in the response. */
    pagination?: PageResponse;
}
export interface QueryTotalSupplyRequest {
    /**
     * pagination defines an optional pagination for the request.
     *
     * Since: cosmos-sdk 0.43
     */
    pagination?: PageRequest;
}
export interface QueryTotalSupplyResponse {
    /** supply is the supply of the coins */
    supply: Coin[];
    /**
     * pagination defines the pagination in the response.
     *
     * Since: cosmos-sdk 0.43
     */
    pagination?: PageResponse;
}
export interface QuerySupplyOfRequest {
    /** denom is the coin denom to query balances for. */
    denom: string;
}
export interface QuerySupplyOfResponse {
    /** amount is the supply of the coin. */
    amount?: Coin;
}
export type QueryParamsRequest = {};
export interface QueryParamsResponse {
    params?: Params;
}
export interface QueryDenomsMetadataRequest {
    /** pagination defines an optional pagination for the request. */
    pagination?: PageRequest;
}
export interface QueryDenomsMetadataResponse {
    /** metadata provides the client information for all the registered tokens. */
    metadatas: Metadata[];
    /** pagination defines the pagination in the response. */
    pagination?: PageResponse;
}
export interface QueryDenomMetadataRequest {
    /** denom is the coin denom to query the metadata for. */
    denom: string;
}
export interface QueryDenomMetadataResponse {
    /** metadata describes and provides all the client information for the requested token. */
    metadata?: Metadata;
}
