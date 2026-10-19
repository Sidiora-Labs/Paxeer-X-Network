import * as fm from "../../../fetch";
export class Query {
    static Grants(req, initReq) {
        return fm.fetchReq(`/cosmos/authz/v1beta1/grants?${fm.renderURLSearchParams(req, [])}`, {
            ...initReq,
            method: "GET"
        });
    }
    static GranterGrants(req, initReq) {
        return fm.fetchReq(`/cosmos/authz/v1beta1/grants/granter/${req["granter"]}?${fm.renderURLSearchParams(req, ["granter"])}`, { ...initReq, method: "GET" });
    }
    static GranteeGrants(req, initReq) {
        return fm.fetchReq(`/cosmos/authz/v1beta1/grants/grantee/${req["grantee"]}?${fm.renderURLSearchParams(req, ["grantee"])}`, { ...initReq, method: "GET" });
    }
}
