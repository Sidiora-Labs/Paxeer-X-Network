import * as fm from "../../../fetch";
export class Query {
    static Params(req, initReq) {
        return fm.fetchReq(`/cosmos/slashing/v1beta1/params?${fm.renderURLSearchParams(req, [])}`, {
            ...initReq,
            method: "GET"
        });
    }
    static SigningInfo(req, initReq) {
        return fm.fetchReq(`/cosmos/slashing/v1beta1/signing_infos/${req["cons_address"]}?${fm.renderURLSearchParams(req, ["cons_address"])}`, { ...initReq, method: "GET" });
    }
    static SigningInfos(req, initReq) {
        return fm.fetchReq(`/cosmos/slashing/v1beta1/signing_infos?${fm.renderURLSearchParams(req, [])}`, {
            ...initReq,
            method: "GET"
        });
    }
}
