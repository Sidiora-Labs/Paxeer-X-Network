import type { ModuleVersion, Plan } from "./upgrade";
export type QueryCurrentPlanRequest = {};
export interface QueryCurrentPlanResponse {
    /** plan is the current upgrade plan. */
    plan?: Plan;
}
export interface QueryAppliedPlanRequest {
    /** name is the name of the applied plan to query for. */
    name: string;
}
export interface QueryAppliedPlanResponse {
    /** height is the block height at which the plan was applied. */
    height: number;
}
export interface QueryUpgradedConsensusStateRequest {
    /**
     * last height of the current chain must be sent in request
     * as this is the height under which next consensus state is stored
     */
    last_height: number;
}
export interface QueryUpgradedConsensusStateResponse {
    /** Since: cosmos-sdk 0.43 */
    upgraded_consensus_state: Uint8Array;
}
export interface QueryModuleVersionsRequest {
    /**
     * module_name is a field to query a specific module
     * consensus version from state. Leaving this empty will
     * fetch the full list of module versions from state
     */
    module_name: string;
}
export interface QueryModuleVersionsResponse {
    /** module_versions is a list of module names with their consensus versions. */
    module_versions: ModuleVersion[];
}
