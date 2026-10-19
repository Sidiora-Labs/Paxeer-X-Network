import * as fm from "../../fetch";
export class Query {
    static Params(req, initReq) {
        return fm.fetchReq(`/paxchain/mint/v1beta1/params?${fm.renderURLSearchParams(req, [])}`, {
            ...initReq,
            method: "GET"
        });
    }
    static Minter(req, initReq) {
        return fm.fetchReq(`/paxchain/mint/v1beta1/minter?${fm.renderURLSearchParams(req, [])}`, {
            ...initReq,
            method: "GET"
        });
    }
}
