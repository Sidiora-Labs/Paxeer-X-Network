import * as fm from "../../../fetch";
export class Query {
    static Balance(req, initReq) {
        return fm.fetchReq(`/cosmos/bank/v1beta1/balances/${req["address"]}/by_denom?${fm.renderURLSearchParams(req, ["address"])}`, { ...initReq, method: "GET" });
    }
    static AllBalances(req, initReq) {
        return fm.fetchReq(`/cosmos/bank/v1beta1/balances/${req["address"]}?${fm.renderURLSearchParams(req, ["address"])}`, { ...initReq, method: "GET" });
    }
    static SpendableBalances(req, initReq) {
        return fm.fetchReq(`/cosmos/bank/v1beta1/spendable_balances/${req["address"]}?${fm.renderURLSearchParams(req, ["address"])}`, { ...initReq, method: "GET" });
    }
    static TotalSupply(req, initReq) {
        return fm.fetchReq(`/cosmos/bank/v1beta1/supply?${fm.renderURLSearchParams(req, [])}`, {
            ...initReq,
            method: "GET"
        });
    }
    static SupplyOf(req, initReq) {
        return fm.fetchReq(`/cosmos/bank/v1beta1/supply/${req["denom"]}?${fm.renderURLSearchParams(req, ["denom"])}`, {
            ...initReq,
            method: "GET"
        });
    }
    static Params(req, initReq) {
        return fm.fetchReq(`/cosmos/bank/v1beta1/params?${fm.renderURLSearchParams(req, [])}`, {
            ...initReq,
            method: "GET"
        });
    }
    static DenomMetadata(req, initReq) {
        return fm.fetchReq(`/cosmos/bank/v1beta1/denoms_metadata/${req["denom"]}?${fm.renderURLSearchParams(req, ["denom"])}`, { ...initReq, method: "GET" });
    }
    static DenomsMetadata(req, initReq) {
        return fm.fetchReq(`/cosmos/bank/v1beta1/denoms_metadata?${fm.renderURLSearchParams(req, [])}`, {
            ...initReq,
            method: "GET"
        });
    }
}
