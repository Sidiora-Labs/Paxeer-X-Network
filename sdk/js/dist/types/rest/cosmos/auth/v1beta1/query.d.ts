import * as fm from "../../../fetch";
import type { QueryAccountRequest, QueryAccountResponse, QueryAccountsRequest, QueryAccountsResponse, QueryNextAccountNumberRequest, QueryNextAccountNumberResponse, QueryParamsRequest, QueryParamsResponse } from "../../../../types/cosmos/auth/v1beta1/query.ts";
export declare class Query {
    static Accounts(req: QueryAccountsRequest, initReq?: fm.InitReq): Promise<QueryAccountsResponse>;
    static Account(req: QueryAccountRequest, initReq?: fm.InitReq): Promise<QueryAccountResponse>;
    static Params(req: QueryParamsRequest, initReq?: fm.InitReq): Promise<QueryParamsResponse>;
    static NextAccountNumber(req: QueryNextAccountNumberRequest, initReq?: fm.InitReq): Promise<QueryNextAccountNumberResponse>;
}
