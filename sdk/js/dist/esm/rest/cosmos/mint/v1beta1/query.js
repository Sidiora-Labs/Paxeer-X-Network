import * as fm from "../../../fetch";
export class Query {
    static Params(req, initReq) {
        return fm.fetchReq(`/cosmos/mint/v1beta1/params?${fm.renderURLSearchParams(req, [])}`, {
            ...initReq,
            method: "GET"
        });
    }
    static Inflation(req, initReq) {
        return fm.fetchReq(`/cosmos/mint/v1beta1/inflation?${fm.renderURLSearchParams(req, [])}`, {
            ...initReq,
            method: "GET"
        });
    }
    static AnnualProvisions(req, initReq) {
        return fm.fetchReq(`/cosmos/mint/v1beta1/annual_provisions?${fm.renderURLSearchParams(req, [])}`, { ...initReq, method: "GET" });
    }
}
