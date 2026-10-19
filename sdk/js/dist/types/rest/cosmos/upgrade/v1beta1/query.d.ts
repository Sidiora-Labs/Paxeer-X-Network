import * as fm from "../../../fetch";
import type { QueryAppliedPlanRequest, QueryAppliedPlanResponse, QueryCurrentPlanRequest, QueryCurrentPlanResponse, QueryModuleVersionsRequest, QueryModuleVersionsResponse, QueryUpgradedConsensusStateRequest, QueryUpgradedConsensusStateResponse } from "../../../../types/cosmos/upgrade/v1beta1/query.ts";
export declare class Query {
    static CurrentPlan(req: QueryCurrentPlanRequest, initReq?: fm.InitReq): Promise<QueryCurrentPlanResponse>;
    static AppliedPlan(req: QueryAppliedPlanRequest, initReq?: fm.InitReq): Promise<QueryAppliedPlanResponse>;
    static UpgradedConsensusState(req: QueryUpgradedConsensusStateRequest, initReq?: fm.InitReq): Promise<QueryUpgradedConsensusStateResponse>;
    static ModuleVersions(req: QueryModuleVersionsRequest, initReq?: fm.InitReq): Promise<QueryModuleVersionsResponse>;
}
