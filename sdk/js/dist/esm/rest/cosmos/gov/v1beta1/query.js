import * as fm from "../../../fetch";
export class Query {
    static Proposal(req, initReq) {
        return fm.fetchReq(`/cosmos/gov/v1beta1/proposals/${req["proposal_id"]}?${fm.renderURLSearchParams(req, ["proposal_id"])}`, { ...initReq, method: "GET" });
    }
    static Proposals(req, initReq) {
        return fm.fetchReq(`/cosmos/gov/v1beta1/proposals?${fm.renderURLSearchParams(req, [])}`, {
            ...initReq,
            method: "GET"
        });
    }
    static Vote(req, initReq) {
        return fm.fetchReq(`/cosmos/gov/v1beta1/proposals/${req["proposal_id"]}/votes/${req["voter"]}?${fm.renderURLSearchParams(req, ["proposal_id", "voter"])}`, { ...initReq, method: "GET" });
    }
    static Votes(req, initReq) {
        return fm.fetchReq(`/cosmos/gov/v1beta1/proposals/${req["proposal_id"]}/votes?${fm.renderURLSearchParams(req, ["proposal_id"])}`, { ...initReq, method: "GET" });
    }
    static Params(req, initReq) {
        return fm.fetchReq(`/cosmos/gov/v1beta1/params/${req["params_type"]}?${fm.renderURLSearchParams(req, ["params_type"])}`, { ...initReq, method: "GET" });
    }
    static Deposit(req, initReq) {
        return fm.fetchReq(`/cosmos/gov/v1beta1/proposals/${req["proposal_id"]}/deposits/${req["depositor"]}?${fm.renderURLSearchParams(req, ["proposal_id", "depositor"])}`, { ...initReq, method: "GET" });
    }
    static Deposits(req, initReq) {
        return fm.fetchReq(`/cosmos/gov/v1beta1/proposals/${req["proposal_id"]}/deposits?${fm.renderURLSearchParams(req, ["proposal_id"])}`, { ...initReq, method: "GET" });
    }
    static TallyResult(req, initReq) {
        return fm.fetchReq(`/cosmos/gov/v1beta1/proposals/${req["proposal_id"]}/tally?${fm.renderURLSearchParams(req, ["proposal_id"])}`, { ...initReq, method: "GET" });
    }
}
