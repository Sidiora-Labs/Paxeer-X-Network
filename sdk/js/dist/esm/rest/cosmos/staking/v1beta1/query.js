import * as fm from "../../../fetch";
export class Query {
    static Validators(req, initReq) {
        return fm.fetchReq(`/cosmos/staking/v1beta1/validators?${fm.renderURLSearchParams(req, [])}`, {
            ...initReq,
            method: "GET"
        });
    }
    static Validator(req, initReq) {
        return fm.fetchReq(`/cosmos/staking/v1beta1/validators/${req["validator_addr"]}?${fm.renderURLSearchParams(req, ["validator_addr"])}`, { ...initReq, method: "GET" });
    }
    static ValidatorDelegations(req, initReq) {
        return fm.fetchReq(`/cosmos/staking/v1beta1/validators/${req["validator_addr"]}/delegations?${fm.renderURLSearchParams(req, ["validator_addr"])}`, { ...initReq, method: "GET" });
    }
    static ValidatorUnbondingDelegations(req, initReq) {
        return fm.fetchReq(`/cosmos/staking/v1beta1/validators/${req["validator_addr"]}/unbonding_delegations?${fm.renderURLSearchParams(req, ["validator_addr"])}`, { ...initReq, method: "GET" });
    }
    static Delegation(req, initReq) {
        return fm.fetchReq(`/cosmos/staking/v1beta1/validators/${req["validator_addr"]}/delegations/${req["delegator_addr"]}?${fm.renderURLSearchParams(req, ["validator_addr", "delegator_addr"])}`, { ...initReq, method: "GET" });
    }
    static UnbondingDelegation(req, initReq) {
        return fm.fetchReq(`/cosmos/staking/v1beta1/validators/${req["validator_addr"]}/delegations/${req["delegator_addr"]}/unbonding_delegation?${fm.renderURLSearchParams(req, ["validator_addr", "delegator_addr"])}`, { ...initReq, method: "GET" });
    }
    static DelegatorDelegations(req, initReq) {
        return fm.fetchReq(`/cosmos/staking/v1beta1/delegations/${req["delegator_addr"]}?${fm.renderURLSearchParams(req, ["delegator_addr"])}`, { ...initReq, method: "GET" });
    }
    static DelegatorUnbondingDelegations(req, initReq) {
        return fm.fetchReq(`/cosmos/staking/v1beta1/delegators/${req["delegator_addr"]}/unbonding_delegations?${fm.renderURLSearchParams(req, ["delegator_addr"])}`, { ...initReq, method: "GET" });
    }
    static Redelegations(req, initReq) {
        return fm.fetchReq(`/cosmos/staking/v1beta1/delegators/${req["delegator_addr"]}/redelegations?${fm.renderURLSearchParams(req, ["delegator_addr"])}`, { ...initReq, method: "GET" });
    }
    static DelegatorValidators(req, initReq) {
        return fm.fetchReq(`/cosmos/staking/v1beta1/delegators/${req["delegator_addr"]}/validators?${fm.renderURLSearchParams(req, ["delegator_addr"])}`, { ...initReq, method: "GET" });
    }
    static DelegatorValidator(req, initReq) {
        return fm.fetchReq(`/cosmos/staking/v1beta1/delegators/${req["delegator_addr"]}/validators/${req["validator_addr"]}?${fm.renderURLSearchParams(req, ["delegator_addr", "validator_addr"])}`, { ...initReq, method: "GET" });
    }
    static HistoricalInfo(req, initReq) {
        return fm.fetchReq(`/cosmos/staking/v1beta1/historical_info/${req["height"]}?${fm.renderURLSearchParams(req, ["height"])}`, { ...initReq, method: "GET" });
    }
    static Pool(req, initReq) {
        return fm.fetchReq(`/cosmos/staking/v1beta1/pool?${fm.renderURLSearchParams(req, [])}`, { ...initReq, method: "GET" });
    }
    static Params(req, initReq) {
        return fm.fetchReq(`/cosmos/staking/v1beta1/params?${fm.renderURLSearchParams(req, [])}`, {
            ...initReq,
            method: "GET"
        });
    }
}
