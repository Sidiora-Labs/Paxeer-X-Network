import * as fm from "../../../fetch";
export class Query {
    static CurrentPlan(req, initReq) {
        return fm.fetchReq(`/cosmos/upgrade/v1beta1/current_plan?${fm.renderURLSearchParams(req, [])}`, {
            ...initReq,
            method: "GET"
        });
    }
    static AppliedPlan(req, initReq) {
        return fm.fetchReq(`/cosmos/upgrade/v1beta1/applied_plan/${req["name"]}?${fm.renderURLSearchParams(req, ["name"])}`, { ...initReq, method: "GET" });
    }
    static UpgradedConsensusState(req, initReq) {
        return fm.fetchReq(`/cosmos/upgrade/v1beta1/upgraded_consensus_state/${req["last_height"]}?${fm.renderURLSearchParams(req, ["last_height"])}`, { ...initReq, method: "GET" });
    }
    static ModuleVersions(req, initReq) {
        return fm.fetchReq(`/cosmos/upgrade/v1beta1/module_versions?${fm.renderURLSearchParams(req, [])}`, { ...initReq, method: "GET" });
    }
}
