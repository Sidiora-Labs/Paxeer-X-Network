import * as fm from "../../../fetch";
export class Query {
    static Evidence(req, initReq) {
        return fm.fetchReq(`/cosmos/evidence/v1beta1/evidence/${req["evidence_hash"]}?${fm.renderURLSearchParams(req, ["evidence_hash"])}`, { ...initReq, method: "GET" });
    }
    static AllEvidence(req, initReq) {
        return fm.fetchReq(`/cosmos/evidence/v1beta1/evidence?${fm.renderURLSearchParams(req, [])}`, {
            ...initReq,
            method: "GET"
        });
    }
}
