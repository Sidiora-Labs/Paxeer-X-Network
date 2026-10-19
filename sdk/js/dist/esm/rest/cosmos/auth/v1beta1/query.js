import * as fm from "../../../fetch";
export class Query {
    static Accounts(req, initReq) {
        return fm.fetchReq(`/cosmos/auth/v1beta1/accounts?${fm.renderURLSearchParams(req, [])}`, {
            ...initReq,
            method: "GET"
        });
    }
    static Account(req, initReq) {
        return fm.fetchReq(`/cosmos/auth/v1beta1/accounts/${req["address"]}?${fm.renderURLSearchParams(req, ["address"])}`, { ...initReq, method: "GET" });
    }
    static Params(req, initReq) {
        return fm.fetchReq(`/cosmos/auth/v1beta1/params?${fm.renderURLSearchParams(req, [])}`, {
            ...initReq,
            method: "GET"
        });
    }
    static NextAccountNumber(req, initReq) {
        return fm.fetchReq(`/cosmos/auth/v1beta1/nextaccountnumber?${fm.renderURLSearchParams(req, [])}`, { ...initReq, method: "GET" });
    }
}
