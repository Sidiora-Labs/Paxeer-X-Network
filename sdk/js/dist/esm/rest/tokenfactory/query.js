import * as fm from "../fetch";
export class Query {
    static Params(req, initReq) {
        return fm.fetchReq(`/pax-protocol/paxchain/tokenfactory/params?${fm.renderURLSearchParams(req, [])}`, {
            ...initReq,
            method: "GET"
        });
    }
    static DenomAuthorityMetadata(req, initReq) {
        return fm.fetchReq(`/pax-protocol/paxchain/tokenfactory/denoms/${req["denom"]}/authority_metadata?${fm.renderURLSearchParams(req, ["denom"])}`, { ...initReq, method: "GET" });
    }
    static DenomMetadata(req, initReq) {
        return fm.fetchReq(`/pax-protocol/paxchain/tokenfactory/denoms/metadata?${fm.renderURLSearchParams(req, [])}`, { ...initReq, method: "GET" });
    }
    static DenomsFromCreator(req, initReq) {
        return fm.fetchReq(`/pax-protocol/paxchain/tokenfactory/denoms_from_creator/${req["creator"]}?${fm.renderURLSearchParams(req, ["creator"])}`, { ...initReq, method: "GET" });
    }
    static DenomAllowList(req, initReq) {
        return fm.fetchReq(`/pax-protocol/paxchain/tokenfactory/denoms/allow_list?${fm.renderURLSearchParams(req, [])}`, { ...initReq, method: "GET" });
    }
}
