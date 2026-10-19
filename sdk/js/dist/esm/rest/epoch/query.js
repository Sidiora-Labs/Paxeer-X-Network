import * as fm from "../fetch";
export class Query {
    static Epoch(req, initReq) {
        return fm.fetchReq(`/pax-protocol/paxchain/epoch/epoch?${fm.renderURLSearchParams(req, [])}`, {
            ...initReq,
            method: "GET"
        });
    }
    static Params(req, initReq) {
        return fm.fetchReq(`/pax-protocol/paxchain/epoch/params?${fm.renderURLSearchParams(req, [])}`, {
            ...initReq,
            method: "GET"
        });
    }
}
