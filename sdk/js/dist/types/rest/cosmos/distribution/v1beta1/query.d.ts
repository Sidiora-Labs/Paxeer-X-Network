import * as fm from "../../../fetch";
import type { QueryCommunityPoolRequest, QueryCommunityPoolResponse, QueryDelegationRewardsRequest, QueryDelegationRewardsResponse, QueryDelegationTotalRewardsRequest, QueryDelegationTotalRewardsResponse, QueryDelegatorValidatorsRequest, QueryDelegatorValidatorsResponse, QueryDelegatorWithdrawAddressRequest, QueryDelegatorWithdrawAddressResponse, QueryParamsRequest, QueryParamsResponse, QueryValidatorCommissionRequest, QueryValidatorCommissionResponse, QueryValidatorOutstandingRewardsRequest, QueryValidatorOutstandingRewardsResponse, QueryValidatorSlashesRequest, QueryValidatorSlashesResponse } from "../../../../types/cosmos/distribution/v1beta1/query.ts";
export declare class Query {
    static Params(req: QueryParamsRequest, initReq?: fm.InitReq): Promise<QueryParamsResponse>;
    static ValidatorOutstandingRewards(req: QueryValidatorOutstandingRewardsRequest, initReq?: fm.InitReq): Promise<QueryValidatorOutstandingRewardsResponse>;
    static ValidatorCommission(req: QueryValidatorCommissionRequest, initReq?: fm.InitReq): Promise<QueryValidatorCommissionResponse>;
    static ValidatorSlashes(req: QueryValidatorSlashesRequest, initReq?: fm.InitReq): Promise<QueryValidatorSlashesResponse>;
    static DelegationRewards(req: QueryDelegationRewardsRequest, initReq?: fm.InitReq): Promise<QueryDelegationRewardsResponse>;
    static DelegationTotalRewards(req: QueryDelegationTotalRewardsRequest, initReq?: fm.InitReq): Promise<QueryDelegationTotalRewardsResponse>;
    static DelegatorValidators(req: QueryDelegatorValidatorsRequest, initReq?: fm.InitReq): Promise<QueryDelegatorValidatorsResponse>;
    static DelegatorWithdrawAddress(req: QueryDelegatorWithdrawAddressRequest, initReq?: fm.InitReq): Promise<QueryDelegatorWithdrawAddressResponse>;
    static CommunityPool(req: QueryCommunityPoolRequest, initReq?: fm.InitReq): Promise<QueryCommunityPoolResponse>;
}
