import * as fm from "../../../fetch";
import type { QueryAllBalancesRequest, QueryAllBalancesResponse, QueryBalanceRequest, QueryBalanceResponse, QueryDenomMetadataRequest, QueryDenomMetadataResponse, QueryDenomsMetadataRequest, QueryDenomsMetadataResponse, QueryParamsRequest, QueryParamsResponse, QuerySpendableBalancesRequest, QuerySpendableBalancesResponse, QuerySupplyOfRequest, QuerySupplyOfResponse, QueryTotalSupplyRequest, QueryTotalSupplyResponse } from "../../../../types/cosmos/bank/v1beta1/query.ts";
export declare class Query {
    static Balance(req: QueryBalanceRequest, initReq?: fm.InitReq): Promise<QueryBalanceResponse>;
    static AllBalances(req: QueryAllBalancesRequest, initReq?: fm.InitReq): Promise<QueryAllBalancesResponse>;
    static SpendableBalances(req: QuerySpendableBalancesRequest, initReq?: fm.InitReq): Promise<QuerySpendableBalancesResponse>;
    static TotalSupply(req: QueryTotalSupplyRequest, initReq?: fm.InitReq): Promise<QueryTotalSupplyResponse>;
    static SupplyOf(req: QuerySupplyOfRequest, initReq?: fm.InitReq): Promise<QuerySupplyOfResponse>;
    static Params(req: QueryParamsRequest, initReq?: fm.InitReq): Promise<QueryParamsResponse>;
    static DenomMetadata(req: QueryDenomMetadataRequest, initReq?: fm.InitReq): Promise<QueryDenomMetadataResponse>;
    static DenomsMetadata(req: QueryDenomsMetadataRequest, initReq?: fm.InitReq): Promise<QueryDenomsMetadataResponse>;
}
