import * as fm from "../../../fetch";
export class Query {
    static Params(req, initReq) {
        return fm.fetchReq(`/cosmos/distribution/v1beta1/params?${fm.renderURLSearchParams(req, [])}`, {
            ...initReq,
            method: "GET"
        });
    }
    static ValidatorOutstandingRewards(req, initReq) {
        return fm.fetchReq(`/cosmos/distribution/v1beta1/validators/${req["validator_address"]}/outstanding_rewards?${fm.renderURLSearchParams(req, ["validator_address"])}`, { ...initReq, method: "GET" });
    }
    static ValidatorCommission(req, initReq) {
        return fm.fetchReq(`/cosmos/distribution/v1beta1/validators/${req["validator_address"]}/commission?${fm.renderURLSearchParams(req, ["validator_address"])}`, { ...initReq, method: "GET" });
    }
    static ValidatorSlashes(req, initReq) {
        return fm.fetchReq(`/cosmos/distribution/v1beta1/validators/${req["validator_address"]}/slashes?${fm.renderURLSearchParams(req, ["validator_address"])}`, { ...initReq, method: "GET" });
    }
    static DelegationRewards(req, initReq) {
        return fm.fetchReq(`/cosmos/distribution/v1beta1/delegators/${req["delegator_address"]}/rewards/${req["validator_address"]}?${fm.renderURLSearchParams(req, ["delegator_address", "validator_address"])}`, { ...initReq, method: "GET" });
    }
    static DelegationTotalRewards(req, initReq) {
        return fm.fetchReq(`/cosmos/distribution/v1beta1/delegators/${req["delegator_address"]}/rewards?${fm.renderURLSearchParams(req, ["delegator_address"])}`, { ...initReq, method: "GET" });
    }
    static DelegatorValidators(req, initReq) {
        return fm.fetchReq(`/cosmos/distribution/v1beta1/delegators/${req["delegator_address"]}/validators?${fm.renderURLSearchParams(req, ["delegator_address"])}`, { ...initReq, method: "GET" });
    }
    static DelegatorWithdrawAddress(req, initReq) {
        return fm.fetchReq(`/cosmos/distribution/v1beta1/delegators/${req["delegator_address"]}/withdraw_address?${fm.renderURLSearchParams(req, ["delegator_address"])}`, { ...initReq, method: "GET" });
    }
    static CommunityPool(req, initReq) {
        return fm.fetchReq(`/cosmos/distribution/v1beta1/community_pool?${fm.renderURLSearchParams(req, [])}`, { ...initReq, method: "GET" });
    }
}
