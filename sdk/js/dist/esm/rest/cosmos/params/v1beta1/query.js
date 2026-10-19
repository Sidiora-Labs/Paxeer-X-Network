import * as fm from "../../../fetch";
export class Query {
    static Params(req, initReq) {
        return fm.fetchReq(`/cosmos/params/v1beta1/params?${fm.renderURLSearchParams(req, [])}`, {
            ...initReq,
            method: "GET"
        });
    }
}
