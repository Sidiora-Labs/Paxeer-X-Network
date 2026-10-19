import * as fm from "../../../fetch";
import type { QueryDelegationRequest, QueryDelegationResponse, QueryDelegatorDelegationsRequest, QueryDelegatorDelegationsResponse, QueryDelegatorUnbondingDelegationsRequest, QueryDelegatorUnbondingDelegationsResponse, QueryDelegatorValidatorRequest, QueryDelegatorValidatorResponse, QueryDelegatorValidatorsRequest, QueryDelegatorValidatorsResponse, QueryHistoricalInfoRequest, QueryHistoricalInfoResponse, QueryParamsRequest, QueryParamsResponse, QueryPoolRequest, QueryPoolResponse, QueryRedelegationsRequest, QueryRedelegationsResponse, QueryUnbondingDelegationRequest, QueryUnbondingDelegationResponse, QueryValidatorDelegationsRequest, QueryValidatorDelegationsResponse, QueryValidatorRequest, QueryValidatorResponse, QueryValidatorUnbondingDelegationsRequest, QueryValidatorUnbondingDelegationsResponse, QueryValidatorsRequest, QueryValidatorsResponse } from "../../../../types/cosmos/staking/v1beta1/query.ts";
export declare class Query {
    static Validators(req: QueryValidatorsRequest, initReq?: fm.InitReq): Promise<QueryValidatorsResponse>;
    static Validator(req: QueryValidatorRequest, initReq?: fm.InitReq): Promise<QueryValidatorResponse>;
    static ValidatorDelegations(req: QueryValidatorDelegationsRequest, initReq?: fm.InitReq): Promise<QueryValidatorDelegationsResponse>;
    static ValidatorUnbondingDelegations(req: QueryValidatorUnbondingDelegationsRequest, initReq?: fm.InitReq): Promise<QueryValidatorUnbondingDelegationsResponse>;
    static Delegation(req: QueryDelegationRequest, initReq?: fm.InitReq): Promise<QueryDelegationResponse>;
    static UnbondingDelegation(req: QueryUnbondingDelegationRequest, initReq?: fm.InitReq): Promise<QueryUnbondingDelegationResponse>;
    static DelegatorDelegations(req: QueryDelegatorDelegationsRequest, initReq?: fm.InitReq): Promise<QueryDelegatorDelegationsResponse>;
    static DelegatorUnbondingDelegations(req: QueryDelegatorUnbondingDelegationsRequest, initReq?: fm.InitReq): Promise<QueryDelegatorUnbondingDelegationsResponse>;
    static Redelegations(req: QueryRedelegationsRequest, initReq?: fm.InitReq): Promise<QueryRedelegationsResponse>;
    static DelegatorValidators(req: QueryDelegatorValidatorsRequest, initReq?: fm.InitReq): Promise<QueryDelegatorValidatorsResponse>;
    static DelegatorValidator(req: QueryDelegatorValidatorRequest, initReq?: fm.InitReq): Promise<QueryDelegatorValidatorResponse>;
    static HistoricalInfo(req: QueryHistoricalInfoRequest, initReq?: fm.InitReq): Promise<QueryHistoricalInfoResponse>;
    static Pool(req: QueryPoolRequest, initReq?: fm.InitReq): Promise<QueryPoolResponse>;
    static Params(req: QueryParamsRequest, initReq?: fm.InitReq): Promise<QueryParamsResponse>;
}
