import * as fm from "../../../fetch";
export class Query {
    static Allowance(req, initReq) {
        return fm.fetchReq(`/cosmos/feegrant/v1beta1/allowance/${req["granter"]}/${req["grantee"]}?${fm.renderURLSearchParams(req, ["granter", "grantee"])}`, { ...initReq, method: "GET" });
    }
    static Allowances(req, initReq) {
        return fm.fetchReq(`/cosmos/feegrant/v1beta1/allowances/${req["grantee"]}?${fm.renderURLSearchParams(req, ["grantee"])}`, { ...initReq, method: "GET" });
    }
    static AllowancesByGranter(req, initReq) {
        return fm.fetchReq(`/cosmos/feegrant/v1beta1/issued/${req["granter"]}?${fm.renderURLSearchParams(req, ["granter"])}`, { ...initReq, method: "GET" });
    }
}
